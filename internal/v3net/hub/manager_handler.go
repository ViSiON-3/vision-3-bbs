package hub

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// errInvalidManager is returned when a proposed area manager is not an
// active subscriber of the network, so the hub has no key to record for it.
var errInvalidManager = errors.New("manager must be an active subscriber of this network")

// activeSubscriberKey returns the registered public key of an active
// subscriber, or errInvalidManager.
func (h *Hub) activeSubscriberKey(network, nodeID string) (string, error) {
	sub := h.subscribers.Get(nodeID, network)
	if sub == nil || sub.Status != "active" {
		return "", fmt.Errorf("%w: %s", errInvalidManager, nodeID)
	}
	return sub.PubKeyB64, nil
}

// handleSetAreaManager reassigns an area to a different manager node
// (coordinator only). The new manager must be an active subscriber; its
// public key comes from the hub's registry rather than the request.
func (h *Hub) handleSetAreaManager(w http.ResponseWriter, r *http.Request) {
	network := extractNetwork(r.URL.Path)
	tag := extractAreaTag(r.URL.Path)
	nodeID := r.Header.Get(headerNodeID)

	if !h.isCoordinator(network, nodeID) {
		http.Error(w, `{"error":"coordinator only"}`, http.StatusForbidden)
		return
	}

	var req protocol.AreaManagerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
		return
	}
	if req.ManagerNodeID == "" {
		http.Error(w, `{"error":"manager_node_id is required"}`, http.StatusBadRequest)
		return
	}

	pubKey, err := h.activeSubscriberKey(network, req.ManagerNodeID)
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}

	found := false
	if err := h.updateNALArea(network, tag, func(area *protocol.Area) {
		found = true
		area.ManagerNodeID = req.ManagerNodeID
		area.ManagerPubKeyB64 = pubKey
	}); err != nil {
		if !found {
			http.Error(w, `{"error":"area not found"}`, http.StatusNotFound)
			return
		}
		slog.Error("set area manager", "network", network, "tag", tag, "error", err)
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}

	slog.Info("area manager reassigned", "network", network, "tag", tag, "manager", req.ManagerNodeID)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
