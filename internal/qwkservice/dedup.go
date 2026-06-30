package qwkservice

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

const repDedupSchema = `
CREATE TABLE IF NOT EXISTS rep_uploads (
	handle   TEXT NOT NULL,
	rep_hash TEXT NOT NULL,
	seen_at  DATETIME DEFAULT (datetime('now')),
	PRIMARY KEY (handle, rep_hash)
);`

// repDedup is a SQLite-backed store of recently imported REP fingerprints,
// keyed by (uploader handle, payload hash), used to make uploads retry-safe.
type repDedup struct {
	db *sql.DB
}

// openREPDedup opens or creates the dedup database, configures it for safe
// concurrent access, and prunes records older than the retention window.
func openREPDedup(path string) (*repDedup, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("qwk dedup: open %s: %w", path, err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec("PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000"); err != nil {
		db.Close()
		return nil, fmt.Errorf("qwk dedup: configure pragmas: %w", err)
	}
	if _, err := db.Exec(repDedupSchema); err != nil {
		db.Close()
		return nil, fmt.Errorf("qwk dedup: create schema: %w", err)
	}
	if _, err := db.Exec("DELETE FROM rep_uploads WHERE seen_at < datetime('now','-90 days')"); err != nil {
		db.Close()
		return nil, fmt.Errorf("qwk dedup: prune: %w", err)
	}
	return &repDedup{db: db}, nil
}

// Close closes the underlying database.
func (d *repDedup) Close() error {
	return d.db.Close()
}

// RecordIfNew atomically records (handle, hash). It returns true when the row
// was newly inserted and false when it already existed (a duplicate upload).
func (d *repDedup) RecordIfNew(handle, hash string) (bool, error) {
	res, err := d.db.Exec(
		"INSERT OR IGNORE INTO rep_uploads (handle, rep_hash) VALUES (?, ?)",
		handle, hash,
	)
	if err != nil {
		return false, fmt.Errorf("qwk dedup: record: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("qwk dedup: rows affected: %w", err)
	}
	return n == 1, nil
}
