package leaf

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/chat"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

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
