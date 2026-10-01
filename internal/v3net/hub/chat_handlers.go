package hub

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/chat"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// Chat rate limits. Each user has one allowance for room posts and another
// for private messages, so users on the same BBS do not throttle each other.
// The node-wide cap stops one node flooding the hub by spreading messages
// over many handles; it is loose enough that a busy board never reaches it
// in normal use.
const (
	chatUserInterval = time.Second            // per user, per kind of message
	chatNodeInterval = 200 * time.Millisecond // per node, sustained...
	chatNodeBurst    = 10                     // ...after a burst of this many
)

// Message kinds with separate per-user chat allowances.
const (
	chatKindPost    = "post"
	chatKindPrivate = "private"
)

// allowChat reports whether handle on nodeID may send a chat message of the
// given kind now. The message must fit both the user's own allowance and
// the node-wide one, and is taken from both or neither: a user sending too
// fast does not use up the allowance the node's other users share, and a
// message refused by the node cap does not count against the user.
func (h *Hub) allowChat(kind, nodeID, handle string) bool {
	return allowBoth(h.chatLimiter, kind+"\x00"+nodeID+"\x00"+handle, h.chatNodeLimiter, nodeID)
}

// roomSender returns the handle a room request from nodeID is sent as, or ""
// if it is not joined to the room. A request without a handle comes from an
// older leaf and is credited to the node's first handle in the room.
func (h *Hub) roomSender(network, room, nodeID, handle string) string {
	if handle == "" {
		return h.chatRooms.HandleForNode(network, room, nodeID)
	}
	if !h.chatRooms.IsJoined(network, room, nodeID, handle) {
		return ""
	}
	return handle
}

// handleChatJoin: POST /v3net/v1/{network}/chat/rooms/join
func (h *Hub) handleChatJoin(w http.ResponseWriter, r *http.Request, network string) {
	nodeID := r.Header.Get(headerNodeID)
	bbsName := h.subscriberBBSName(nodeID, network)

	var req protocol.ChatJoinRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	room, err := chat.NormalizeRoom(req.Room)
	if err != nil {
		jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.Handle == "" {
		jsonError(w, "handle required", http.StatusBadRequest)
		return
	}

	users := h.chatRooms.Join(network, room, nodeID, req.Handle)

	broadcastChatEvent(h.broadcaster, network, protocol.EventChatJoin, protocol.ChatJoinPayload{
		Room: room, Handle: req.Handle, BBS: bbsName, Node: nodeID,
	})

	history, _ := h.chatStore.RoomHistory(network, room, 50)
	resp := protocol.ChatJoinResponse{
		Rooms:   h.chatRooms.RoomList(network),
		History: history,
		Users:   users,
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp) // best-effort response write
}

// handleChatLeave: POST /v3net/v1/{network}/chat/rooms/leave
func (h *Hub) handleChatLeave(w http.ResponseWriter, r *http.Request, network string) {
	nodeID := r.Header.Get(headerNodeID)
	bbsName := h.subscriberBBSName(nodeID, network)

	var req protocol.ChatLeaveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	room, err := chat.NormalizeRoom(req.Room)
	if err != nil {
		jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}

	h.chatRooms.Leave(network, room, nodeID, req.Handle)
	broadcastChatEvent(h.broadcaster, network, protocol.EventChatLeave, protocol.ChatLeavePayload{
		Room: room, Handle: req.Handle, BBS: bbsName,
	})
	w.WriteHeader(http.StatusNoContent)
}

// handleChatPost: POST /v3net/v1/{network}/chat/rooms/post
func (h *Hub) handleChatPost(w http.ResponseWriter, r *http.Request, network string) {
	nodeID := r.Header.Get(headerNodeID)
	bbsName := h.subscriberBBSName(nodeID, network)

	var req protocol.ChatPostRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	room, err := chat.NormalizeRoom(req.Room)
	if err != nil {
		jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}

	handle := h.roomSender(network, room, nodeID, req.Handle)
	if handle == "" {
		jsonError(w, "not joined to room", http.StatusForbidden)
		return
	}

	// Take the rate-limit token only once the request is known to be valid,
	// so a rejected request does not throttle the user's next real message.
	if !h.allowChat(chatKindPost, nodeID, handle) {
		jsonError(w, "rate limit exceeded", http.StatusTooManyRequests)
		return
	}

	if err := h.chatStore.SaveMessage(network, room, handle, nodeID, bbsName, req.Text); err != nil {
		jsonError(w, "storage error", http.StatusInternalServerError)
		return
	}
	broadcastChatEvent(h.broadcaster, network, protocol.EventChatMessage, protocol.ChatMsgPayload{
		Room: room, FromHandle: handle, FromNode: nodeID, FromBBS: bbsName,
		Text: req.Text, Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
	w.WriteHeader(http.StatusNoContent)
}

// handleChatPrivate: POST /v3net/v1/{network}/chat/rooms/private
func (h *Hub) handleChatPrivate(w http.ResponseWriter, r *http.Request, network string) {
	nodeID := r.Header.Get(headerNodeID)
	bbsName := h.subscriberBBSName(nodeID, network)

	var req protocol.ChatPrivateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if req.ToNode == "" || req.ToHandle == "" {
		jsonError(w, "to_node and to_handle required", http.StatusBadRequest)
		return
	}

	if h.subscribers.Get(req.ToNode, network) == nil {
		jsonError(w, "target node not found", http.StatusNotFound)
		return
	}

	// The node vouches for its own users, so the handle need not be joined
	// to a room: users can send private messages from outside any room.
	fromHandle := req.Handle
	if fromHandle == "" {
		fromHandle = h.chatRooms.AnyHandleForNode(network, nodeID)
	}
	if fromHandle == "" {
		fromHandle = bbsName
	}

	// As for room posts, only a valid request takes the rate-limit token.
	if !h.allowChat(chatKindPrivate, nodeID, fromHandle) {
		jsonError(w, "rate limit exceeded", http.StatusTooManyRequests)
		return
	}

	if err := h.chatStore.SavePrivate(network, fromHandle, nodeID, req.ToHandle, req.ToNode, req.Text); err != nil {
		jsonError(w, "storage error", http.StatusInternalServerError)
		return
	}
	broadcastChatEvent(h.broadcaster, network, protocol.EventChatPrivate, protocol.ChatMsgPayload{
		FromHandle: fromHandle, FromNode: nodeID, FromBBS: bbsName,
		ToHandle: req.ToHandle, ToNode: req.ToNode,
		Text: req.Text, Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
	w.WriteHeader(http.StatusNoContent)
}

// handleChatTopic: POST /v3net/v1/{network}/chat/rooms/topic
func (h *Hub) handleChatTopic(w http.ResponseWriter, r *http.Request, network string) {
	nodeID := r.Header.Get(headerNodeID)

	var req protocol.ChatTopicRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	room, err := chat.NormalizeRoom(req.Room)
	if err != nil {
		jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}
	handle := h.roomSender(network, room, nodeID, req.Handle)
	if handle == "" {
		jsonError(w, "not joined to room", http.StatusForbidden)
		return
	}

	h.chatRooms.SetTopic(network, room, req.Topic)
	broadcastChatEvent(h.broadcaster, network, protocol.EventChatTopic, protocol.ChatTopicPayload{
		Room: room, Topic: req.Topic, SetBy: handle,
	})
	w.WriteHeader(http.StatusNoContent)
}

// handleChatRooms: GET /v3net/v1/{network}/chat/rooms  (no auth required)
func (h *Hub) handleChatRooms(w http.ResponseWriter, r *http.Request, network string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(h.chatRooms.RoomList(network)) // best-effort response write
}

// handleChatHistory: GET /v3net/v1/{network}/chat/rooms/{room}/history  (no auth)
func (h *Hub) handleChatHistory(w http.ResponseWriter, r *http.Request, network string) {
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 2 {
		jsonError(w, "bad path", http.StatusBadRequest)
		return
	}
	room := parts[len(parts)-2]
	room, err := chat.NormalizeRoom(room)
	if err != nil {
		jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			if n > 200 {
				n = 200
			}
			limit = n
		}
	}
	msgs, err := h.chatStore.RoomHistory(network, room, limit)
	if err != nil {
		jsonError(w, "query error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(msgs) // best-effort response write
}

// subscriberBBSName looks up the BBS name for a node, falling back to nodeID.
func (h *Hub) subscriberBBSName(nodeID, network string) string {
	if sub := h.subscribers.Get(nodeID, network); sub != nil && sub.BBSName != "" {
		return sub.BBSName
	}
	return nodeID
}

// jsonError writes a JSON error response.
func jsonError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg}) // best-effort response write
}
