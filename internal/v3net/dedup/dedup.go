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
//
// pending is 1 for a claim (see Claim): an import that has started but has
// not been confirmed written to the message base.
//
// awaiting_poll is 1 for a post this node sent that its poll has not yet
// reached in the hub's order (see MarkSent and Passed). It counts as seen,
// but is not the poll cursor: posts the hub stored before it may not have
// been fetched yet.
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
	pending          INTEGER NOT NULL DEFAULT 0,
	awaiting_poll    INTEGER NOT NULL DEFAULT 0,
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
	pending          INTEGER NOT NULL DEFAULT 0,
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

// migrate brings a seen_messages table from an older version up to schema.
// A table already up to date is left alone, so this is safe to run on every
// open.
func migrate(db *sql.DB) error {
	if err := migrateOrigin(db); err != nil {
		return err
	}
	//  Rows from before these columns are all confirmed and not awaiting a
	//  poll, so both get 0.
	if err := addColumn(db, "pending"); err != nil {
		return err
	}
	return addColumn(db, "awaiting_poll")
}

// addColumn adds the flag column name, 0 by default, to a seen_messages
// table that predates it.
func addColumn(db *sql.DB, name string) error {
	var n int
	if err := db.QueryRow(
		"SELECT COUNT(*) FROM pragma_table_info('seen_messages') WHERE name = ?", name,
	).Scan(&n); err != nil {
		return fmt.Errorf("dedup: migrate: %w", err)
	}
	if n > 0 {
		return nil
	}
	if _, err := db.Exec("ALTER TABLE seen_messages ADD COLUMN " + name + " INTEGER NOT NULL DEFAULT 0"); err != nil {
		return fmt.Errorf("dedup: migrate: add %s: %w", name, err)
	}
	return nil
}

// migrateOrigin rebuilds a seen_messages table that predates the origin_node
// column. A table that already has it is left alone.
func migrateOrigin(db *sql.DB) error {
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

// MarkSent records a post this node is about to send, as MarkSeen does, but
// as awaiting the poll: it is seen, so the poll does not import it, and it is
// not the poll cursor (LastSeen) until the poll reaches it (Passed). The hub
// may hold posts from other nodes stored before it that the poll has not
// fetched; a cursor on this post would skip them. It is made before the send
// (leaf.SendPost), so a post the hub never stores leaves a row that is seen
// but never the cursor, which does no harm.
func (ix *Index) MarkSent(network, originNode, msgUUID string) error {
	_, err := ix.db.Exec(
		"INSERT OR IGNORE INTO seen_messages (network, origin_node, msg_uuid, awaiting_poll) VALUES (?, ?, ?, 1)",
		network, originNode, msgUUID,
	)
	if err != nil {
		return fmt.Errorf("dedup: mark sent: %w", err)
	}
	return nil
}

// Passed tells the index that the poll has reached the message in the hub's
// order. A post awaiting the poll (MarkSent) stops waiting and becomes the
// newest row, so it is the cursor until a later message is recorded; any
// other row is left alone.
func (ix *Index) Passed(network, originNode, msgUUID string) error {
	_, err := ix.db.Exec(
		`UPDATE seen_messages SET awaiting_poll = 0, id = (SELECT MAX(id) FROM seen_messages) + 1
		WHERE network = ? AND msg_uuid = ? AND origin_node = ? AND awaiting_poll = 1`,
		network, msgUUID, originNode,
	)
	if err != nil {
		return fmt.Errorf("dedup: passed: %w", err)
	}
	return nil
}

// Lookup reports whether the message (network, originNode, msgUUID) has been
// recorded, as Seen does, and whether that record is only a claim not yet
// confirmed (see Claim). A confirmed record, including a legacy one with no
// origin, wins over a claim.
func (ix *Index) Lookup(network, originNode, msgUUID string) (seen, pending bool, err error) {
	var count, minPending int
	err = ix.db.QueryRow(
		"SELECT COUNT(*), COALESCE(MIN(pending), 0) FROM seen_messages WHERE network = ? AND msg_uuid = ? AND origin_node IN (?, '')",
		network, msgUUID, originNode,
	).Scan(&count, &minPending)
	if err != nil {
		return false, false, fmt.Errorf("dedup: lookup: %w", err)
	}
	return count > 0, count > 0 && minPending == 1, nil
}

// Claim records that an import of the message (network, originNode, msgUUID)
// has started, before it is written to the message base. Confirm finishes it
// once written; Release drops it if the write fails. A claim left behind,
// because the BBS stopped or Confirm failed, tells the next import of the
// message to look in the message base before writing it again. Claiming a
// message already recorded does nothing.
func (ix *Index) Claim(network, originNode, msgUUID string) error {
	_, err := ix.db.Exec(
		"INSERT OR IGNORE INTO seen_messages (network, origin_node, msg_uuid, pending) VALUES (?, ?, ?, 1)",
		network, originNode, msgUUID,
	)
	if err != nil {
		return fmt.Errorf("dedup: claim: %w", err)
	}
	return nil
}

// Confirm marks a claimed message written, as local message localMsgNum.
func (ix *Index) Confirm(network, originNode, msgUUID string, localMsgNum int64) error {
	_, err := ix.db.Exec(
		"UPDATE seen_messages SET pending = 0, local_jam_msgnum = ? WHERE network = ? AND msg_uuid = ? AND origin_node = ?",
		localMsgNum, network, msgUUID, originNode,
	)
	if err != nil {
		return fmt.Errorf("dedup: confirm: %w", err)
	}
	return nil
}

// Release drops a claim whose write failed, so a later copy of the message
// is imported as new. A confirmed record is left alone.
func (ix *Index) Release(network, originNode, msgUUID string) error {
	_, err := ix.db.Exec(
		"DELETE FROM seen_messages WHERE network = ? AND msg_uuid = ? AND origin_node = ? AND pending = 1",
		network, msgUUID, originNode,
	)
	if err != nil {
		return fmt.Errorf("dedup: release: %w", err)
	}
	return nil
}

// LastSeen returns the msg_uuid and origin_node of the most recently marked
// message for a network, or "", "" if none. A claim not yet confirmed (see
// Claim) is not a candidate: the poll cursor stays before it, so the message
// is fetched again and its import finished. Nor is a post awaiting the poll
// (MarkSent). The origin is "" for a row recorded before origin_node
// existed.
func (ix *Index) LastSeen(network string) (msgUUID, originNode string, err error) {
	err = ix.db.QueryRow(
		"SELECT msg_uuid, origin_node FROM seen_messages WHERE network = ? AND pending = 0 AND awaiting_poll = 0 ORDER BY id DESC LIMIT 1",
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
