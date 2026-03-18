package hub

import (
	"database/sql"
	"fmt"
)

const messagesSchema = `
CREATE TABLE IF NOT EXISTS messages (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	msg_uuid    TEXT UNIQUE NOT NULL,
	network     TEXT NOT NULL,
	data        TEXT NOT NULL,
	received_at DATETIME DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_messages_network ON messages(network, id);
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
	return &MessageStore{db: db}, nil
}

// Store inserts a message. Returns false if the msg_uuid already exists (dedup).
func (ms *MessageStore) Store(msgUUID, network, data string) (bool, error) {
	res, err := ms.db.Exec(
		"INSERT OR IGNORE INTO messages (msg_uuid, network, data) VALUES (?, ?, ?)",
		msgUUID, network, data,
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

// Fetch returns messages for a network newer than the given cursor UUID.
// If sinceUUID is empty or "0", returns from the beginning.
// Returns the raw JSON data strings ordered oldest first.
func (ms *MessageStore) Fetch(network, sinceUUID string, limit int) ([]string, bool, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}

	var rows *sql.Rows
	var err error

	// Fetch limit+1 to detect if there are more pages.
	fetchLimit := limit + 1

	if sinceUUID == "" || sinceUUID == "0" {
		rows, err = ms.db.Query(
			"SELECT data FROM messages WHERE network = ? ORDER BY id ASC LIMIT ?",
			network, fetchLimit,
		)
	} else {
		rows, err = ms.db.Query(
			`SELECT data FROM messages
			 WHERE network = ? AND id > (SELECT COALESCE((SELECT id FROM messages WHERE msg_uuid = ?), 0))
			 ORDER BY id ASC LIMIT ?`,
			network, sinceUUID, fetchLimit,
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
