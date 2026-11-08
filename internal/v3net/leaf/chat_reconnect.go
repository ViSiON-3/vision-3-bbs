package leaf

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/ViSiON-3/vision-3-bbs/internal/chat"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

type chatMembership struct{ room, handle string }

// suspend preserves room intent, but stops posts until a new stream has
// restored it. Repeated failed reconnects do not repeat the disconnect notice.
func (r *chatSessionRegistry) suspend() {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for s := range r.sessions {
		s.mu.Lock()
		if !s.closed && s.currentRoom != "" && !s.recovering {
			s.recovering = true
			select {
			case s.events <- chat.ChatEvent{Type: chat.TypeSystem, Text: "Chat disconnected; restoring room membership before posting."}:
			default:
			}
		}
		s.mu.Unlock()
	}
}

func (s *ChatSession) chatReady() error {
	if err := s.chatAllowed(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return fmt.Errorf("chat session closed")
	}
	if s.recovering {
		return fmt.Errorf("chat room is reconnecting; membership is not restored yet")
	}
	return nil
}

// skipRejoinEcho consumes one local rejoin announcement for all sessions.
// The final recovery snapshot already counts those memberships. Pending
// echoes belong to one stream and are discarded when the next recovery starts.
func (r *chatSessionRegistry) skipRejoinEcho(ev protocol.Event) bool {
	if ev.Type != protocol.EventChatJoin {
		return false
	}
	var p protocol.ChatJoinPayload
	if json.Unmarshal(ev.Data, &p) != nil || p.Node != r.nodeID {
		return false
	}
	key := chatMembership{p.Room, p.Handle}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.rejoinEchoes[key] {
		return false
	}
	delete(r.rejoinEchoes, key)
	return true
}

// activeMembership rechecks intent after a request, since Close and a Leave
// during recovery can abandon it while the request is in flight.
func (r *chatSessionRegistry) activeMembership(key chatMembership) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for s := range r.sessions {
		s.mu.Lock()
		active := !s.closed && s.currentRoom == key.room && s.handle == key.handle
		s.mu.Unlock()
		if active {
			return true
		}
	}
	return false
}

func roomTopic(rooms []protocol.ProtoChatRoomInfo, room string) string {
	for _, r := range rooms {
		if r.Name == room {
			return r.Topic
		}
	}
	return ""
}

// recoverChat restores each unique room/handle before reading the new stream.
// Requests use the stream context, signed HTTP path, and normal response
// verification. Any failure ends this stream and the SSE loop retries with
// backoff; no session is announced ready after a partial recovery.
func (l *Leaf) recoverChat(ctx context.Context) error {
	r := l.chatSessions
	r.membership.Lock()
	defer r.membership.Unlock()
	r.mu.Lock()
	r.rejoinEchoes = make(map[chatMembership]bool)
	groups := make(map[chatMembership]bool)
	for s := range r.sessions {
		s.mu.Lock()
		if !s.closed && s.currentRoom != "" {
			s.recovering = true
			groups[chatMembership{s.currentRoom, s.handle}] = true
		}
		s.mu.Unlock()
	}
	r.mu.Unlock()
	snapshots := make(map[string]protocol.ChatJoinResponse)
	restored := make(map[chatMembership]bool)
	for key := range groups {
		if !r.activeMembership(key) {
			continue
		}
		r.mu.Lock()
		r.rejoinEchoes[key] = true
		r.mu.Unlock()
		joined, err := l.rejoinChat(ctx, key)
		if err != nil {
			if ctx.Err() == nil {
				r.recoveryFailed(err)
			}
			return err
		}
		restored[key] = true
		if !r.activeMembership(key) {
			continue
		}
		// The last join in each room includes all handles restored so far.
		snapshots[key.room] = joined
	}
	// A session restored early can close while a later group is in flight.
	for key := range restored {
		if !r.activeMembership(key) {
			body, _ := json.Marshal(protocol.ChatLeaveRequest{Room: key.room, Handle: key.handle})
			if err := l.signedPostCtx(ctx, fmt.Sprintf("/v3net/v1/%s/chat/rooms/leave", l.cfg.Network), body); err != nil {
				if ctx.Err() == nil {
					r.recoveryFailed(err)
				}
				return err
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.RLock()
	for s := range r.sessions {
		s.mu.Lock()
		if joined, ok := snapshots[s.currentRoom]; ok && !s.closed {
			s.currentUsers = slices.Clone(joined.Users)
			s.currentTopic = roomTopic(joined.Rooms, s.currentRoom)
			s.recovering = false
		}
		s.mu.Unlock()
	}
	r.mu.RUnlock()
	r.notifyReconnect()
	return nil
}

func (l *Leaf) rejoinChat(ctx context.Context, key chatMembership) (protocol.ChatJoinResponse, error) {
	var joined protocol.ChatJoinResponse
	body, _ := json.Marshal(protocol.ChatJoinRequest{Room: key.room, Handle: key.handle})
	resp, err := l.signedPostWithResponse(ctx, fmt.Sprintf("/v3net/v1/%s/chat/rooms/join", l.cfg.Network), body)
	if err != nil {
		return joined, err
	}
	defer func() { _ = resp.Body.Close() }() // read side
	if resp.StatusCode/100 != 2 {
		return joined, fmt.Errorf("restore chat %s: hub returned status %d", key.room, resp.StatusCode)
	}
	data, err := readBody(resp.Body, maxRespBytes)
	if err != nil {
		return joined, err
	}
	err = json.Unmarshal(data, &joined)
	return joined, err
}

func (r *chatSessionRegistry) recoveryFailed(err error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for s := range r.sessions {
		s.mu.Lock()
		if !s.closed && s.currentRoom != "" {
			select {
			case s.events <- chat.ChatEvent{Type: chat.TypeSystem, Text: "Chat room recovery failed; will retry: " + err.Error()}:
			default:
			}
		}
		s.mu.Unlock()
	}
}
