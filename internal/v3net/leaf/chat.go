package leaf

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/chat"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

var _ chat.ChatService = (*ChatSession)(nil)

// chatSessionRegistry tracks active ChatSessions on this leaf.
type chatSessionRegistry struct {
	mu       sync.RWMutex
	sessions map[string]*ChatSession // handle → session
}

func newChatSessionRegistry() *chatSessionRegistry {
	return &chatSessionRegistry{sessions: make(map[string]*ChatSession)}
}

func (r *chatSessionRegistry) register(s *ChatSession) {
	r.mu.Lock()
	r.sessions[s.handle] = s
	r.mu.Unlock()
}

func (r *chatSessionRegistry) deregister(handle string) {
	r.mu.Lock()
	delete(r.sessions, handle)
	r.mu.Unlock()
}

// dispatch applies leaf-side filtering and delivers events to matching sessions.
func (r *chatSessionRegistry) dispatch(ev protocol.Event) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	switch ev.Type {
	case protocol.EventChatMessage, protocol.EventChatJoin,
		protocol.EventChatLeave, protocol.EventChatTopic:
		var msg struct {
			Room string `json:"room"`
		}
		json.Unmarshal(ev.Data, &msg)
		for _, s := range r.sessions {
			if s.currentRoom == msg.Room {
				s.deliver(ev)
			}
		}

	case protocol.EventChatPrivate:
		var msg protocol.ChatMsgPayload
		json.Unmarshal(ev.Data, &msg)
		for _, s := range r.sessions {
			if s.handle == msg.ToHandle {
				s.deliver(ev)
			}
		}
	}
}

// notifyReconnect sends a TypeSystem reconnect event to all active sessions.
func (r *chatSessionRegistry) notifyReconnect() {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ev := chat.ChatEvent{Type: chat.TypeSystem, Reconnect: true, Text: "reconnected"}
	for _, s := range r.sessions {
		select {
		case s.events <- ev:
		default:
		}
	}
}

// ChatSession is the V3Net-backed implementation of chat.ChatService.
// ChatService methods (Join, Leave, Post, etc.) will be added in Task 8.
type ChatSession struct {
	leaf         *Leaf
	handle       string
	currentRoom  string
	currentUsers []string
	events       chan chat.ChatEvent
	mu           sync.Mutex
	closed       bool
}

// deliver converts a protocol.Event into a chat.ChatEvent and sends it
// to the session's events channel (non-blocking; drops if full).
func (s *ChatSession) deliver(ev protocol.Event) {
	var ce chat.ChatEvent
	switch ev.Type {
	case protocol.EventChatMessage:
		var p protocol.ChatMsgPayload
		json.Unmarshal(ev.Data, &p)
		ce = chat.ChatEvent{Type: chat.TypeMessage, Message: protoMsgToDomain(p)}
	case protocol.EventChatPrivate:
		var p protocol.ChatMsgPayload
		json.Unmarshal(ev.Data, &p)
		ce = chat.ChatEvent{Type: chat.TypePrivate, Message: protoMsgToDomain(p)}
	case protocol.EventChatJoin:
		var p protocol.ChatJoinPayload
		json.Unmarshal(ev.Data, &p)
		s.mu.Lock()
		s.currentUsers = append(s.currentUsers, p.Handle)
		s.mu.Unlock()
		ce = chat.ChatEvent{Type: chat.TypeJoin, Join: &chat.ChatJoin{Room: p.Room, Handle: p.Handle, BBS: p.BBS}}
	case protocol.EventChatLeave:
		var p protocol.ChatLeavePayload
		json.Unmarshal(ev.Data, &p)
		s.mu.Lock()
		s.currentUsers = removeString(s.currentUsers, p.Handle)
		s.mu.Unlock()
		ce = chat.ChatEvent{Type: chat.TypeLeave, Leave: &chat.ChatLeave{Room: p.Room, Handle: p.Handle, BBS: p.BBS}}
	case protocol.EventChatTopic:
		var p protocol.ChatTopicPayload
		json.Unmarshal(ev.Data, &p)
		ce = chat.ChatEvent{Type: chat.TypeTopic, Topic: &chat.ChatTopic{Room: p.Room, Topic: p.Topic, SetBy: p.SetBy}}
	default:
		return
	}
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return
	}
	select {
	case s.events <- ce:
	default:
	}
}

func protoMsgToDomain(p protocol.ChatMsgPayload) *chat.ChatMessage {
	t, _ := time.Parse(time.RFC3339, p.Timestamp)
	return &chat.ChatMessage{
		Room: p.Room, Handle: p.FromHandle, Node: p.FromNode,
		BBS: p.FromBBS, Text: p.Text, Timestamp: t,
	}
}

func removeString(ss []string, s string) []string {
	out := ss[:0]
	for _, v := range ss {
		if v != s {
			out = append(out, v)
		}
	}
	return out
}

func (s *ChatSession) Join(room string) ([]chat.RoomInfo, []chat.ChatMessage, error) {
	room, err := chat.NormalizeRoom(room)
	if err != nil {
		return nil, nil, err
	}
	body, _ := json.Marshal(protocol.ChatJoinRequest{Room: room, Handle: s.handle})
	resp, err := s.leaf.signedPostWithResponse(context.Background(),
		fmt.Sprintf("/v3net/v1/%s/chat/rooms/join", s.leaf.cfg.Network), body)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("join chat: hub returned status %d", resp.StatusCode)
	}
	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, err
	}
	var joinResp protocol.ChatJoinResponse
	if err := json.Unmarshal(respBytes, &joinResp); err != nil {
		return nil, nil, err
	}
	s.mu.Lock()
	s.currentRoom = room
	s.currentUsers = joinResp.Users
	s.mu.Unlock()

	rooms := make([]chat.RoomInfo, len(joinResp.Rooms))
	for i, r := range joinResp.Rooms {
		rooms[i] = chat.RoomInfo{Name: r.Name, Topic: r.Topic, UserCount: r.UserCount}
	}
	msgs := make([]chat.ChatMessage, len(joinResp.History))
	for i, m := range joinResp.History {
		msgs[i] = *protoMsgToDomain(m)
		msgs[i].Room = room
	}
	return rooms, msgs, nil
}

func (s *ChatSession) Leave(room string) error {
	body, _ := json.Marshal(protocol.ChatLeaveRequest{Room: room, Handle: s.handle})
	err := s.leaf.signedPostCtx(context.Background(),
		fmt.Sprintf("/v3net/v1/%s/chat/rooms/leave", s.leaf.cfg.Network), body)
	if err == nil {
		s.mu.Lock()
		s.currentRoom = ""
		s.mu.Unlock()
	}
	return err
}

func (s *ChatSession) Post(room, text string) error {
	body, _ := json.Marshal(protocol.ChatPostRequest{Room: room, Text: text})
	return s.leaf.signedPostCtx(context.Background(),
		fmt.Sprintf("/v3net/v1/%s/chat/rooms/post", s.leaf.cfg.Network), body)
}

func (s *ChatSession) Private(handle, node, text string) error {
	body, _ := json.Marshal(protocol.ChatPrivateRequest{ToHandle: handle, ToNode: node, Text: text})
	return s.leaf.signedPostCtx(context.Background(),
		fmt.Sprintf("/v3net/v1/%s/chat/rooms/private", s.leaf.cfg.Network), body)
}

func (s *ChatSession) SetTopic(room, topic string) error {
	body, _ := json.Marshal(protocol.ChatTopicRequest{Room: room, Topic: topic})
	return s.leaf.signedPostCtx(context.Background(),
		fmt.Sprintf("/v3net/v1/%s/chat/rooms/topic", s.leaf.cfg.Network), body)
}

func (s *ChatSession) Rooms() ([]chat.RoomInfo, error) {
	data, err := s.leaf.get(fmt.Sprintf("/v3net/v1/%s/chat/rooms", s.leaf.cfg.Network))
	if err != nil {
		return nil, err
	}
	var protoRooms []protocol.ProtoChatRoomInfo
	if err := json.Unmarshal(data, &protoRooms); err != nil {
		return nil, err
	}
	rooms := make([]chat.RoomInfo, len(protoRooms))
	for i, r := range protoRooms {
		rooms[i] = chat.RoomInfo{Name: r.Name, Topic: r.Topic, UserCount: r.UserCount}
	}
	return rooms, nil
}

func (s *ChatSession) History(room string, limit int) ([]chat.ChatMessage, error) {
	url := fmt.Sprintf("/v3net/v1/%s/chat/rooms/%s/history?limit=%d",
		s.leaf.cfg.Network, room, limit)
	data, err := s.leaf.get(url)
	if err != nil {
		return nil, err
	}
	var protoMsgs []protocol.ChatMsgPayload
	if err := json.Unmarshal(data, &protoMsgs); err != nil {
		return nil, err
	}
	msgs := make([]chat.ChatMessage, len(protoMsgs))
	for i, m := range protoMsgs {
		msgs[i] = *protoMsgToDomain(m)
	}
	return msgs, nil
}

func (s *ChatSession) Users() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.currentUsers))
	copy(out, s.currentUsers)
	return out
}

func (s *ChatSession) Events() <-chan chat.ChatEvent { return s.events }

func (s *ChatSession) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.mu.Unlock()
	s.leaf.chatSessions.deregister(s.handle)
	close(s.events)
	return nil
}
