package hub

import (
	"database/sql"
	"fmt"
	"strings"
)

// A message's identity is (network, origin_node, msg_uuid): the UUID is
// chosen by the sender, so two nodes can use the same one, and both posts are
// kept. checkMessageAuthor makes origin_node the authenticated poster, so a
// node cannot claim another node's UUID.
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
	// Fetch finds a UUID's copies in id order: for the cursor's earliest copy
	// and, for older leaves, to serve only the earliest. The unique index
	// cannot serve that (its id comes after origin_node), and without it
	// SQLite walks the whole network by id. Created after the rebuild, which
	// would drop it.
	if _, err := db.Exec("CREATE INDEX IF NOT EXISTS idx_messages_uuid ON messages(network, msg_uuid, id)"); err != nil {
		return nil, fmt.Errorf("hub: create uuid index: %w", err)
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
// perOrigin is set by leaves that identify a message by origin node and
// UUID; they get every message. Older leaves identify a message by UUID
// alone, so they get only the earliest message in the network with each
// UUID. A leaf that dedups on UUID would drop the later copies anyway, and
// serving them could stall it: once another node re-posts UUIDs the leaf
// has, a page of nothing but those copies never moves its cursor, which
// stays on the earliest copy.
//
// The cursor is the message (sinceOrigin, sinceUUID) in this network. Without
// sinceOrigin, or when the network has no message with that origin and UUID,
// it is the earliest message in the network with that UUID: that may send
// again a few messages the leaf already has, which it drops as duplicates,
// but never skips one. Only a UUID the network does not hold at all starts
// from the beginning.
func (ms *MessageStore) Fetch(network, sinceUUID, sinceOrigin string, perOrigin bool, limit int, areaTags []string) ([]string, bool, error) {
	if len(areaTags) == 0 {
		return nil, false, nil
	}

	if limit <= 0 || limit > 500 {
		limit = 100
	}

	// Fetch limit+1 to detect if there are more pages.
	query, args := fetchQuery(network, sinceUUID, sinceOrigin, perOrigin, limit+1, areaTags)
	rows, err := ms.db.Query(query, args...)
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

// fetchQuery builds Fetch's query and its arguments; see Fetch.
func fetchQuery(network, sinceUUID, sinceOrigin string, perOrigin bool, fetchLimit int, areaTags []string) (string, []any) {
	// Build IN (?,?,...) placeholder list.
	placeholders := make([]string, len(areaTags))
	for i := range areaTags {
		placeholders[i] = "?"
	}
	inClause := strings.Join(placeholders, ",")

	query := "SELECT data FROM messages m WHERE m.network = ? AND m.area_tag IN (" + inClause + ")"
	args := make([]any, 0, 1+len(areaTags)+6)
	args = append(args, network)
	for _, tag := range areaTags {
		args = append(args, tag)
	}
	if !perOrigin {
		// Only the earliest copy of each UUID: a search of idx_messages_uuid.
		query += ` AND NOT EXISTS (SELECT 1 FROM messages e
			WHERE e.network = m.network AND e.msg_uuid = m.msg_uuid AND e.id < m.id)`
	}
	if sinceUUID != "" && sinceUUID != "0" {
		const exact = "(SELECT id FROM messages WHERE network = ? AND msg_uuid = ? AND origin_node = ?), "
		const earliest = "(SELECT MIN(id) FROM messages WHERE network = ? AND msg_uuid = ?)"
		cursor := earliest
		if sinceOrigin != "" {
			cursor = exact + earliest
			args = append(args, network, sinceUUID, sinceOrigin)
		}
		args = append(args, network, sinceUUID)
		query += " AND m.id > COALESCE(" + cursor + ", 0)"
	}
	query += " ORDER BY m.id ASC LIMIT ?"
	args = append(args, fetchLimit)
	return query, args
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
