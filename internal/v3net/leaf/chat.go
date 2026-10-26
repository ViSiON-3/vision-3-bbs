package leaf

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/chat"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

var (
	_ chat.ChatService = (*ChatSession)(nil)
	_ chat.Pinger      = (*ChatSession)(nil)
)

// chatSessionRegistry tracks active ChatSessions on this leaf. A user can
// have chat open on more than one node, so several sessions may share a
// handle (#662).
type chatSessionRegistry struct {
	mu       sync.RWMutex
	sessions map[*ChatSession]struct{}
	nodeID   string

	// membership serializes Join and Leave requests to the hub. The hub keeps
	// one membership per handle on a node, so a session leaving a room must
	// not tell the hub while another session with its handle is still there,
	// and that check must not race a join by the other session.
	membership sync.Mutex
}

func newChatSessionRegistry(nodeID string) *chatSessionRegistry {
	return &chatSessionRegistry{sessions: make(map[*ChatSession]struct{}), nodeID: nodeID}
}

func (r *chatSessionRegistry) register(s *ChatSession) {
	r.mu.Lock()
	r.sessions[s] = struct{}{}
	r.mu.Unlock()
}

func (r *chatSessionRegistry) deregister(s *ChatSession) {
	r.mu.Lock()
	delete(r.sessions, s)
	r.mu.Unlock()
}

// sharesRoom reports whether a session other than s, with s's handle, is in
// room. The caller holds r.membership.
func (r *chatSessionRegistry) sharesRoom(s *ChatSession, room string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for o := range r.sessions {
		if o != s && o.handle == s.handle && o.room() == room {
			return true
		}
	}
	return false
}

// dispatch applies leaf-side filtering and delivers events to matching sessions.
func (r *chatSessionRegistry) dispatch(ev protocol.Event) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	switch ev.Type {
	case protocol.EventChatMessage, protocol.EventChatJoin,
		protocol.EventChatLeave, protocol.EventChatTopic:
		var msg struct {
			Room       string `json:"room"`
			FromHandle string `json:"from_handle"`
			FromNode   string `json:"from_node"`
			Origin     string `json:"origin"`
		}
		if err := json.Unmarshal(ev.Data, &msg); err != nil {
			return // drop malformed event rather than routing it to the wrong room
		}
		for s := range r.sessions {
			// The hub sends a post back to its author too. The chat screen
			// showed the line when Post returned, so this copy would show it
			// twice (#658).
			if ev.Type == protocol.EventChatMessage && s.ownPost(msg.Origin, msg.FromNode, msg.FromHandle) {
				continue
			}
			// room() takes and releases s.mu before deliver takes it again.
			if s.room() == msg.Room {
				s.deliver(ev)
			}
		}

	case protocol.EventChatPrivate:
		var msg protocol.ChatMsgPayload
		if err := json.Unmarshal(ev.Data, &msg); err != nil {
			return // drop malformed event rather than delivering partial data
		}
		if msg.ToNode != "" && msg.ToNode != r.nodeID {
			return
		}
		for s := range r.sessions {
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
	for s := range r.sessions {
		s.mu.Lock()
		if !s.closed {
			select {
			case s.events <- ev:
			default:
			}
		}
		s.mu.Unlock()
	}
}

// ChatSession is the V3Net-backed implementation of chat.ChatService for one
// user handle. Commands go to the hub as HTTP requests (signed POSTs for
// anything that changes state); incoming chat events reach the session
// through the leaf's chatSessionRegistry and are delivered on Events.
type ChatSession struct {
	leaf         *Leaf
	handle       string
	origin       string // sent with posts to recognise their echo; see ownPost
	currentRoom  string
	currentUsers []string
	events       chan chat.ChatEvent
	mu           sync.Mutex
	closed       bool
}

// room returns the session's current room. Join and Leave change it from
// user goroutines while the SSE loop dispatches events, so it is read under
// s.mu.
func (s *ChatSession) room() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.currentRoom
}

// ownPost reports whether a room message is this session's own post coming
// back from the hub. A hub that relays the post's origin identifies the
// session exactly, so another session of the same user on this BBS still
// gets the line. Otherwise the message is matched on node and handle, as the
// join echo is: users on other BBSes can share a handle.
func (s *ChatSession) ownPost(origin, fromNode, fromHandle string) bool {
	if origin != "" {
		return origin == s.origin
	}
	return fromNode != "" && fromNode == s.leaf.chatSessions.nodeID && fromHandle == s.handle
}

// deliver converts a protocol.Event into a chat.ChatEvent and sends it
// to the session's events channel (non-blocking; drops if full).
func (s *ChatSession) deliver(ev protocol.Event) {
	var ce chat.ChatEvent
	switch ev.Type {
	case protocol.EventChatMessage:
		var p protocol.ChatMsgPayload
		if err := json.Unmarshal(ev.Data, &p); err != nil {
			return // drop malformed event rather than delivering partial data
		}
		ce = chat.ChatEvent{Type: chat.TypeMessage, Message: protoMsgToDomain(p)}
	case protocol.EventChatPrivate:
		var p protocol.ChatMsgPayload
		if err := json.Unmarshal(ev.Data, &p); err != nil {
			return // drop malformed event rather than delivering partial data
		}
		ce = chat.ChatEvent{Type: chat.TypePrivate, Message: protoMsgToDomain(p)}
	case protocol.EventChatJoin:
		var p protocol.ChatJoinPayload
		if err := json.Unmarshal(ev.Data, &p); err != nil {
			return // drop malformed event rather than corrupting the user list
		}
		// The list from Join already counts this session, and the hub also
		// broadcasts that join, so skip the echo of our own join. It is
		// matched on node as well as handle: users on other BBSes can share
		// a handle, and the hub lists each of them.
		ownEcho := p.Handle == s.handle && p.Node != "" && p.Node == s.leaf.chatSessions.nodeID
		if !ownEcho {
			s.mu.Lock()
			s.currentUsers = append(s.currentUsers, p.Handle)
			s.mu.Unlock()
		}
		ce = chat.ChatEvent{Type: chat.TypeJoin, Join: &chat.ChatJoin{Room: p.Room, Handle: p.Handle, BBS: p.BBS}}
	case protocol.EventChatLeave:
		var p protocol.ChatLeavePayload
		if err := json.Unmarshal(ev.Data, &p); err != nil {
			return // drop malformed event rather than corrupting the user list
		}
		s.mu.Lock()
		s.currentUsers = removeOne(s.currentUsers, p.Handle)
		s.mu.Unlock()
		ce = chat.ChatEvent{Type: chat.TypeLeave, Leave: &chat.ChatLeave{Room: p.Room, Handle: p.Handle, BBS: p.BBS}}
	case protocol.EventChatTopic:
		var p protocol.ChatTopicPayload
		if err := json.Unmarshal(ev.Data, &p); err != nil {
			return // drop malformed event rather than delivering partial data
		}
		ce = chat.ChatEvent{Type: chat.TypeTopic, Topic: &chat.ChatTopic{Room: p.Room, Topic: p.Topic, SetBy: p.SetBy}}
	default:
		return
	}
	s.mu.Lock()
	if !s.closed {
		select {
		case s.events <- ce:
		default:
		}
	}
	s.mu.Unlock()
}

func protoMsgToDomain(p protocol.ChatMsgPayload) *chat.ChatMessage {
	t, _ := time.Parse(time.RFC3339, p.Timestamp)
	return &chat.ChatMessage{
		Room: p.Room, Handle: p.FromHandle, Node: p.FromNode,
		BBS: p.FromBBS, Text: p.Text, Timestamp: t,
	}
}

// removeOne returns ss without its first occurrence of s. The user list has
// one entry per membership, so when one of several users sharing a handle
// leaves, the others stay listed.
func removeOne(ss []string, s string) []string {
	if i := slices.Index(ss, s); i >= 0 {
		return slices.Delete(ss, i, i+1)
	}
	return ss
}

// Join implements chat.ChatService. It normalizes room, asks the hub to add
// this handle to it, and on success records it as the current room so hub
// events for that room are delivered here. It returns the hub's room list and
// recent history; a non-2xx hub response is reported as an error.
func (s *ChatSession) Join(room string) ([]chat.RoomInfo, []chat.ChatMessage, error) {
	room, err := chat.NormalizeRoom(room)
	if err != nil {
		return nil, nil, err
	}
	s.leaf.chatSessions.membership.Lock()
	defer s.leaf.chatSessions.membership.Unlock()
	body, _ := json.Marshal(protocol.ChatJoinRequest{Room: room, Handle: s.handle})
	resp, err := s.leaf.signedPostWithResponse(context.Background(),
		fmt.Sprintf("/v3net/v1/%s/chat/rooms/join", s.leaf.cfg.Network), body)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = resp.Body.Close() }() // read side
	if resp.StatusCode/100 != 2 {
		return nil, nil, fmt.Errorf("join chat: hub returned status %d", resp.StatusCode)
	}
	// The hub has joined us to the room and announced it. If its reply can't
	// be read, leave again so the error means "not in the room", as the
	// ChatService contract says.
	respBytes, err := readBody(resp.Body, maxRespBytes)
	if err != nil {
		s.undoJoin(room)
		return nil, nil, fmt.Errorf("join chat: %w", err)
	}
	var joinResp protocol.ChatJoinResponse
	if err := json.Unmarshal(respBytes, &joinResp); err != nil {
		s.undoJoin(room)
		return nil, nil, fmt.Errorf("join chat: %w", err)
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

// undoJoin asks the hub to take this handle out of room after a Join that
// the hub accepted but whose reply could not be used, unless another session
// with this handle is in the room. Unlike Leave it leaves currentRoom alone,
// since the session never switched to room. Best effort: a failure is only
// logged. The caller holds the registry's membership lock.
func (s *ChatSession) undoJoin(room string) {
	if s.leaf.chatSessions.sharesRoom(s, room) {
		return
	}
	body, _ := json.Marshal(protocol.ChatLeaveRequest{Room: room, Handle: s.handle})
	if err := s.leaf.signedPostCtx(context.Background(),
		fmt.Sprintf("/v3net/v1/%s/chat/rooms/leave", s.leaf.cfg.Network), body); err != nil {
		slog.Warn("leaf: could not undo a failed chat join", "network", s.leaf.cfg.Network, "room", room, "error", err)
	}
}

// Leave implements chat.ChatService, telling the hub this handle has left
// room. The current room is cleared only if the hub accepted the request.
// While another session with this handle is still in room, the hub is not
// told, since its membership is shared by both, and the room is just cleared.
func (s *ChatSession) Leave(room string) error {
	s.leaf.chatSessions.membership.Lock()
	defer s.leaf.chatSessions.membership.Unlock()
	var err error
	if !s.leaf.chatSessions.sharesRoom(s, room) {
		body, _ := json.Marshal(protocol.ChatLeaveRequest{Room: room, Handle: s.handle})
		err = s.leaf.signedPostCtx(context.Background(),
			fmt.Sprintf("/v3net/v1/%s/chat/rooms/leave", s.leaf.cfg.Network), body)
	}
	if err == nil {
		s.mu.Lock()
		s.currentRoom = ""
		s.mu.Unlock()
	}
	return err
}

// Post implements chat.ChatService by sending text to room through the hub.
// The hub's copy sent back to this session is dropped by dispatch, so the
// caller shows its own line once, when Post returns.
func (s *ChatSession) Post(room, text string) error {
	body, _ := json.Marshal(protocol.ChatPostRequest{Room: room, Text: text, Handle: s.handle, Origin: s.origin})
	return s.leaf.signedPostCtx(context.Background(),
		fmt.Sprintf("/v3net/v1/%s/chat/rooms/post", s.leaf.cfg.Network), body)
}

// Private implements chat.ChatService by asking the hub to deliver text
// privately to handle on the given node.
func (s *ChatSession) Private(handle, node, text string) error {
	body, _ := json.Marshal(protocol.ChatPrivateRequest{ToHandle: handle, ToNode: node, Text: text, Handle: s.handle})
	return s.leaf.signedPostCtx(context.Background(),
		fmt.Sprintf("/v3net/v1/%s/chat/rooms/private", s.leaf.cfg.Network), body)
}

// SetTopic implements chat.ChatService by asking the hub to change room's
// topic; the change arrives back as a topic event.
func (s *ChatSession) SetTopic(room, topic string) error {
	body, _ := json.Marshal(protocol.ChatTopicRequest{Room: room, Topic: topic, Handle: s.handle})
	return s.leaf.signedPostCtx(context.Background(),
		fmt.Sprintf("/v3net/v1/%s/chat/rooms/topic", s.leaf.cfg.Network), body)
}

// Rooms implements chat.ChatService, fetching the network's active rooms
// from the hub on every call.
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

// History implements chat.ChatService, fetching up to limit recent messages
// for room from the hub. The limit is passed through; the hub enforces any cap.
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

// Users implements chat.ChatService, returning a copy of the handles in the
// current room: the list from Join, kept up to date by join and leave events.
func (s *ChatSession) Users() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.currentUsers))
	copy(out, s.currentUsers)
	return out
}

// Events implements chat.ChatService. Delivery is non-blocking, so events are
// dropped when the channel's buffer is full. The channel is closed by Close.
func (s *ChatSession) Events() <-chan chat.ChatEvent { return s.events }

// Close implements chat.ChatService. It closes the Events channel and removes
// the session from the leaf's registry; it does not send a leave to the hub.
// Calling it more than once is safe and always returns nil.
func (s *ChatSession) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	close(s.events)
	s.mu.Unlock()
	s.leaf.chatSessions.deregister(s)
	return nil
}

// Ping implements chat.Pinger. It times one request to the hub, a GET of the
// network's public room list, which needs no signature and is small, and
// returns the round trip including reading the reply. A transport error or a
// non-200 status is returned as an error, with no duration. ctx bounds the
// request; the leaf's client also times it out after 10 seconds.
func (s *ChatSession) Ping(ctx context.Context) (time.Duration, error) {
	path := fmt.Sprintf("/v3net/v1/%s/chat/rooms", s.leaf.cfg.Network)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.leaf.cfg.HubURL+path, nil)
	if err != nil {
		return 0, fmt.Errorf("leaf: create ping request: %w", err)
	}
	start := time.Now()
	resp, err := s.leaf.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("leaf: ping: %w", err)
	}
	defer func() { _ = resp.Body.Close() }() // read side
	if _, err := readBody(resp.Body, maxRespBytes); err != nil {
		return 0, fmt.Errorf("leaf: ping: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("leaf: ping: status %d", resp.StatusCode)
	}
	return time.Since(start), nil
}
