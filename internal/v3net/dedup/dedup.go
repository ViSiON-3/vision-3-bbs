// Package dedup provides a SQLite-backed UUID deduplication index for V3Net messages.
package dedup

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS seen_messages (
	msg_uuid       TEXT PRIMARY KEY,
	network        TEXT NOT NULL,
	local_jam_msgnum INTEGER,
	seen_at        DATETIME DEFAULT (datetime('now'))
);
`

// Index is a SQLite-backed deduplication index.
type Index struct {
	db *sql.DB
}

// Open opens or creates a dedup index at the given path.
func Open(path string) (*Index, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("dedup: open %s: %w", path, err)
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("dedup: create schema: %w", err)
	}
	return &Index{db: db}, nil
}

// Close closes the underlying database.
func (ix *Index) Close() error {
	return ix.db.Close()
}

// Seen returns true if the given message UUID has already been recorded.
func (ix *Index) Seen(msgUUID string) (bool, error) {
	var count int
	err := ix.db.QueryRow("SELECT COUNT(*) FROM seen_messages WHERE msg_uuid = ?", msgUUID).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("dedup: seen check: %w", err)
	}
	return count > 0, nil
}

// MarkSeen records a message UUID. If localMsgNum is non-nil, it stores the
// local JAM message number for reference.
func (ix *Index) MarkSeen(msgUUID, network string, localMsgNum *int64) error {
	_, err := ix.db.Exec(
		"INSERT OR IGNORE INTO seen_messages (msg_uuid, network, local_jam_msgnum) VALUES (?, ?, ?)",
		msgUUID, network, localMsgNum,
	)
	if err != nil {
		return fmt.Errorf("dedup: mark seen: %w", err)
	}
	return nil
}

// LastSeen returns the msg_uuid of the most recently marked message for a
// network, or "" if none.
func (ix *Index) LastSeen(network string) (string, error) {
	var uuid string
	err := ix.db.QueryRow(
		"SELECT msg_uuid FROM seen_messages WHERE network = ? ORDER BY seen_at DESC, rowid DESC LIMIT 1",
		network,
	).Scan(&uuid)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("dedup: last seen: %w", err)
	}
	return uuid, nil
}
