package protocol

import (
	"strings"
	"unicode"
)

// --- Requests (leaf → hub, JSON body) ---

// ChatJoinRequest asks the hub to add this leaf/handle to a room.
type ChatJoinRequest struct {
	Room   string `json:"room"`
	Handle string `json:"handle"`
}

// ChatLeaveRequest removes this leaf/handle from a room.
type ChatLeaveRequest struct {
	Room   string `json:"room"`
	Handle string `json:"handle"`
}

// ChatPostRequest posts a message to a room. Handle names the sending user,
// who must have joined the room. Older leaves omit it, and the hub then
// credits the node's first handle in the room. Origin is an opaque ID for
// the posting session, which the hub copies into the broadcast so the leaf
// can tell that session's own echo apart from other sessions of the same
// handle on the node.
type ChatPostRequest struct {
	Room   string `json:"room"`
	Text   string `json:"text"`
	Handle string `json:"handle,omitempty"`
	Origin string `json:"origin,omitempty"`
}

// ChatPrivateRequest sends a direct message to a specific user+node. Handle
// names the sending user; older leaves omit it, and the hub then picks one
// of the node's joined handles, or its BBS name. An empty ToNode asks the hub
// to resolve ToHandle from joined chat memberships on the same network;
// zero matches return 404 and multiple nodes return 409 without delivery.
type ChatPrivateRequest struct {
	ToHandle string `json:"to_handle"`
	ToNode   string `json:"to_node"`
	Text     string `json:"text"`
	Handle   string `json:"handle,omitempty"`
}

// ChatTopicRequest sets the topic for a room. Handle names the user setting
// it, as for ChatPostRequest.
type ChatTopicRequest struct {
	Room   string `json:"room"`
	Topic  string `json:"topic"`
	Handle string `json:"handle,omitempty"`
}

// --- Response bodies ---

// ProtoChatRoomInfo is the wire representation of a room.
type ProtoChatRoomInfo struct {
	Name      string `json:"name"`
	Topic     string `json:"topic"`
	UserCount int    `json:"user_count"`
}

// ChatMsgPayload is a single chat message (history endpoint + SSE events).
type ChatMsgPayload struct {
	Room       string `json:"room"`
	FromHandle string `json:"from_handle"`
	FromNode   string `json:"from_node"`
	FromBBS    string `json:"from_bbs"`
	ToHandle   string `json:"to_handle,omitempty"` // private messages only
	ToNode     string `json:"to_node,omitempty"`   // private messages only
	Text       string `json:"text"`
	Timestamp  string `json:"timestamp"` // RFC3339
	// Origin is the posting session's ChatPostRequest.Origin. Set on room
	// message broadcasts only; empty from older leaves and hubs.
	Origin string `json:"origin,omitempty"`
}

// ChatMember is one membership of a room: a handle on a node, with the
// node's BBS name as the hub knows it. Handles are not unique across a
// network, so a member is identified by handle and node together.
type ChatMember struct {
	Handle string `json:"handle"`
	Node   string `json:"node"`
	BBS    string `json:"bbs"`
}

// ChatJoinResponse is returned by POST /chat/rooms/join. Members and Users
// list the same memberships of the joined room; Users, the bare handles, is
// kept for leaves that predate Members.
type ChatJoinResponse struct {
	Rooms   []ProtoChatRoomInfo `json:"rooms"`
	History []ChatMsgPayload    `json:"history"` // up to 50 recent messages
	Users   []string            `json:"users"`   // handles currently in joined room
	Members []ChatMember        `json:"members,omitempty"`
}

// ChatRoomListResponse is returned by GET /chat/rooms.
type ChatRoomListResponse = []ProtoChatRoomInfo

// --- SSE event payloads (hub → leaf, carried in Event.Data) ---

// ChatJoinPayload is the data for a "chat_join" SSE event.
type ChatJoinPayload struct {
	Room   string `json:"room"`
	Handle string `json:"handle"`
	BBS    string `json:"bbs"`
	// Node is the joining node's ID, so a leaf can recognise the echo of its
	// own join, which its join response already counted. Empty from hubs
	// that predate the field.
	Node string `json:"node,omitempty"`
}

// ChatLeavePayload is the data for a "chat_leave" SSE event.
type ChatLeavePayload struct {
	Room   string `json:"room"`
	Handle string `json:"handle"`
	BBS    string `json:"bbs"`
	// Node is the leaving node's ID, so a leaf can take the right member
	// out of its list when boards share a handle. Empty from hubs that
	// predate the field.
	Node string `json:"node,omitempty"`
}

// ChatTopicPayload is the data for a "chat_topic" SSE event.
type ChatTopicPayload struct {
	Room  string `json:"room"`
	Topic string `json:"topic"`
	SetBy string `json:"set_by"`
}

// SSE event type constants for chat.
const (
	EventChatMessage = "chat_message"
	EventChatPrivate = "chat_private"
	EventChatJoin    = "chat_join"
	EventChatLeave   = "chat_leave"
	EventChatTopic   = "chat_topic"
)

// ParseChatTarget reads a caller named as handle@node, the form the chat
// screen's user list falls back to and a sysop's block list uses: the
// text up to the last "@" is the handle, the rest the node ID. Both must
// be present, and neither may hold control characters.
func ParseChatTarget(s string) (handle, node string, ok bool) {
	s = strings.TrimSpace(s)
	at := strings.LastIndex(s, "@")
	if at <= 0 || at == len(s)-1 || strings.ContainsFunc(s, unicode.IsControl) {
		return "", "", false
	}
	handle, node = strings.TrimSpace(s[:at]), strings.TrimSpace(s[at+1:])
	if handle == "" || node == "" || strings.ContainsAny(node, " @") {
		return "", "", false
	}
	return handle, node, true
}
