package hub

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// requireOperator returns false (and writes the error) unless the
// authenticated node is the hub operator (the hub's own keystore ID).
func (h *Hub) requireOperator(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get(headerNodeID) != h.cfg.Keystore.NodeID() {
		http.Error(w, `{"error":"hub operator only"}`, http.StatusForbidden)
		return false
	}
	return true
}

// handleListNodes serves GET /v3net/v1/{network}/nodes (operator only).
func (h *Hub) handleListNodes(w http.ResponseWriter, r *http.Request) {
	if !h.requireOperator(w, r) {
		return
	}
	network := extractNetwork(r.URL.Path)
	if h.findNetwork(network) == nil {
		http.Error(w, `{"error":"network not found"}`, http.StatusNotFound)
		return
	}
	subs, err := h.subscribers.List(network)
	if err != nil {
		slog.Error("list nodes", "error", err)
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}
	nodes := make([]protocol.NodeInfo, 0, len(subs))
	for _, s := range subs {
		nodes = append(nodes, protocol.NodeInfo{
			NodeID: s.NodeID, BBSName: s.BBSName, BBSHost: s.BBSHost,
			Status: s.Status, CreatedAt: s.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, nodes)
}

// handleNodeAction serves POST /v3net/v1/{network}/nodes/{id}/{action}
// for approve, ban, unban, and remove (operator only).
func (h *Hub) handleNodeAction(w http.ResponseWriter, r *http.Request, nodeID, action string) {
	if !h.requireOperator(w, r) {
		return
	}
	network := extractNetwork(r.URL.Path)
	if h.findNetwork(network) == nil {
		http.Error(w, `{"error":"network not found"}`, http.StatusNotFound)
		return
	}
	if nodeID == h.cfg.Keystore.NodeID() {
		http.Error(w, `{"error":"cannot modify the hub's own node"}`, http.StatusBadRequest)
		return
	}

	var err error
	status := ""
	switch action {
	case "approve":
		if sub := h.subscribers.Get(nodeID, network); sub != nil && sub.Status != "pending" {
			http.Error(w, `{"error":"node is not pending"}`, http.StatusConflict)
			return
		}
		status = "active"
		err = h.subscribers.SetStatus(nodeID, network, status)
	case "unban":
		if sub := h.subscribers.Get(nodeID, network); sub != nil && sub.Status != "banned" {
			http.Error(w, `{"error":"node is not banned"}`, http.StatusConflict)
			return
		}
		status = "active"
		err = h.subscribers.SetStatus(nodeID, network, status)
	case "ban":
		status = "banned"
		err = h.subscribers.SetStatus(nodeID, network, status)
	case "remove":
		err = h.subscribers.Delete(nodeID, network)
	default:
		http.NotFound(w, r)
		return
	}
	if errors.Is(err, ErrUnknownNode) {
		http.Error(w, `{"error":"node not found"}`, http.StatusNotFound)
		return
	}
	if err != nil {
		slog.Error("node action", "action", action, "node", nodeID, "error", err)
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}
	slog.Info("v3net hub: node status changed", "network", network, "node", nodeID, "action", action)
	if status == "" {
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": status})
}

// matchNodeActionPath matches /v3net/v1/{network}/nodes/{id}/{action} and
// returns the node ID and action.
func matchNodeActionPath(path string) (nodeID, action string, ok bool) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	// v3net/v1/{network}/nodes/{id}/{action}
	if len(parts) != 6 || parts[3] != "nodes" {
		return "", "", false
	}
	return parts[4], parts[5], true
}
