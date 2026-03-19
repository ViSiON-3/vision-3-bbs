package hub

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
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
// Results are filtered to the node's actively subscribed areas.
func (h *Hub) handleGetMessages(w http.ResponseWriter, r *http.Request) {
	network := extractNetwork(r.URL.Path)
	nodeID := r.Header.Get(headerNodeID)
	since := r.URL.Query().Get("since")
	limit := 100
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n >= 1 && n <= 500 {
			limit = n
		}
	}

	// Collect the node's actively subscribed area tags.
	allSubs, err := h.areaSubscriptions.ListForNode(nodeID, network)
	if err != nil {
		slog.Error("list area subscriptions", "error", err)
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}
	var areaTags []string
	for _, s := range allSubs {
		if s.Status == "active" {
			areaTags = append(areaTags, s.Tag)
		}
	}

	results, hasMore, err := h.messages.Fetch(network, since, limit, areaTags)
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
	nodeID := r.Header.Get(headerNodeID)

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

	// Enforce NAL-based area access control.
	currentNAL, nalErr := h.nalStore.Get(network)
	if nalErr != nil || currentNAL == nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "no NAL published for this network"})
		return
	}

	area := currentNAL.FindArea(msg.AreaTag)
	if area == nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "unknown area_tag: " + msg.AreaTag})
		return
	}

	active, err := h.areaSubscriptions.IsActive(nodeID, network, msg.AreaTag)
	if err != nil {
		slog.Error("check area subscription", "error", err)
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}
	if !active {
		http.Error(w, `{"error":"area access denied"}`, http.StatusForbidden)
		return
	}

	if msg.NeedsTruncation() {
		msg.Truncate()
	}

	data, err := json.Marshal(msg)
	if err != nil {
		http.Error(w, `{"error":"marshal failed"}`, http.StatusInternalServerError)
		return
	}

	isNew, err := h.messages.Store(msg.MsgUUID, network, msg.AreaTag, string(data))
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

	// Validate that node_id is the correct derivation of the submitted public key.
	pubKeyBytes, err := base64.StdEncoding.DecodeString(req.PubKeyB64)
	if err != nil || len(pubKeyBytes) != 32 {
		http.Error(w, `{"error":"invalid pubkey_b64"}`, http.StatusUnprocessableEntity)
		return
	}
	h256 := sha256.Sum256(pubKeyBytes)
	expectedNodeID := hex.EncodeToString(h256[:8])
	if req.NodeID != expectedNodeID {
		http.Error(w, `{"error":"node_id does not match pubkey_b64"}`, http.StatusUnprocessableEntity)
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

	// If area_tags are provided, process area subscriptions.
	// Only process area subscriptions for active network subscribers.
	if len(req.AreaTags) > 0 && actualStatus != "active" {
		writeJSON(w, http.StatusOK, protocol.SubscribeResponse{OK: true, Status: actualStatus})
		return
	}
	if len(req.AreaTags) > 0 {
		currentNAL, nalErr := h.nalStore.Get(req.Network)
		if nalErr != nil || currentNAL == nil {
			// No NAL available — return basic response without area status.
			writeJSON(w, http.StatusOK, protocol.SubscribeResponse{OK: true, Status: actualStatus})
			return
		}

		var areaStatuses []protocol.AreaSubscriptionStatus
		type pendingSubscription struct {
			tag    string
			status string
		}
		var pending []pendingSubscription
		type pendingAccessRequest struct {
			tag string
		}
		var pendingRequests []pendingAccessRequest

		// First pass: validate all tags and determine statuses.
		for _, tag := range req.AreaTags {
			area := currentNAL.FindArea(tag)
			if area == nil {
				writeJSON(w, http.StatusUnprocessableEntity, map[string]string{
					"error": "unknown area tag: " + tag,
				})
				return
			}

			// Check deny list.
			if isDenied(area, req.NodeID) {
				http.Error(w, `{"error":"access denied"}`, http.StatusForbidden)
				return
			}

			var areaStatus string
			switch area.Access.Mode {
			case protocol.AccessModeOpen:
				areaStatus = "active"
			case protocol.AccessModeApproval:
				areaStatus = "pending"
				pendingRequests = append(pendingRequests, pendingAccessRequest{tag: tag})
			case protocol.AccessModeClosed:
				// Only allowed if already on allow list.
				if containsStr(area.Access.AllowList, req.NodeID) {
					areaStatus = "active"
				} else {
					http.Error(w, `{"error":"access denied"}`, http.StatusForbidden)
					return
				}
			}

			pending = append(pending, pendingSubscription{tag: tag, status: areaStatus})
		}

		// Second pass: all validations passed, apply subscriptions and events.
		for _, ps := range pending {
			h.areaSubscriptions.Upsert(req.NodeID, req.Network, ps.tag, ps.status)
			areaStatuses = append(areaStatuses, protocol.AreaSubscriptionStatus{
				Tag:    ps.tag,
				Status: ps.status,
			})
		}

		for _, pr := range pendingRequests {
			h.accessRequests.Add(req.Network, pr.tag, req.NodeID, req.BBSName)
			ev, _ := protocol.NewEvent(protocol.EventAreaAccessRequested, protocol.AreaAccessRequestedPayload{
				Network: req.Network,
				Tag:     pr.tag,
				NodeID:  req.NodeID,
				BBSName: req.BBSName,
			})
			h.broadcaster.Publish(req.Network, ev)
		}

		writeJSON(w, http.StatusOK, protocol.SubscribeWithAreasResponse{
			OK:    true,
			Areas: areaStatuses,
		})
		return
	}

	writeJSON(w, http.StatusOK, protocol.SubscribeResponse{OK: true, Status: actualStatus})
}

func isDenied(area *protocol.Area, nodeID string) bool {
	for _, id := range area.Access.DenyList {
		if id == nodeID {
			return true
		}
	}
	return false
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
