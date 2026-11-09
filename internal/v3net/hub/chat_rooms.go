package hub

import (
	"encoding/json"
	"sort"
	"strings"
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

// Join adds handle on nodeID to room and returns the room's members, with
// their nodes and no BBS names. added is false if the handle was already a
// member, as it is when the same user has chat open on two nodes of one BBS.
func (cr *chatRooms) Join(network, room, nodeID, handle string) (members []protocol.ChatMember, added bool) {
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
			return cr.membersLocked(network, room), false
		}
	}
	rs.members[nodeID] = append(rs.members[nodeID], handle)
	return cr.membersLocked(network, room), true
}

// Leave removes a membership and reports whether one existed. Repeated
// cleanup requests must not generate duplicate leave announcements.
func (cr *chatRooms) Leave(network, room, nodeID, handle string) bool {
	cr.mu.Lock()
	defer cr.mu.Unlock()
	rooms := cr.networks[network]
	if rooms == nil {
		return false
	}
	rs := rooms[room]
	if rs == nil {
		return false
	}
	removed := false
	handles := rs.members[nodeID]
	for i, h := range handles {
		if h == handle {
			rs.members[nodeID] = append(handles[:i], handles[i+1:]...)
			removed = true
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
	return removed
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

// Users returns the handles in room, one per membership, as Members does.
func (cr *chatRooms) Users(network, room string) []string {
	return handlesOf(cr.Members(network, room))
}

// Members returns room's memberships with their nodes and no BBS names,
// sorted by handle, then node, or nil for a room that does not exist.
func (cr *chatRooms) Members(network, room string) []protocol.ChatMember {
	cr.mu.RLock()
	defer cr.mu.RUnlock()
	return cr.membersLocked(network, room)
}

// handlesOf returns the handles of members, in their order, or nil for no
// members.
func handlesOf(members []protocol.ChatMember) []string {
	if members == nil {
		return nil
	}
	out := make([]string, len(members))
	for i, m := range members {
		out[i] = m.Handle
	}
	return out
}

// nodesForHandle returns the distinct nodes with this exact handle joined
// to any room on network, sorted for stable disambiguation choices. Several
// rooms on the same BBS represent one possible destination.
func (cr *chatRooms) nodesForHandle(network, handle string) []string {
	cr.mu.RLock()
	defer cr.mu.RUnlock()
	seen := make(map[string]bool)
	for _, room := range cr.networks[network] {
		for node, handles := range room.members {
			for _, member := range handles {
				if member == handle {
					seen[node] = true
					break
				}
			}
		}
	}
	nodes := make([]string, 0, len(seen))
	for node := range seen {
		nodes = append(nodes, node)
	}
	sort.Strings(nodes)
	return nodes
}

// membersLocked is Members with the lock held. Memberships are kept per
// node in a map, so the list is sorted for a stable order on every read:
// by handle, case-insensitively, then node.
func (cr *chatRooms) membersLocked(network, room string) []protocol.ChatMember {
	rooms := cr.networks[network]
	if rooms == nil {
		return nil
	}
	rs := rooms[room]
	if rs == nil {
		return nil
	}
	var out []protocol.ChatMember
	for node, handles := range rs.members {
		for _, h := range handles {
			out = append(out, protocol.ChatMember{Handle: h, Node: node})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if a, b := strings.ToLower(out[i].Handle), strings.ToLower(out[j].Handle); a != b {
			return a < b
		}
		if out[i].Handle != out[j].Handle {
			return out[i].Handle < out[j].Handle
		}
		return out[i].Node < out[j].Node
	})
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
