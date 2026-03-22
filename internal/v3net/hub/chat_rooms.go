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

// chatRooms is the in-memory ephemeral room registry, scoped per network.
type chatRooms struct {
	mu       sync.RWMutex
	networks map[string]map[string]*roomState // network → room → state
}

func newChatRooms() *chatRooms {
	return &chatRooms{networks: make(map[string]map[string]*roomState)}
}

// roomsFor returns the room map for network, creating it if needed.
// Caller must hold mu (write lock).
func (cr *chatRooms) roomsFor(network string) map[string]*roomState {
	rooms := cr.networks[network]
	if rooms == nil {
		rooms = make(map[string]*roomState)
		cr.networks[network] = rooms
	}
	return rooms
}

func (cr *chatRooms) Join(network, room, nodeID, handle string) []string {
	cr.mu.Lock()
	defer cr.mu.Unlock()
	rooms := cr.roomsFor(network)
	rs := rooms[room]
	if rs == nil {
		rs = &roomState{members: make(map[string][]string)}
		rooms[room] = rs
	}
	for _, h := range rs.members[nodeID] {
		if h == handle {
			return cr.usersLocked(network, room)
		}
	}
	rs.members[nodeID] = append(rs.members[nodeID], handle)
	return cr.usersLocked(network, room)
}

func (cr *chatRooms) Leave(network, room, nodeID, handle string) {
	cr.mu.Lock()
	defer cr.mu.Unlock()
	rooms := cr.networks[network]
	if rooms == nil {
		return
	}
	rs := rooms[room]
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
		delete(rooms, room)
		if len(rooms) == 0 {
			delete(cr.networks, network)
		}
	}
}

func (cr *chatRooms) SetTopic(network, room, topic string) {
	cr.mu.Lock()
	defer cr.mu.Unlock()
	rooms := cr.roomsFor(network)
	rs := rooms[room]
	if rs == nil {
		rs = &roomState{members: make(map[string][]string)}
		rooms[room] = rs
	}
	rs.topic = topic
}

func (cr *chatRooms) IsJoined(network, room, nodeID, handle string) bool {
	cr.mu.RLock()
	defer cr.mu.RUnlock()
	rooms := cr.networks[network]
	if rooms == nil {
		return false
	}
	rs := rooms[room]
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

func (cr *chatRooms) RoomList(network string) []protocol.ProtoChatRoomInfo {
	cr.mu.RLock()
	defer cr.mu.RUnlock()
	rooms := cr.networks[network]
	var list []protocol.ProtoChatRoomInfo
	for name, rs := range rooms {
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

func (cr *chatRooms) Users(network, room string) []string {
	cr.mu.RLock()
	defer cr.mu.RUnlock()
	return cr.usersLocked(network, room)
}

func (cr *chatRooms) usersLocked(network, room string) []string {
	rooms := cr.networks[network]
	if rooms == nil {
		return nil
	}
	rs := rooms[room]
	if rs == nil {
		return nil
	}
	var out []string
	for _, handles := range rs.members {
		out = append(out, handles...)
	}
	return out
}

func (cr *chatRooms) HandleDisconnect(network, nodeID string) [][2]string {
	cr.mu.Lock()
	defer cr.mu.Unlock()
	rooms := cr.networks[network]
	if rooms == nil {
		return nil
	}
	var removed [][2]string
	for roomName, rs := range rooms {
		for _, handle := range rs.members[nodeID] {
			removed = append(removed, [2]string{roomName, handle})
		}
		delete(rs.members, nodeID)
		if len(rs.members) == 0 {
			delete(rooms, roomName)
		}
	}
	if len(rooms) == 0 {
		delete(cr.networks, network)
	}
	return removed
}

// broadcastChatEvent is a helper to publish a chat SSE event via a Broadcaster.
func broadcastChatEvent(b *Broadcaster, network, eventType string, payload any) {
	data, _ := json.Marshal(payload)
	b.Publish(network, protocol.Event{Type: eventType, Data: data})
}

func (cr *chatRooms) HandleForNode(network, room, nodeID string) string {
	cr.mu.RLock()
	defer cr.mu.RUnlock()
	rooms := cr.networks[network]
	if rooms == nil {
		return ""
	}
	rs := rooms[room]
	if rs == nil {
		return ""
	}
	handles := rs.members[nodeID]
	if len(handles) == 0 {
		return ""
	}
	return handles[0]
}

func (cr *chatRooms) AnyHandleForNode(network, nodeID string) string {
	cr.mu.RLock()
	defer cr.mu.RUnlock()
	rooms := cr.networks[network]
	for _, rs := range rooms {
		if handles := rs.members[nodeID]; len(handles) > 0 {
			return handles[0]
		}
	}
	return ""
}
