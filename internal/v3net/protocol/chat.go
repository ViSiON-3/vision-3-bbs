package protocol

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

// ChatPostRequest posts a message to a room.
type ChatPostRequest struct {
	Room string `json:"room"`
	Text string `json:"text"`
}

// ChatPrivateRequest sends a direct message to a specific user+node.
type ChatPrivateRequest struct {
	ToHandle string `json:"to_handle"`
	ToNode   string `json:"to_node"`
	Text     string `json:"text"`
}

// ChatTopicRequest sets the topic for a room.
type ChatTopicRequest struct {
	Room  string `json:"room"`
	Topic string `json:"topic"`
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
}

// ChatJoinResponse is returned by POST /chat/rooms/join.
type ChatJoinResponse struct {
	Rooms   []ProtoChatRoomInfo `json:"rooms"`
	History []ChatMsgPayload    `json:"history"` // up to 50 recent messages
	Users   []string            `json:"users"`   // handles currently in joined room
}

// ChatRoomListResponse is returned by GET /chat/rooms.
type ChatRoomListResponse = []ProtoChatRoomInfo

// --- SSE event payloads (hub → leaf, carried in Event.Data) ---

// ChatJoinPayload is the data for a "chat_join" SSE event.
type ChatJoinPayload struct {
	Room   string `json:"room"`
	Handle string `json:"handle"`
	BBS    string `json:"bbs"`
}

// ChatLeavePayload is the data for a "chat_leave" SSE event.
type ChatLeavePayload struct {
	Room   string `json:"room"`
	Handle string `json:"handle"`
	BBS    string `json:"bbs"`
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
