package hub

import (
	"encoding/json"
	"sort"
	"sync"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

type roomState struct {
	topic   string
	members map[string][]string // nodeID → []handle
}

// chatRooms is the in-memory ephemeral room registry.
type chatRooms struct {
	mu    sync.RWMutex
	rooms map[string]*roomState // room name → state
}

func newChatRooms() *chatRooms {
	return &chatRooms{rooms: make(map[string]*roomState)}
}

// Join adds handle@nodeID to room, creating the room if needed.
// Returns the current user list for that room.
func (cr *chatRooms) Join(room, nodeID, handle string) []string {
	cr.mu.Lock()
	defer cr.mu.Unlock()
	rs := cr.rooms[room]
	if rs == nil {
		rs = &roomState{members: make(map[string][]string)}
		cr.rooms[room] = rs
	}
	// Avoid duplicate handles for the same node.
	for _, h := range rs.members[nodeID] {
		if h == handle {
			return cr.usersLocked(room)
		}
	}
	rs.members[nodeID] = append(rs.members[nodeID], handle)
	return cr.usersLocked(room)
}

// Leave removes handle@nodeID from room. Deletes room if empty.
func (cr *chatRooms) Leave(room, nodeID, handle string) {
	cr.mu.Lock()
	defer cr.mu.Unlock()
	rs := cr.rooms[room]
	if rs == nil {
		return
	}
	handles := rs.members[nodeID]
	for i, h := range handles {
		if h == handle {
			rs.members[nodeID] = append(handles[:i], handles[i+1:]...)
			break
		}
	}
	if len(rs.members[nodeID]) == 0 {
		delete(rs.members, nodeID)
	}
	if len(rs.members) == 0 {
		delete(cr.rooms, room)
	}
}

// SetTopic updates the topic for room (creates room if needed).
func (cr *chatRooms) SetTopic(room, topic string) {
	cr.mu.Lock()
	defer cr.mu.Unlock()
	rs := cr.rooms[room]
	if rs == nil {
		rs = &roomState{members: make(map[string][]string)}
		cr.rooms[room] = rs
	}
	rs.topic = topic
}

// IsJoined reports whether handle@nodeID is in room.
func (cr *chatRooms) IsJoined(room, nodeID, handle string) bool {
	cr.mu.RLock()
	defer cr.mu.RUnlock()
	rs := cr.rooms[room]
	if rs == nil {
		return false
	}
	for _, h := range rs.members[nodeID] {
		if h == handle {
			return true
		}
	}
	return false
}

// RoomList returns all active rooms sorted by name.
func (cr *chatRooms) RoomList() []protocol.ProtoChatRoomInfo {
	cr.mu.RLock()
	defer cr.mu.RUnlock()
	var list []protocol.ProtoChatRoomInfo
	for name, rs := range cr.rooms {
		count := 0
		for _, handles := range rs.members {
			count += len(handles)
		}
		list = append(list, protocol.ProtoChatRoomInfo{
			Name:      name,
			Topic:     rs.topic,
			UserCount: count,
		})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	return list
}

// Users returns all handles currently in room.
func (cr *chatRooms) Users(room string) []string {
	cr.mu.RLock()
	defer cr.mu.RUnlock()
	return cr.usersLocked(room)
}

// usersLocked returns handles in room; caller must hold mu.
func (cr *chatRooms) usersLocked(room string) []string {
	rs := cr.rooms[room]
	if rs == nil {
		return nil
	}
	var out []string
	for _, handles := range rs.members {
		out = append(out, handles...)
	}
	return out
}

// HandleDisconnect removes all handles for nodeID from all rooms.
// Returns a slice of (room, handle) pairs that were removed so the
// caller can broadcast chat_leave events.
func (cr *chatRooms) HandleDisconnect(nodeID string) [][2]string {
	cr.mu.Lock()
	defer cr.mu.Unlock()
	var removed [][2]string
	for roomName, rs := range cr.rooms {
		for _, handle := range rs.members[nodeID] {
			removed = append(removed, [2]string{roomName, handle})
		}
		delete(rs.members, nodeID)
		if len(rs.members) == 0 {
			delete(cr.rooms, roomName)
		}
	}
	return removed
}

// broadcastChatEvent is a helper to publish a chat SSE event via a Broadcaster.
func broadcastChatEvent(b *Broadcaster, network, eventType string, payload any) {
	data, _ := json.Marshal(payload)
	b.Publish(network, protocol.Event{Type: eventType, Data: data})
}

// HandleForNode returns the first handle that nodeID has in room, or "".
func (cr *chatRooms) HandleForNode(room, nodeID string) string {
	cr.mu.RLock()
	defer cr.mu.RUnlock()
	rs := cr.rooms[room]
	if rs == nil {
		return ""
	}
	handles := rs.members[nodeID]
	if len(handles) == 0 {
		return ""
	}
	return handles[0]
}

// AnyHandleForNode returns any handle that nodeID has across any room, or "".
func (cr *chatRooms) AnyHandleForNode(nodeID string) string {
	cr.mu.RLock()
	defer cr.mu.RUnlock()
	for _, rs := range cr.rooms {
		if handles := rs.members[nodeID]; len(handles) > 0 {
			return handles[0]
		}
	}
	return ""
}
