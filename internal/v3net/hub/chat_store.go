package hub

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

const chatSchema = `
CREATE TABLE IF NOT EXISTS chat_history (
	id          INTEGER PRIMARY KEY,
	network     TEXT NOT NULL,
	room        TEXT NOT NULL,
	from_handle TEXT NOT NULL,
	from_node   TEXT NOT NULL,
	from_bbs    TEXT NOT NULL,
	text        TEXT NOT NULL,
	created_at  DATETIME NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_chat_history_room_time
	ON chat_history(network, room, created_at);

CREATE TABLE IF NOT EXISTS chat_private_history (
	id          INTEGER PRIMARY KEY,
	network     TEXT NOT NULL,
	from_handle TEXT NOT NULL,
	from_node   TEXT NOT NULL,
	to_handle   TEXT NOT NULL,
	to_node     TEXT NOT NULL,
	text        TEXT NOT NULL,
	created_at  DATETIME NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_chat_private_history_node
	ON chat_private_history(network, to_node, created_at);
`

// ChatHistoryStore handles SQLite-backed chat history persistence for the hub.
type ChatHistoryStore struct {
	db            *sql.DB
	retentionDays int
}

// NewChatHistoryStore initializes the chat history tables and returns a store.
func NewChatHistoryStore(db *sql.DB, retentionDays int) (*ChatHistoryStore, error) {
	if _, err := db.Exec(chatSchema); err != nil {
		return nil, fmt.Errorf("hub: create chat history tables: %w", err)
	}

	store := &ChatHistoryStore{
		db:            db,
		retentionDays: retentionDays,
	}

	// Prune old messages on startup.
	if err := store.prune(); err != nil {
		return nil, fmt.Errorf("hub: prune chat history: %w", err)
	}

	return store, nil
}

// SaveMessage saves a public room message to chat_history.
func (chs *ChatHistoryStore) SaveMessage(network, room, fromHandle, fromNode, fromBBS, text string) error {
	_, err := chs.db.Exec(
		`INSERT INTO chat_history (network, room, from_handle, from_node, from_bbs, text, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		network, room, fromHandle, fromNode, fromBBS, text, time.Now().UTC(),
	)
	if err != nil {
		return fmt.Errorf("hub: save chat message: %w", err)
	}
	return nil
}

// SavePrivate saves a private message to chat_private_history.
func (chs *ChatHistoryStore) SavePrivate(network, fromHandle, fromNode, toHandle, toNode, text string) error {
	_, err := chs.db.Exec(
		`INSERT INTO chat_private_history (network, from_handle, from_node, to_handle, to_node, text, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		network, fromHandle, fromNode, toHandle, toNode, text, time.Now().UTC(),
	)
	if err != nil {
		return fmt.Errorf("hub: save chat private message: %w", err)
	}
	return nil
}

// RoomHistory returns up to `limit` messages from a room's chat history, oldest first.
// Clamps limit: if <= 0 or > 200, uses 50.
func (chs *ChatHistoryStore) RoomHistory(network, room string, limit int) ([]protocol.ChatMsgPayload, error) {
	// Clamp limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	rows, err := chs.db.Query(
		`SELECT from_handle, from_node, from_bbs, text, created_at
		 FROM chat_history
		 WHERE network = ? AND room = ?
		 ORDER BY created_at DESC
		 LIMIT ?`,
		network, room, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("hub: query room history: %w", err)
	}
	defer rows.Close()

	var results []protocol.ChatMsgPayload
	for rows.Next() {
		var fromHandle, fromNode, fromBBS, text string
		var createdAt time.Time
		if err := rows.Scan(&fromHandle, &fromNode, &fromBBS, &text, &createdAt); err != nil {
			return nil, fmt.Errorf("hub: scan room history row: %w", err)
		}
		results = append(results, protocol.ChatMsgPayload{
			Room:       room,
			FromHandle: fromHandle,
			FromNode:   fromNode,
			FromBBS:    fromBBS,
			Text:       text,
			Timestamp:  createdAt.Format(time.RFC3339),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("hub: iterate room history: %w", err)
	}

	// Reverse so oldest is first.
	for i, j := 0, len(results)-1; i < j; i, j = i+1, j-1 {
		results[i], results[j] = results[j], results[i]
	}

	return results, nil
}

// prune deletes messages older than retentionDays from both chat history tables.
func (chs *ChatHistoryStore) prune() error {
	cutoff := time.Now().UTC().AddDate(0, 0, -chs.retentionDays)

	if _, err := chs.db.Exec(
		"DELETE FROM chat_history WHERE created_at < ?",
		cutoff,
	); err != nil {
		return fmt.Errorf("hub: prune chat_history: %w", err)
	}

	if _, err := chs.db.Exec(
		"DELETE FROM chat_private_history WHERE created_at < ?",
		cutoff,
	); err != nil {
		return fmt.Errorf("hub: prune chat_private_history: %w", err)
	}

	return nil
}
