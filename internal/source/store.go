package source

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite" // registers the "sqlite" driver
)

const storeSchema = `
CREATE TABLE IF NOT EXISTS markers (
	source       TEXT    NOT NULL,
	feed         TEXT    NOT NULL,
	item_id      TEXT    NOT NULL,
	marker       TEXT    NOT NULL,
	last_seen_at INTEGER NOT NULL,
	PRIMARY KEY (source, feed, item_id)
);
CREATE TABLE IF NOT EXISTS syncs (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	source      TEXT    NOT NULL,
	feed        TEXT    NOT NULL,
	area_tag    TEXT    NOT NULL,
	finished_at INTEGER NOT NULL,
	items       INTEGER NOT NULL,
	replies     INTEGER NOT NULL,
	pages       INTEGER NOT NULL,
	error       TEXT    NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS syncs_by_feed ON syncs (source, feed, finished_at);
CREATE TABLE IF NOT EXISTS meta (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);
`

// FeedResult is what one feed's sync did.
type FeedResult struct {
	Source, Feed, AreaTag string
	Items, Replies, Pages int
	Err                   error
}

// Store is the gateway's own state: each thread's change marker from the
// last sync, so a thread page is loaded again only when it has changed, and
// a log of runs. Duplicate messages are decided by MSGID in the message
// base, not here, so losing this database costs page loads, never
// duplicates.
type Store struct {
	db *sql.DB
}

// OpenStore opens, creating if needed, data/source/source.db under dataDir.
// A new database takes over the comment counts in data/reddit/reddit.db,
// from before the gateway handled other sources, so the first sync after an
// upgrade does not reload every thread.
func OpenStore(dataDir string) (*Store, error) {
	path := filepath.Join(dataDir, "source", "source.db")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", path, err)
	}
	// One run may still be going when the next starts; wait for its lock.
	if _, err := db.Exec("PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("configuring %s: %w", path, err)
	}
	if _, err := db.Exec(storeSchema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("creating tables in %s: %w", path, err)
	}
	st := &Store{db: db}
	if err := st.migrateRedditDB(filepath.Join(dataDir, "reddit", "reddit.db")); err != nil {
		_ = db.Close()
		return nil, err
	}
	return st, nil
}

// redditMigrated is the meta key set once reddit.db's counts are copied.
const redditMigrated = "reddit_db_migrated"

// migrateRedditDB copies reddit.db's counts once. Until a copy has
// committed, every open tries again: a migration that failed, say on a
// reddit.db locked by an old helper mid-upgrade, is not lost.
func (s *Store) migrateRedditDB(old string) error {
	var done string
	err := s.db.QueryRow(`SELECT value FROM meta WHERE key = ?`, redditMigrated).Scan(&done)
	if err == nil {
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("reading the store's meta table: %w", err)
	}
	if _, err := os.Stat(old); err != nil {
		return nil // nothing to take over
	}
	// ATTACH applies to one connection, so both statements share one.
	ctx := context.Background()
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("reading %s: %w", old, err)
	}
	defer func() { _ = conn.Close() }() // returns it to the pool
	if _, err := conn.ExecContext(ctx, `ATTACH DATABASE ? AS old`, old); err != nil {
		return fmt.Errorf("reading %s: %w", old, err)
	}
	defer func() { _, _ = conn.ExecContext(ctx, `DETACH DATABASE old`) }() // the copy below is checked
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("copying comment counts from %s: %w", old, err)
	}
	defer func() { _ = tx.Rollback() }() // no-op after Commit
	if _, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO markers (source, feed, item_id, marker, last_seen_at)
		SELECT 'reddit', subreddit, post_id, CAST(comment_count AS TEXT), last_seen_at FROM old.posts`); err != nil {
		return fmt.Errorf("copying comment counts from %s: %w", old, err)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO meta (key, value) VALUES (?, ?)`, redditMigrated, old); err != nil {
		return fmt.Errorf("copying comment counts from %s: %w", old, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("copying comment counts from %s: %w", old, err)
	}
	return nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// Marker returns the change marker recorded for a thread.
func (s *Store) Marker(source, feed, itemID string) (string, bool, error) {
	var m string
	err := s.db.QueryRow(`SELECT marker FROM markers WHERE source = ? AND feed = ? AND item_id = ?`, source, feed, itemID).Scan(&m)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return m, true, nil
}

// SetMarker records a thread's change marker.
func (s *Store) SetMarker(source, feed, itemID, marker string) error {
	_, err := s.db.Exec(`INSERT INTO markers (source, feed, item_id, marker, last_seen_at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (source, feed, item_id) DO UPDATE SET marker = excluded.marker, last_seen_at = excluded.last_seen_at`,
		source, feed, itemID, marker, time.Now().Unix())
	return err
}

// RecordRun logs one feed's sync.
func (s *Store) RecordRun(r FeedResult, finished time.Time) error {
	errText := ""
	if r.Err != nil {
		errText = r.Err.Error()
	}
	_, err := s.db.Exec(`INSERT INTO syncs (source, feed, area_tag, finished_at, items, replies, pages, error) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		r.Source, r.Feed, r.AreaTag, finished.Unix(), r.Items, r.Replies, r.Pages, errText)
	return err
}

// LastRun returns a feed's most recent logged sync.
func (s *Store) LastRun(source, feed string) (FeedResult, time.Time, bool, error) {
	var r FeedResult
	var at int64
	var errText string
	err := s.db.QueryRow(`SELECT source, feed, area_tag, finished_at, items, replies, pages, error FROM syncs
		WHERE source = ? AND feed = ? ORDER BY finished_at DESC, id DESC LIMIT 1`, source, feed).
		Scan(&r.Source, &r.Feed, &r.AreaTag, &at, &r.Items, &r.Replies, &r.Pages, &errText)
	if errors.Is(err, sql.ErrNoRows) {
		return r, time.Time{}, false, nil
	}
	if err != nil {
		return r, time.Time{}, false, err
	}
	if errText != "" {
		r.Err = errors.New(errText)
	}
	return r, time.Unix(at, 0).UTC(), true, nil
}
