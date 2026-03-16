package hub

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// handleNetworks returns all networks this hub serves (public, no auth).
func (h *Hub) handleNetworks(w http.ResponseWriter, r *http.Request) {
	var summaries []protocol.NetworkSummary
	for _, nc := range h.cfg.Networks {
		count, _ := h.messages.Count(nc.Name)
		summaries = append(summaries, protocol.NetworkSummary{
			Name:         nc.Name,
			Description:  nc.Description,
			HubNodeID:    h.cfg.Keystore.NodeID(),
			MessageCount: count,
		})
	}
	writeJSON(w, http.StatusOK, summaries)
}

// handleNetworkInfo returns full metadata for a single network (public, no auth).
func (h *Hub) handleNetworkInfo(w http.ResponseWriter, r *http.Request) {
	network := extractNetwork(r.URL.Path)
	nc := h.findNetwork(network)
	if nc == nil {
		http.Error(w, `{"error":"network not found"}`, http.StatusNotFound)
		return
	}

	count, _ := h.messages.Count(network)
	info := protocol.NetworkInfo{
		Name:         nc.Name,
		Description:  nc.Description,
		HubNodeID:    h.cfg.Keystore.NodeID(),
		HubPubKeyB64: h.cfg.Keystore.PubKeyBase64(),
		LeafCount:    h.subscribers.ActiveCount(network),
		MessageCount: count,
		Policy: protocol.NetworkPolicy{
			MaxBodyBytes:    protocol.MaxBodyBytes,
			PollIntervalMin: 60,
			RequireTearline: false,
		},
	}
	writeJSON(w, http.StatusOK, info)
}

// handleGetMessages returns messages newer than a cursor (auth required).
func (h *Hub) handleGetMessages(w http.ResponseWriter, r *http.Request) {
	network := extractNetwork(r.URL.Path)
	since := r.URL.Query().Get("since")
	limit := 100
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n >= 1 && n <= 500 {
			limit = n
		}
	}

	results, hasMore, err := h.messages.Fetch(network, since, limit)
	if err != nil {
		slog.Error("fetch messages", "error", err)
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}

	if hasMore {
		w.Header().Set("X-V3Net-Has-More", "true")
	}

	// Write raw JSON array of message objects.
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("["))
	for i, data := range results {
		if i > 0 {
			w.Write([]byte(","))
		}
		w.Write([]byte(data))
	}
	w.Write([]byte("]"))
}

// handlePostMessage accepts a new message from a leaf node (auth required).
func (h *Hub) handlePostMessage(w http.ResponseWriter, r *http.Request) {
	network := extractNetwork(r.URL.Path)

	var msg protocol.Message
	if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
		http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
		return
	}

	if err := msg.Validate(); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}

	if msg.Network != network {
		http.Error(w, `{"error":"network mismatch"}`, http.StatusBadRequest)
		return
	}

	if msg.IsTruncated() {
		msg.Truncate()
	}

	data, err := json.Marshal(msg)
	if err != nil {
		http.Error(w, `{"error":"marshal failed"}`, http.StatusInternalServerError)
		return
	}

	isNew, err := h.messages.Store(msg.MsgUUID, network, string(data))
	if err != nil {
		slog.Error("store message", "error", err)
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}

	if isNew {
		ev, _ := protocol.NewEvent(protocol.EventNewMessage, protocol.NewMessagePayload{
			Network: network,
			MsgUUID: msg.MsgUUID,
			From:    msg.From,
			Subject: msg.Subject,
		})
		h.broadcaster.Publish(network, ev)
	}

	writeJSON(w, http.StatusOK, protocol.MessageResponse{OK: true, MsgUUID: msg.MsgUUID})
}

// handleEvents serves the SSE event stream (auth required).
func (h *Hub) handleEvents(w http.ResponseWriter, r *http.Request) {
	network := extractNetwork(r.URL.Path)
	h.broadcaster.ServeSSE(w, r, network)
}

// handleChat accepts an inter-BBS chat message (auth required).
// Rate limited to 1 message per second per node.
func (h *Hub) handleChat(w http.ResponseWriter, r *http.Request) {
	nodeID := r.Header.Get(headerNodeID)
	if !h.chatLimiter.Allow(nodeID) {
		http.Error(w, `{"error":"rate limit exceeded"}`, http.StatusTooManyRequests)
		return
	}

	network := extractNetwork(r.URL.Path)

	var req protocol.ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
		return
	}

	sub := h.subscribers.Get(nodeID, network)
	nodeName := ""
	if sub != nil {
		nodeName = sub.BBSHost
	}

	ev, _ := protocol.NewEvent(protocol.EventChat, protocol.ChatPayload{
		From:      req.From,
		Node:      nodeName,
		Text:      req.Text,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
	h.broadcaster.Publish(network, ev)

	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handlePresence accepts a logon/logoff notification (auth required).
func (h *Hub) handlePresence(w http.ResponseWriter, r *http.Request) {
	network := extractNetwork(r.URL.Path)
	nodeID := r.Header.Get(headerNodeID)

	var req protocol.PresenceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
		return
	}

	if req.Type != protocol.EventLogon && req.Type != protocol.EventLogoff {
		http.Error(w, `{"error":"type must be logon or logoff"}`, http.StatusBadRequest)
		return
	}

	sub := h.subscribers.Get(nodeID, network)
	nodeName := ""
	if sub != nil {
		nodeName = sub.BBSHost
	}

	ts := time.Now().UTC().Format(time.RFC3339)
	var ev protocol.Event
	var err error
	if req.Type == protocol.EventLogon {
		ev, err = protocol.NewEvent(protocol.EventLogon, protocol.LogonPayload{
			Handle: req.Handle, Node: nodeName, Timestamp: ts,
		})
	} else {
		ev, err = protocol.NewEvent(protocol.EventLogoff, protocol.LogoffPayload{
			Handle: req.Handle, Node: nodeName, Timestamp: ts,
		})
	}
	if err != nil {
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}

	h.broadcaster.Publish(network, ev)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleSubscribe registers a new leaf node (no auth — bootstrap step).
func (h *Hub) handleSubscribe(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 8*1024) // 8KB limit for subscribe

	var req protocol.SubscribeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
		return
	}

	if h.findNetwork(req.Network) == nil {
		http.Error(w, `{"error":"unknown network"}`, http.StatusNotFound)
		return
	}

	status := "pending"
	if h.cfg.AutoApprove {
		status = "active"
	}

	sub := Subscriber{
		NodeID:    req.NodeID,
		Network:   req.Network,
		PubKeyB64: req.PubKeyB64,
		BBSName:   req.BBSName,
		BBSHost:   req.BBSHost,
		Status:    status,
	}

	actualStatus, err := h.subscribers.Add(sub)
	if err != nil {
		slog.Error("add subscriber", "error", err)
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, protocol.SubscribeResponse{OK: true, Status: actualStatus})
}

func (h *Hub) findNetwork(name string) *NetworkConfig {
	for i := range h.cfg.Networks {
		if h.cfg.Networks[i].Name == name {
			return &h.cfg.Networks[i]
		}
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// messagesPathSuffix checks if a path ends with /messages (to distinguish GET vs POST).
func messagesPathSuffix(path string) bool {
	return strings.HasSuffix(strings.TrimRight(path, "/"), "/messages")
}
