package reddit

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite" // registers the "sqlite" driver
)

const storeSchema = `
CREATE TABLE IF NOT EXISTS posts (
	subreddit     TEXT    NOT NULL,
	post_id       TEXT    NOT NULL,
	comment_count INTEGER NOT NULL,
	last_seen_at  INTEGER NOT NULL,
	PRIMARY KEY (subreddit, post_id)
);
CREATE TABLE IF NOT EXISTS syncs (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	subreddit   TEXT    NOT NULL,
	area_tag    TEXT    NOT NULL,
	finished_at INTEGER NOT NULL,
	posts       INTEGER NOT NULL,
	comments    INTEGER NOT NULL,
	pages       INTEGER NOT NULL,
	error       TEXT    NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS syncs_by_sub ON syncs (subreddit, finished_at);
`

// Store is the gateway's own state: each post's comment count from the last
// sync, so a post page is loaded again only when it has new comments, and a
// log of runs. Duplicate messages are decided by MSGID in the message base,
// not here, so losing this database costs page loads, never duplicates.
type Store struct {
	db *sql.DB
}

// OpenStore opens (creating if needed) the database at path.
func OpenStore(path string) (*Store, error) {
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
	return &Store{db: db}, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// Count returns the comment count recorded for a post.
func (s *Store) Count(sub, postID string) (int, bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT comment_count FROM posts WHERE subreddit = ? AND post_id = ?`, sub, postID).Scan(&n)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return n, true, nil
}

// Set records a post's comment count.
func (s *Store) Set(sub, postID string, n int) error {
	_, err := s.db.Exec(`INSERT INTO posts (subreddit, post_id, comment_count, last_seen_at) VALUES (?, ?, ?, ?)
		ON CONFLICT (subreddit, post_id) DO UPDATE SET comment_count = excluded.comment_count, last_seen_at = excluded.last_seen_at`,
		sub, postID, n, time.Now().Unix())
	return err
}

// RecordRun logs one subreddit's sync.
func (s *Store) RecordRun(r SubResult, finished time.Time) error {
	errText := ""
	if r.Err != nil {
		errText = r.Err.Error()
	}
	_, err := s.db.Exec(`INSERT INTO syncs (subreddit, area_tag, finished_at, posts, comments, pages, error) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		r.Subreddit, r.AreaTag, finished.Unix(), r.Posts, r.Comments, r.Pages, errText)
	return err
}

// LastRun returns the most recent logged sync of a subreddit.
func (s *Store) LastRun(sub string) (SubResult, time.Time, bool, error) {
	var r SubResult
	var at int64
	var errText string
	err := s.db.QueryRow(`SELECT subreddit, area_tag, finished_at, posts, comments, pages, error FROM syncs
		WHERE subreddit = ? ORDER BY finished_at DESC, id DESC LIMIT 1`, sub).
		Scan(&r.Subreddit, &r.AreaTag, &at, &r.Posts, &r.Comments, &r.Pages, &errText)
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
