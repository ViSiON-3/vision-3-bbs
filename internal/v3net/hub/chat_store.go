package hub

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
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
	if retentionDays <= 0 {
		return nil, fmt.Errorf("hub: retentionDays must be > 0, got %d", retentionDays)
	}

	if _, err := db.Exec(chatSchema); err != nil {
		return nil, fmt.Errorf("hub: create chat history tables: %w", err)
	}
	// Signed chat (#1008): a room line's signed form, kept so a board that
	// joins later can check history as it checks live lines. Hubs from
	// before it have the table without the column.
	if err := addColumnIfMissing(db, "chat_history", "signed", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return nil, err
	}

	store := &ChatHistoryStore{
		db:            db,
		retentionDays: retentionDays,
	}

	return store, nil
}

// addColumnIfMissing adds column (with its declaration) to table unless
// it is there already.
func addColumnIfMissing(db *sql.DB, table, column, decl string) error {
	rows, err := db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return fmt.Errorf("hub: read %s columns: %w", table, err)
	}
	found := false
	for rows.Next() {
		var cid, notNull, pk int
		var name, typ string
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dflt, &pk); err != nil {
			_ = rows.Close() // read-only
			return fmt.Errorf("hub: read %s columns: %w", table, err)
		}
		if name == column {
			found = true
		}
	}
	_ = rows.Close() // read-only
	if err := rows.Err(); err != nil {
		return fmt.Errorf("hub: read %s columns: %w", table, err)
	}
	if found {
		return nil
	}
	if _, err := db.Exec("ALTER TABLE " + table + " ADD COLUMN " + column + " " + decl); err != nil {
		return fmt.Errorf("hub: add %s.%s: %w", table, column, err)
	}
	return nil
}

// SaveMessage saves a public room message to chat_history, with its signed
// form when the sending board signed it.
func (chs *ChatHistoryStore) SaveMessage(network, room, fromHandle, fromNode, fromBBS, text string, signed *protocol.ChatSigned) error {
	var signedJSON string
	if signed != nil {
		b, err := json.Marshal(signed)
		if err != nil {
			return fmt.Errorf("hub: encode signed chat: %w", err)
		}
		signedJSON = string(b)
	}
	_, err := chs.db.Exec(
		`INSERT INTO chat_history (network, room, from_handle, from_node, from_bbs, text, created_at, signed)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		network, room, fromHandle, fromNode, fromBBS, text, time.Now().UTC(), signedJSON,
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
		`SELECT from_handle, from_node, from_bbs, text, created_at, signed
		 FROM chat_history
		 WHERE network = ? AND room = ?
		 ORDER BY created_at DESC
		 LIMIT ?`,
		network, room, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("hub: query room history: %w", err)
	}
	defer func() { _ = rows.Close() }() // read-only

	var results []protocol.ChatMsgPayload
	for rows.Next() {
		var fromHandle, fromNode, fromBBS, text, signedJSON string
		var createdAt time.Time
		if err := rows.Scan(&fromHandle, &fromNode, &fromBBS, &text, &createdAt, &signedJSON); err != nil {
			return nil, fmt.Errorf("hub: scan room history row: %w", err)
		}
		msg := protocol.ChatMsgPayload{
			Room:       room,
			FromHandle: fromHandle,
			FromNode:   fromNode,
			FromBBS:    fromBBS,
			Text:       text,
			Timestamp:  createdAt.Format(time.RFC3339),
		}
		if signedJSON != "" {
			var sc protocol.ChatSigned
			if json.Unmarshal([]byte(signedJSON), &sc) == nil {
				msg.Signed = &sc
			}
		}
		results = append(results, msg)
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
	if _, err := chs.db.Exec("DELETE FROM chat_history WHERE created_at < ?", cutoff); err != nil {
		return fmt.Errorf("hub: prune chat_history: %w", err)
	}
	if _, err := chs.db.Exec("DELETE FROM chat_private_history WHERE created_at < ?", cutoff); err != nil {
		return fmt.Errorf("hub: prune chat_private_history: %w", err)
	}
	return nil
}

// StartPruner runs a background goroutine that prunes old messages daily.
// It stops when ctx is cancelled.
func (chs *ChatHistoryStore) StartPruner(ctx context.Context) {
	go func() {
		// Run once at startup.
		if err := chs.prune(); err != nil {
			slog.Error("hub: chat history prune", "error", err)
		}
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := chs.prune(); err != nil {
					slog.Error("hub: chat history prune", "error", err)
				}
			}
		}
	}()
}
