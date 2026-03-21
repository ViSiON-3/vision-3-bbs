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

	users := h.chatRooms.Join(room, nodeID, req.Handle)

	broadcastChatEvent(h.broadcaster, network, protocol.EventChatJoin, protocol.ChatJoinPayload{
		Room: room, Handle: req.Handle, BBS: bbsName,
	})

	history, _ := h.chatStore.RoomHistory(network, room, 50)
	resp := protocol.ChatJoinResponse{
		Rooms:   h.chatRooms.RoomList(),
		History: history,
		Users:   users,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
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

	h.chatRooms.Leave(room, nodeID, req.Handle)
	broadcastChatEvent(h.broadcaster, network, protocol.EventChatLeave, protocol.ChatLeavePayload{
		Room: room, Handle: req.Handle, BBS: bbsName,
	})
	w.WriteHeader(http.StatusNoContent)
}

// handleChatPost: POST /v3net/v1/{network}/chat/rooms/post
func (h *Hub) handleChatPost(w http.ResponseWriter, r *http.Request, network string) {
	nodeID := r.Header.Get(headerNodeID)
	if !h.chatLimiter.Allow(nodeID) {
		jsonError(w, "rate limit exceeded", http.StatusTooManyRequests)
		return
	}
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

	handle := h.chatRooms.HandleForNode(room, nodeID)
	if handle == "" {
		jsonError(w, "not joined to room", http.StatusForbidden)
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
	if !h.chatLimiter.Allow(nodeID) {
		jsonError(w, "rate limit exceeded", http.StatusTooManyRequests)
		return
	}
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

	fromHandle := h.chatRooms.AnyHandleForNode(nodeID)
	if fromHandle == "" {
		fromHandle = bbsName
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
	handle := h.chatRooms.HandleForNode(room, nodeID)
	if handle == "" {
		jsonError(w, "not joined to room", http.StatusForbidden)
		return
	}

	h.chatRooms.SetTopic(room, req.Topic)
	broadcastChatEvent(h.broadcaster, network, protocol.EventChatTopic, protocol.ChatTopicPayload{
		Room: room, Topic: req.Topic, SetBy: handle,
	})
	w.WriteHeader(http.StatusNoContent)
}

// handleChatRooms: GET /v3net/v1/{network}/chat/rooms  (no auth required)
func (h *Hub) handleChatRooms(w http.ResponseWriter, r *http.Request, _ string) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(h.chatRooms.RoomList())
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
			limit = n
		}
	}
	msgs, err := h.chatStore.RoomHistory(network, room, limit)
	if err != nil {
		jsonError(w, "query error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(msgs)
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
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
