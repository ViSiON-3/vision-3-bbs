package chat

import (
	"database/sql"
	"fmt"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// compile-time check
var _ ChatService = (*LocalChatService)(nil)

const localChatSchema = `
CREATE TABLE IF NOT EXISTS chat_history (
	id          INTEGER PRIMARY KEY,
	room        TEXT NOT NULL,
	from_handle TEXT NOT NULL,
	text        TEXT NOT NULL,
	created_at  DATETIME NOT NULL
);
CREATE TABLE IF NOT EXISTS chat_private_history (
	id          INTEGER PRIMARY KEY,
	from_handle TEXT NOT NULL,
	to_handle   TEXT NOT NULL,
	text        TEXT NOT NULL,
	created_at  DATETIME NOT NULL
);
`

// LocalChatService is a single-BBS chat backend backed by SQLite.
type LocalChatService struct {
	handle string
	db     *sql.DB

	mu           sync.RWMutex
	currentRoom  string
	currentUsers []string
	events       chan ChatEvent
}

type localRoom struct {
	topic    string
	sessions map[string]*LocalChatService // handle → session
}

// sharedRooms is a process-level registry so all LocalChatService instances
// on the same BBS share room state (simulating the networked behaviour).
var (
	sharedMu    sync.RWMutex
	sharedRooms = make(map[string]*localRoom)
)

// NewLocalChatService opens (or creates) the local chat SQLite database.
func NewLocalChatService(handle, dbPath string) (*LocalChatService, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("local chat: open db: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.Exec("PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000")
	if _, err := db.Exec(localChatSchema); err != nil {
		db.Close()
		return nil, fmt.Errorf("local chat: create tables: %w", err)
	}
	pruneLocalHistory(db, 7)
	return &LocalChatService{
		handle: handle,
		db:     db,
		events: make(chan ChatEvent, 64),
	}, nil
}

func (s *LocalChatService) Join(room string) ([]RoomInfo, []ChatMessage, error) {
	room, err := NormalizeRoom(room)
	if err != nil {
		return nil, nil, err
	}
	sharedMu.Lock()
	rs := sharedRooms[room]
	if rs == nil {
		rs = &localRoom{sessions: make(map[string]*LocalChatService)}
		sharedRooms[room] = rs
	}
	rs.sessions[s.handle] = s
	sharedMu.Unlock()

	s.mu.Lock()
	s.currentRoom = room
	s.currentUsers = localUsersInRoom(room)
	s.mu.Unlock()

	broadcastLocalEvent(room, s.handle, ChatEvent{
		Type: TypeJoin, Join: &ChatJoin{Room: room, Handle: s.handle},
	})

	rooms := localRoomList()
	history, err := s.History(room, 50)
	return rooms, history, err
}

func (s *LocalChatService) Leave(room string) error {
	sharedMu.Lock()
	if rs := sharedRooms[room]; rs != nil {
		delete(rs.sessions, s.handle)
		if len(rs.sessions) == 0 {
			delete(sharedRooms, room)
		}
	}
	sharedMu.Unlock()

	s.mu.Lock()
	s.currentRoom = ""
	s.mu.Unlock()

	broadcastLocalEvent(room, s.handle, ChatEvent{
		Type: TypeLeave, Leave: &ChatLeave{Room: room, Handle: s.handle},
	})
	return nil
}

func (s *LocalChatService) Post(room, text string) error {
	_, err := s.db.Exec(
		`INSERT INTO chat_history(room,from_handle,text,created_at) VALUES(?,?,?,?)`,
		room, s.handle, text, time.Now().UTC(),
	)
	if err != nil {
		return err
	}
	msg := ChatMessage{Room: room, Handle: s.handle, Text: text, Timestamp: time.Now()}
	broadcastLocalEvent(room, s.handle, ChatEvent{Type: TypeMessage, Message: &msg})
	return nil
}

func (s *LocalChatService) Private(handle, _ string, text string) error {
	_, err := s.db.Exec(
		`INSERT INTO chat_private_history(from_handle,to_handle,text,created_at) VALUES(?,?,?,?)`,
		s.handle, handle, text, time.Now().UTC(),
	)
	if err != nil {
		return err
	}
	msg := ChatMessage{Handle: s.handle, Text: text, Timestamp: time.Now()}
	sharedMu.RLock()
	for _, rs := range sharedRooms {
		if target, ok := rs.sessions[handle]; ok {
			select {
			case target.events <- ChatEvent{Type: TypePrivate, Message: &msg}:
			default:
			}
		}
	}
	sharedMu.RUnlock()
	return nil
}

func (s *LocalChatService) SetTopic(room, topic string) error {
	sharedMu.Lock()
	if rs := sharedRooms[room]; rs != nil {
		rs.topic = topic
	}
	sharedMu.Unlock()
	broadcastLocalEvent(room, "", ChatEvent{
		Type:  TypeTopic,
		Topic: &ChatTopic{Room: room, Topic: topic, SetBy: s.handle},
	})
	return nil
}

func (s *LocalChatService) Rooms() ([]RoomInfo, error) {
	return localRoomList(), nil
}

func (s *LocalChatService) History(room string, limit int) ([]ChatMessage, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.db.Query(
		`SELECT from_handle, text, created_at FROM chat_history
		 WHERE room=? ORDER BY created_at DESC LIMIT ?`, room, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var msgs []ChatMessage
	for rows.Next() {
		var m ChatMessage
		var ts time.Time
		if err := rows.Scan(&m.Handle, &m.Text, &ts); err != nil {
			return nil, err
		}
		m.Room, m.Timestamp = room, ts
		msgs = append(msgs, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// reverse to oldest-first
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}
	return msgs, nil
}

func (s *LocalChatService) Users() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, len(s.currentUsers))
	copy(out, s.currentUsers)
	return out
}

func (s *LocalChatService) Events() <-chan ChatEvent { return s.events }

func (s *LocalChatService) Close() error {
	if room := func() string {
		s.mu.RLock()
		defer s.mu.RUnlock()
		return s.currentRoom
	}(); room != "" {
		s.Leave(room)
	}
	close(s.events)
	return s.db.Close()
}

// --- package-level helpers ---

func localRoomList() []RoomInfo {
	sharedMu.RLock()
	defer sharedMu.RUnlock()
	var list []RoomInfo
	for name, rs := range sharedRooms {
		list = append(list, RoomInfo{Name: name, Topic: rs.topic, UserCount: len(rs.sessions)})
	}
	return list
}

func localUsersInRoom(room string) []string {
	sharedMu.RLock()
	defer sharedMu.RUnlock()
	rs := sharedRooms[room]
	if rs == nil {
		return nil
	}
	var out []string
	for h := range rs.sessions {
		out = append(out, h)
	}
	return out
}

func broadcastLocalEvent(room, senderHandle string, ev ChatEvent) {
	sharedMu.RLock()
	defer sharedMu.RUnlock()
	rs := sharedRooms[room]
	if rs == nil {
		return
	}
	for handle, s := range rs.sessions {
		if handle == senderHandle {
			continue
		}
		select {
		case s.events <- ev:
		default:
		}
	}
}

func pruneLocalHistory(db *sql.DB, days int) {
	cutoff := time.Now().UTC().AddDate(0, 0, -days)
	db.Exec(`DELETE FROM chat_history WHERE created_at < ?`, cutoff)
	db.Exec(`DELETE FROM chat_private_history WHERE created_at < ?`, cutoff)
}
