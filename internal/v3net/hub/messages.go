package hub

import (
	"database/sql"
	"fmt"
	"strings"
)

// A message's identity is (network, origin_node, msg_uuid): the UUID is
// chosen by the sender, so two nodes can use the same one, and both posts are
// kept. checkMessageAuthor makes origin_node the authenticated poster, so a
// node cannot claim another node's UUID. The UNIQUE columns are listed with
// msg_uuid before origin_node so its index also serves the cursor lookup by
// (network, msg_uuid) in Fetch.
const messagesSchema = `
CREATE TABLE IF NOT EXISTS messages (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	msg_uuid    TEXT NOT NULL,
	origin_node TEXT NOT NULL DEFAULT '',
	network     TEXT NOT NULL,
	area_tag    TEXT NOT NULL,
	data        TEXT NOT NULL,
	received_at DATETIME DEFAULT (datetime('now')),
	UNIQUE(network, msg_uuid, origin_node)
);
CREATE INDEX IF NOT EXISTS idx_messages_network ON messages(network, id);
CREATE INDEX IF NOT EXISTS idx_messages_area ON messages(network, area_tag, id);
`

// messagesRebuild moves a messages table from the old schema (msg_uuid
// UNIQUE on its own) to messagesSchema. SQLite cannot drop a constraint in
// place, so the table is copied, keeping every id and so the fetch order.
// origin_node comes from the stored message JSON.
const messagesRebuild = `
CREATE TABLE messages_v2 (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	msg_uuid    TEXT NOT NULL,
	origin_node TEXT NOT NULL DEFAULT '',
	network     TEXT NOT NULL,
	area_tag    TEXT NOT NULL,
	data        TEXT NOT NULL,
	received_at DATETIME DEFAULT (datetime('now')),
	UNIQUE(network, msg_uuid, origin_node)
);
INSERT INTO messages_v2 (id, msg_uuid, origin_node, network, area_tag, data, received_at)
	SELECT id, msg_uuid, COALESCE(json_extract(data, '$.origin_node'), ''), network, area_tag, data, received_at
	FROM messages ORDER BY id;
DROP TABLE messages;
ALTER TABLE messages_v2 RENAME TO messages;
CREATE INDEX idx_messages_network ON messages(network, id);
CREATE INDEX idx_messages_area ON messages(network, area_tag, id);
`

// MessageStore handles SQLite-backed message persistence for the hub.
type MessageStore struct {
	db *sql.DB
}

// NewMessageStore initializes the messages table.
func NewMessageStore(db *sql.DB) (*MessageStore, error) {
	if _, err := db.Exec(messagesSchema); err != nil {
		return nil, fmt.Errorf("hub: create messages table: %w", err)
	}
	// Migration: add area_tag column to existing databases.
	if _, err := db.Exec("ALTER TABLE messages ADD COLUMN area_tag TEXT NOT NULL DEFAULT ''"); err != nil {
		// Ignore "duplicate column" — migration already applied.
		if !strings.Contains(err.Error(), "duplicate column") {
			return nil, fmt.Errorf("hub: migrate add area_tag column: %w", err)
		}
	}
	// Migration: add composite index for area filtering.
	if _, err := db.Exec("CREATE INDEX IF NOT EXISTS idx_messages_area ON messages(network, area_tag, id)"); err != nil {
		return nil, fmt.Errorf("hub: migrate create area index: %w", err)
	}
	if err := migrateMessageIdentity(db); err != nil {
		return nil, err
	}
	return &MessageStore{db: db}, nil
}

// migrateMessageIdentity rebuilds a messages table that predates the
// origin_node column, so a UUID is unique per origin node rather than per
// hub. A table that already has the column is left alone, so this is safe to
// run on every start.
func migrateMessageIdentity(db *sql.DB) error {
	var n int
	if err := db.QueryRow(
		"SELECT COUNT(*) FROM pragma_table_info('messages') WHERE name = 'origin_node'",
	).Scan(&n); err != nil {
		return fmt.Errorf("hub: migrate message identity: %w", err)
	}
	if n > 0 {
		return nil
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("hub: migrate message identity: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op after Commit
	// Dropping the table drops its AUTOINCREMENT high-water mark too; carry
	// it over so an id freed by a deleted row is never handed out again.
	var seq sql.NullInt64
	if err := tx.QueryRow("SELECT seq FROM sqlite_sequence WHERE name = 'messages'").Scan(&seq); err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("hub: migrate message identity: read sequence: %w", err)
	}
	if _, err := tx.Exec(messagesRebuild); err != nil {
		return fmt.Errorf("hub: migrate message identity: rebuild: %w", err)
	}
	if seq.Valid {
		if _, err := tx.Exec(
			"UPDATE sqlite_sequence SET seq = MAX(seq, ?) WHERE name = 'messages'", seq.Int64,
		); err != nil {
			return fmt.Errorf("hub: migrate message identity: restore sequence: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("hub: migrate message identity: commit: %w", err)
	}
	return nil
}

// Store inserts a message. Returns false if the network already has a message
// with this msg_uuid from this origin node (dedup).
func (ms *MessageStore) Store(msgUUID, originNode, network, areaTag, data string) (bool, error) {
	res, err := ms.db.Exec(
		"INSERT OR IGNORE INTO messages (msg_uuid, origin_node, network, area_tag, data) VALUES (?, ?, ?, ?, ?)",
		msgUUID, originNode, network, areaTag, data,
	)
	if err != nil {
		return false, fmt.Errorf("hub: store message: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("hub: rows affected: %w", err)
	}
	return rows > 0, nil
}

// Fetch returns messages for a network newer than the given cursor,
// filtered to only areas in areaTags. If areaTags is empty, returns nil immediately.
// If sinceUUID is empty or "0", returns from the beginning.
// Returns the raw JSON data strings ordered oldest first.
//
// The cursor is the message (sinceOrigin, sinceUUID) in this network. Without
// sinceOrigin, sent by leaves older than per-origin identity, it is the
// earliest message in the network with that UUID: that may send again a few
// messages the leaf already has, which it drops as duplicates, but never
// skips one. An unknown cursor starts from the beginning.
func (ms *MessageStore) Fetch(network, sinceUUID, sinceOrigin string, limit int, areaTags []string) ([]string, bool, error) {
	if len(areaTags) == 0 {
		return nil, false, nil
	}

	if limit <= 0 || limit > 500 {
		limit = 100
	}

	// Build IN (?,?,...) placeholder list.
	placeholders := make([]string, len(areaTags))
	for i := range areaTags {
		placeholders[i] = "?"
	}
	inClause := strings.Join(placeholders, ",")

	// Fetch limit+1 to detect if there are more pages.
	fetchLimit := limit + 1

	var rows *sql.Rows
	var err error

	if sinceUUID == "" || sinceUUID == "0" {
		// Args: network, each areaTag, fetchLimit.
		args := make([]any, 0, 1+len(areaTags)+1)
		args = append(args, network)
		for _, tag := range areaTags {
			args = append(args, tag)
		}
		args = append(args, fetchLimit)
		rows, err = ms.db.Query(
			"SELECT data FROM messages WHERE network = ? AND area_tag IN ("+inClause+") ORDER BY id ASC LIMIT ?",
			args...,
		)
	} else {
		// Args: network, each areaTag, network, sinceUUID, [sinceOrigin], fetchLimit.
		cursor := "SELECT MIN(id) FROM messages WHERE network = ? AND msg_uuid = ?"
		args := make([]any, 0, 1+len(areaTags)+4)
		args = append(args, network)
		for _, tag := range areaTags {
			args = append(args, tag)
		}
		args = append(args, network, sinceUUID)
		if sinceOrigin != "" {
			cursor += " AND origin_node = ?"
			args = append(args, sinceOrigin)
		}
		args = append(args, fetchLimit)
		rows, err = ms.db.Query(
			`SELECT data FROM messages
			 WHERE network = ? AND area_tag IN (`+inClause+`) AND id > COALESCE((`+cursor+`), 0)
			 ORDER BY id ASC LIMIT ?`,
			args...,
		)
	}
	if err != nil {
		return nil, false, fmt.Errorf("hub: fetch messages: %w", err)
	}
	defer func() { _ = rows.Close() }() // read-only

	var results []string
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			return nil, false, fmt.Errorf("hub: scan message: %w", err)
		}
		results = append(results, data)
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("hub: iterate messages: %w", err)
	}

	hasMore := len(results) > limit
	if hasMore {
		results = results[:limit]
	}
	return results, hasMore, nil
}

// Count returns the total message count for a network.
func (ms *MessageStore) Count(network string) (int64, error) {
	var count int64
	err := ms.db.QueryRow("SELECT COUNT(*) FROM messages WHERE network = ?", network).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("hub: count messages: %w", err)
	}
	return count, nil
}
