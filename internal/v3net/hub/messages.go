package hub

import (
	"database/sql"
	"fmt"
	"strings"
)

const messagesSchema = `
CREATE TABLE IF NOT EXISTS messages (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	msg_uuid    TEXT UNIQUE NOT NULL,
	network     TEXT NOT NULL,
	area_tag    TEXT NOT NULL,
	data        TEXT NOT NULL,
	received_at DATETIME DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_messages_network ON messages(network, id);
CREATE INDEX IF NOT EXISTS idx_messages_area ON messages(network, area_tag, id);
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
	db.Exec("ALTER TABLE messages ADD COLUMN area_tag TEXT NOT NULL DEFAULT ''")
	// Migration: add composite index for area filtering.
	db.Exec("CREATE INDEX IF NOT EXISTS idx_messages_area ON messages(network, area_tag, id)")
	return &MessageStore{db: db}, nil
}

// Store inserts a message. Returns false if the msg_uuid already exists (dedup).
func (ms *MessageStore) Store(msgUUID, network, areaTag, data string) (bool, error) {
	res, err := ms.db.Exec(
		"INSERT OR IGNORE INTO messages (msg_uuid, network, area_tag, data) VALUES (?, ?, ?, ?)",
		msgUUID, network, areaTag, data,
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

// Fetch returns messages for a network newer than the given cursor UUID,
// filtered to only areas in areaTags. If areaTags is empty, returns nil immediately.
// If sinceUUID is empty or "0", returns from the beginning.
// Returns the raw JSON data strings ordered oldest first.
func (ms *MessageStore) Fetch(network, sinceUUID string, limit int, areaTags []string) ([]string, bool, error) {
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
		// Args: network, each areaTag, sinceUUID, fetchLimit.
		args := make([]any, 0, 1+len(areaTags)+2)
		args = append(args, network)
		for _, tag := range areaTags {
			args = append(args, tag)
		}
		args = append(args, sinceUUID, fetchLimit)
		rows, err = ms.db.Query(
			`SELECT data FROM messages
			 WHERE network = ? AND area_tag IN (`+inClause+`) AND id > (SELECT COALESCE((SELECT id FROM messages WHERE msg_uuid = ?), 0))
			 ORDER BY id ASC LIMIT ?`,
			args...,
		)
	}
	if err != nil {
		return nil, false, fmt.Errorf("hub: fetch messages: %w", err)
	}
	defer rows.Close()

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
