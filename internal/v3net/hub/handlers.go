package hub

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strconv"
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
	_, _ = w.Write([]byte("[")) // best-effort response write
	for i, data := range results {
		if i > 0 {
			_, _ = w.Write([]byte(",")) // best-effort response write
		}
		_, _ = w.Write([]byte(data)) // best-effort response write
	}
	_, _ = w.Write([]byte("]")) // best-effort response write
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
	if nalErr != nil {
		slog.Error("get NAL for post", "network", network, "error", nalErr)
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}
	if currentNAL == nil {
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
	nodeID := r.Header.Get(headerNodeID)
	h.broadcaster.ServeSSE(w, r, network)
	// ServeSSE blocks until the client disconnects.
	// Clean up chat room presence for the disconnected node.
	removed := h.chatRooms.HandleDisconnect(network, nodeID)
	sub := h.subscribers.Get(nodeID, network)
	bbsName := nodeID
	if sub != nil && sub.BBSName != "" {
		bbsName = sub.BBSName
	}
	for _, pair := range removed {
		broadcastChatEvent(h.broadcaster, network, protocol.EventChatLeave,
			protocol.ChatLeavePayload{Room: pair[0], Handle: pair[1], BBS: bbsName})
	}
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

	// Name the node by its host, as other boards know it. A subscriber that
	// never gave a host falls back to its BBS name, then to its node ID, so
	// a presence event always says where the user is.
	nodeName := nodeID
	if sub := h.subscribers.Get(nodeID, network); sub != nil {
		switch {
		case sub.BBSHost != "":
			nodeName = sub.BBSHost
		case sub.BBSName != "":
			nodeName = sub.BBSName
		}
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

// handleSubscribe registers a leaf node. It is the bootstrap step, so it
// does not use authMiddleware: the hub may not know the node yet. A request
// may instead be signed with the key it submits (see verifySubscribeSignature),
// proving the caller holds that key. Node keys are public, so an unsigned
// request can only register a new node; changing an existing registration
// (its BBS name and host, or its area subscriptions) requires a signature.
func (h *Hub) handleSubscribe(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 8*1024) // 8KB limit for subscribe

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, `{"error":"request body too large or unreadable"}`, http.StatusBadRequest)
		return
	}
	var req protocol.SubscribeRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
		return
	}

	if h.findNetwork(req.Network) == nil {
		http.Error(w, `{"error":"unknown network"}`, http.StatusNotFound)
		return
	}

	// Validate that node_id is the correct derivation of the submitted public key.
	pubKeyBytes, err := base64.StdEncoding.DecodeString(req.PubKeyB64)
	if err != nil || len(pubKeyBytes) != ed25519.PublicKeySize {
		http.Error(w, `{"error":"invalid pubkey_b64"}`, http.StatusUnprocessableEntity)
		return
	}
	h256 := sha256.Sum256(pubKeyBytes)
	expectedNodeID := hex.EncodeToString(h256[:8])
	if req.NodeID != expectedNodeID {
		http.Error(w, `{"error":"node_id does not match pubkey_b64"}`, http.StatusUnprocessableEntity)
		return
	}

	signed, msg := verifySubscribeSignature(r, body, req.NodeID, pubKeyBytes)
	if msg != "" {
		http.Error(w, msg, http.StatusUnauthorized)
		return
	}

	// Node IDs are a 64-bit truncation of the key hash, so check the
	// stored key too rather than trusting the ID alone.
	existing := h.subscribers.Get(req.NodeID, req.Network)
	if existing != nil {
		storedKey, err := base64.StdEncoding.DecodeString(existing.PubKeyB64)
		if err != nil || !bytes.Equal(storedKey, pubKeyBytes) {
			http.Error(w, `{"error":"node_id is registered with a different key"}`, http.StatusConflict)
			return
		}
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

	// A signed re-subscribe updates the node's name and host. Banned nodes
	// keep the details they were banned under.
	if existing != nil && signed && actualStatus != "banned" &&
		(existing.BBSName != req.BBSName || existing.BBSHost != req.BBSHost) {
		if err := h.subscribers.SetProfile(req.NodeID, req.Network, req.BBSName, req.BBSHost); err != nil {
			slog.Error("v3net hub: update subscriber profile", "node", req.NodeID, "error", err)
			http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
			return
		}
	}

	// Anyone can build an unsigned request for a known node, so one must not
	// change an existing node's area subscriptions or file access requests
	// in its name.
	if len(req.AreaTags) > 0 && existing != nil && !signed {
		slog.Warn("v3net hub: ignoring area_tags on unsigned re-subscribe", "node", req.NodeID, "network", req.Network)
		writeJSON(w, http.StatusOK, protocol.SubscribeResponse{OK: true, Status: actualStatus})
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
		if nalErr != nil {
			slog.Error("get NAL for subscribe", "network", req.Network, "error", nalErr)
			http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
			return
		}
		if currentNAL == nil {
			// No NAL published yet — return basic response without area status.
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

			// Re-subscribing never downgrades an active area subscription,
			// e.g. one a manager approved in an approval-mode area.
			active, err := h.areaSubscriptions.IsActive(req.NodeID, req.Network, tag)
			if err != nil {
				slog.Error("v3net hub: check area subscription", "node", req.NodeID, "tag", tag, "error", err)
				http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
				return
			}

			var areaStatus string
			switch {
			case active:
				areaStatus = "active"
			case area.Access.Mode == protocol.AccessModeOpen:
				areaStatus = "active"
			case area.Access.Mode == protocol.AccessModeApproval:
				if containsStr(area.Access.AllowList, req.NodeID) {
					areaStatus = "active"
				} else {
					areaStatus = "pending"
					pendingRequests = append(pendingRequests, pendingAccessRequest{tag: tag})
				}
			case area.Access.Mode == protocol.AccessModeClosed:
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
		// Persistence failures must not be reported as success: Upsert and
		// Add are both idempotent, so the leaf can safely retry the whole
		// subscribe request after a 500.
		for _, ps := range pending {
			if err := h.areaSubscriptions.Upsert(req.NodeID, req.Network, ps.tag, ps.status); err != nil {
				slog.Error("v3net hub: persist area subscription", "node", req.NodeID, "tag", ps.tag, "error", err)
				http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
				return
			}
			areaStatuses = append(areaStatuses, protocol.AreaSubscriptionStatus{
				Tag:    ps.tag,
				Status: ps.status,
			})
		}

		for _, pr := range pendingRequests {
			if _, err := h.accessRequests.Add(req.Network, pr.tag, req.NodeID, req.BBSName); err != nil {
				slog.Error("v3net hub: persist access request", "node", req.NodeID, "tag", pr.tag, "error", err)
				http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
				return
			}
			// Publish only after the request is durably stored, so managers
			// are never notified about a request that does not exist.
			ev, _ := protocol.NewEvent(protocol.EventAreaAccessRequested, protocol.AreaAccessRequestedPayload{
				Network: req.Network,
				Tag:     pr.tag,
				NodeID:  req.NodeID,
				BBSName: req.BBSName,
			})
			h.broadcaster.Publish(req.Network, ev)
		}

		writeJSON(w, http.StatusOK, protocol.SubscribeWithAreasResponse{
			OK:     true,
			Status: actualStatus,
			Areas:  areaStatuses,
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
	// Usually a client disconnect, but Encode can also fail on a marshal
	// bug — log so server-side errors don't vanish silently.
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Debug("hub: write JSON response", "error", err)
	}
}

// verifySubscribeSignature checks an optional signature on a subscribe
// request, made with the same scheme as authenticated endpoints but verified
// against the submitted key, since the hub may not have one stored yet.
// It reports whether the request was signed; a non-empty message means a
// signature was present but invalid.
func verifySubscribeSignature(r *http.Request, body []byte, nodeID string, pubKey ed25519.PublicKey) (bool, string) {
	headerNode := r.Header.Get(headerNodeID)
	sig := r.Header.Get(headerSignature)
	if headerNode == "" && sig == "" {
		return false, ""
	}
	if headerNode == "" || sig == "" || r.Header.Get("Date") == "" {
		return false, `{"error":"missing auth headers"}`
	}
	if headerNode != nodeID {
		return false, `{"error":"node ID header does not match node_id"}`
	}
	if msg := checkRequestDate(r.Header.Get("Date")); msg != "" {
		return false, msg
	}
	if !signatureValid(r, body, pubKey) {
		return false, `{"error":"invalid signature"}`
	}
	return true, ""
}
