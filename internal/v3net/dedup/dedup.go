// Package dedup provides a SQLite-backed deduplication index for V3Net
// messages, keyed on (network, origin_node, msg_uuid).
package dedup

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

// A message's identity is (network, origin_node, msg_uuid), as on the hub:
// the UUID is chosen by the sender, so two nodes can use the same one, and
// each is a separate message. network is part of the key because one index
// serves every network a node joins, and LastSeen is a per-network cursor: a
// message seen in one network must not hide one in another, or keep that
// network's cursor from moving.
//
// Rows recorded before origin_node existed have an empty origin_node and match
// any origin (see Seen), so nothing already imported is imported again.
// The UNIQUE columns put msg_uuid before origin_node so the index serves
// Seen's lookup of both the exact and the legacy row.
const schema = `
CREATE TABLE IF NOT EXISTS seen_messages (
	id               INTEGER PRIMARY KEY,
	network          TEXT NOT NULL,
	msg_uuid         TEXT NOT NULL,
	origin_node      TEXT NOT NULL DEFAULT '',
	local_jam_msgnum INTEGER,
	seen_at          DATETIME DEFAULT (datetime('now')),
	UNIQUE(network, msg_uuid, origin_node)
);
`

// rebuild moves a seen_messages table from the old schema (msg_uuid as the
// primary key) to schema. SQLite cannot change a primary key in place, so
// the table is copied. Each row keeps its rowid as id, so LastSeen still
// finds the newest row, and gets an empty origin_node.
const rebuild = `
CREATE TABLE seen_messages_v2 (
	id               INTEGER PRIMARY KEY,
	network          TEXT NOT NULL,
	msg_uuid         TEXT NOT NULL,
	origin_node      TEXT NOT NULL DEFAULT '',
	local_jam_msgnum INTEGER,
	seen_at          DATETIME DEFAULT (datetime('now')),
	UNIQUE(network, msg_uuid, origin_node)
);
INSERT INTO seen_messages_v2 (id, network, msg_uuid, origin_node, local_jam_msgnum, seen_at)
	SELECT rowid, network, msg_uuid, '', local_jam_msgnum, seen_at
	FROM seen_messages ORDER BY rowid;
DROP TABLE seen_messages;
ALTER TABLE seen_messages_v2 RENAME TO seen_messages;
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
	db.SetMaxOpenConns(1)
	if _, err := db.Exec("PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000"); err != nil {
		_ = db.Close() // cleanup on error path
		return nil, fmt.Errorf("dedup: configure pragmas: %w", err)
	}
	if _, err := db.Exec(schema); err != nil {
		_ = db.Close() // cleanup on error path
		return nil, fmt.Errorf("dedup: create schema: %w", err)
	}
	if err := migrate(db); err != nil {
		_ = db.Close() // cleanup on error path
		return nil, err
	}
	return &Index{db: db}, nil
}

// migrate rebuilds a seen_messages table that predates the origin_node
// column. A table that already has it is left alone, so this is safe to run
// on every open.
func migrate(db *sql.DB) error {
	var n int
	if err := db.QueryRow(
		"SELECT COUNT(*) FROM pragma_table_info('seen_messages') WHERE name = 'origin_node'",
	).Scan(&n); err != nil {
		return fmt.Errorf("dedup: migrate: %w", err)
	}
	if n > 0 {
		return nil
	}
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("dedup: migrate: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op after Commit
	if _, err := tx.Exec(rebuild); err != nil {
		return fmt.Errorf("dedup: migrate: rebuild: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("dedup: migrate: commit: %w", err)
	}
	return nil
}

// Close closes the underlying database.
func (ix *Index) Close() error {
	return ix.db.Close()
}

// Seen reports whether the message (network, originNode, msgUUID) has
// already been recorded. A row recorded before origin_node existed matches
// any origin.
func (ix *Index) Seen(network, originNode, msgUUID string) (bool, error) {
	var count int
	err := ix.db.QueryRow(
		"SELECT COUNT(*) FROM seen_messages WHERE network = ? AND msg_uuid = ? AND origin_node IN (?, '')",
		network, msgUUID, originNode,
	).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("dedup: seen check: %w", err)
	}
	return count > 0, nil
}

// MarkSeen records the message (network, originNode, msgUUID). If
// localMsgNum is non-nil, it stores the local JAM message number for
// reference.
func (ix *Index) MarkSeen(network, originNode, msgUUID string, localMsgNum *int64) error {
	_, err := ix.db.Exec(
		"INSERT OR IGNORE INTO seen_messages (network, origin_node, msg_uuid, local_jam_msgnum) VALUES (?, ?, ?, ?)",
		network, originNode, msgUUID, localMsgNum,
	)
	if err != nil {
		return fmt.Errorf("dedup: mark seen: %w", err)
	}
	return nil
}

// LastSeen returns the msg_uuid and origin_node of the most recently marked
// message for a network, or "", "" if none. The origin is "" for a row
// recorded before origin_node existed.
func (ix *Index) LastSeen(network string) (msgUUID, originNode string, err error) {
	err = ix.db.QueryRow(
		"SELECT msg_uuid, origin_node FROM seen_messages WHERE network = ? ORDER BY id DESC LIMIT 1",
		network,
	).Scan(&msgUUID, &originNode)
	if err == sql.ErrNoRows {
		return "", "", nil
	}
	if err != nil {
		return "", "", fmt.Errorf("dedup: last seen: %w", err)
	}
	return msgUUID, originNode, nil
}
