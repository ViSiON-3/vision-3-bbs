package hub

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/nal"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

const accessRequestSchema = `
CREATE TABLE IF NOT EXISTS area_access_requests (
	id           TEXT PRIMARY KEY,
	network      TEXT NOT NULL,
	area_tag     TEXT NOT NULL,
	node_id      TEXT NOT NULL,
	bbs_name     TEXT,
	status       TEXT DEFAULT 'pending',
	requested_at DATETIME DEFAULT (datetime('now')),
	resolved_at  DATETIME,
	UNIQUE(network, area_tag, node_id)
);
`

// AccessRequestStore manages pending area subscription requests.
type AccessRequestStore struct {
	db *sql.DB
}

// NewAccessRequestStore initializes the access requests table.
func NewAccessRequestStore(db *sql.DB) (*AccessRequestStore, error) {
	if _, err := db.Exec(accessRequestSchema); err != nil {
		return nil, fmt.Errorf("hub: create access_requests table: %w", err)
	}
	return &AccessRequestStore{db: db}, nil
}

// Add inserts a new access request. Returns the ID.
func (ar *AccessRequestStore) Add(network, areaTag, nodeID, bbsName string) (string, error) {
	id := newUUID()
	_, err := ar.db.Exec(
		`INSERT OR IGNORE INTO area_access_requests (id, network, area_tag, node_id, bbs_name)
		 VALUES (?, ?, ?, ?, ?)`,
		id, network, areaTag, nodeID, bbsName,
	)
	if err != nil {
		return "", fmt.Errorf("hub: add access request: %w", err)
	}
	return id, nil
}

// ListPending returns pending requests for a network and area tag.
func (ar *AccessRequestStore) ListPending(network, areaTag string) ([]protocol.AccessRequest, error) {
	rows, err := ar.db.Query(
		`SELECT node_id, bbs_name, requested_at FROM area_access_requests
		 WHERE network = ? AND area_tag = ? AND status = 'pending'
		 ORDER BY requested_at ASC`, network, areaTag,
	)
	if err != nil {
		return nil, fmt.Errorf("hub: list access requests: %w", err)
	}
	defer rows.Close()

	var result []protocol.AccessRequest
	for rows.Next() {
		var req protocol.AccessRequest
		var bbsName sql.NullString
		if err := rows.Scan(&req.NodeID, &bbsName, &req.RequestedAt); err != nil {
			return nil, fmt.Errorf("hub: scan access request: %w", err)
		}
		req.BBSName = bbsName.String
		result = append(result, req)
	}
	return result, rows.Err()
}

// Resolve sets the status of matching access requests.
func (ar *AccessRequestStore) Resolve(network, areaTag, nodeID, status string) error {
	_, err := ar.db.Exec(
		`UPDATE area_access_requests SET status = ?, resolved_at = datetime('now')
		 WHERE network = ? AND area_tag = ? AND node_id = ?`,
		status, network, areaTag, nodeID,
	)
	if err != nil {
		return fmt.Errorf("hub: resolve access request: %w", err)
	}
	return nil
}

// handleGetAccess returns current access config for an area (manager-only).
func (h *Hub) handleGetAccess(w http.ResponseWriter, r *http.Request) {
	network := extractNetwork(r.URL.Path)
	tag := extractAreaTag(r.URL.Path)
	nodeID := r.Header.Get(headerNodeID)

	if !h.isAreaManager(network, tag, nodeID) {
		http.Error(w, `{"error":"area manager only"}`, http.StatusForbidden)
		return
	}

	currentNAL, err := h.nalStore.Get(network)
	if err != nil || currentNAL == nil {
		http.Error(w, `{"error":"NAL not found"}`, http.StatusNotFound)
		return
	}

	area := currentNAL.FindArea(tag)
	if area == nil {
		http.Error(w, `{"error":"area not found"}`, http.StatusNotFound)
		return
	}

	cfg := protocol.AreaAccessConfig{
		Mode:      area.Access.Mode,
		AllowList: area.Access.AllowList,
		DenyList:  area.Access.DenyList,
	}
	if cfg.AllowList == nil {
		cfg.AllowList = []string{}
	}
	if cfg.DenyList == nil {
		cfg.DenyList = []string{}
	}
	writeJSON(w, http.StatusOK, cfg)
}

// handleSetAccessMode changes the access mode for an area (manager-only).
func (h *Hub) handleSetAccessMode(w http.ResponseWriter, r *http.Request) {
	network := extractNetwork(r.URL.Path)
	tag := extractAreaTag(r.URL.Path)
	nodeID := r.Header.Get(headerNodeID)

	if !h.isAreaManager(network, tag, nodeID) {
		http.Error(w, `{"error":"area manager only"}`, http.StatusForbidden)
		return
	}

	var req protocol.AccessModeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
		return
	}
	if err := protocol.ValidateAccessMode(req.Mode); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}

	if err := h.updateNALArea(network, tag, func(area *protocol.Area) {
		area.Access.Mode = req.Mode
	}); err != nil {
		slog.Error("set access mode", "error", err)
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleListAccessRequests returns pending access requests for an area (manager-only).
func (h *Hub) handleListAccessRequests(w http.ResponseWriter, r *http.Request) {
	network := extractNetwork(r.URL.Path)
	tag := extractAreaTag(r.URL.Path)
	nodeID := r.Header.Get(headerNodeID)

	if !h.isAreaManager(network, tag, nodeID) {
		http.Error(w, `{"error":"area manager only"}`, http.StatusForbidden)
		return
	}

	reqs, err := h.accessRequests.ListPending(network, tag)
	if err != nil {
		slog.Error("list access requests", "error", err)
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}
	if reqs == nil {
		reqs = []protocol.AccessRequest{}
	}
	writeJSON(w, http.StatusOK, reqs)
}

// handleApproveAccess approves access requests for an area (manager-only).
func (h *Hub) handleApproveAccess(w http.ResponseWriter, r *http.Request) {
	network := extractNetwork(r.URL.Path)
	tag := extractAreaTag(r.URL.Path)
	nodeID := r.Header.Get(headerNodeID)

	if !h.isAreaManager(network, tag, nodeID) {
		http.Error(w, `{"error":"area manager only"}`, http.StatusForbidden)
		return
	}

	var req protocol.NodeIDsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
		return
	}

	// Add nodes to allow list in NAL.
	if err := h.updateNALArea(network, tag, func(area *protocol.Area) {
		for _, nid := range req.NodeIDs {
			if !containsStr(area.Access.AllowList, nid) {
				area.Access.AllowList = append(area.Access.AllowList, nid)
			}
		}
	}); err != nil {
		slog.Error("approve access", "error", err)
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}

	// Update access request status and area subscriptions.
	for _, nid := range req.NodeIDs {
		if err := h.accessRequests.Resolve(network, tag, nid, "approved"); err != nil {
			slog.Error("approve access request", "node", nid, "network", network, "tag", tag, "error", err)
		}
		if err := h.areaSubscriptions.SetStatus(nid, network, tag, "active"); err != nil {
			slog.Error("set subscription active", "node", nid, "network", network, "tag", tag, "error", err)
		}
	}

	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleDenyAccess denies access requests and adds nodes to deny list (manager-only).
func (h *Hub) handleDenyAccess(w http.ResponseWriter, r *http.Request) {
	network := extractNetwork(r.URL.Path)
	tag := extractAreaTag(r.URL.Path)
	nodeID := r.Header.Get(headerNodeID)

	if !h.isAreaManager(network, tag, nodeID) {
		http.Error(w, `{"error":"area manager only"}`, http.StatusForbidden)
		return
	}

	var req protocol.NodeIDsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
		return
	}

	// Remove from allow list and add to deny list.
	if err := h.updateNALArea(network, tag, func(area *protocol.Area) {
		for _, nid := range req.NodeIDs {
			area.Access.AllowList = removeStr(area.Access.AllowList, nid)
			if !containsStr(area.Access.DenyList, nid) {
				area.Access.DenyList = append(area.Access.DenyList, nid)
			}
		}
	}); err != nil {
		slog.Error("deny access", "error", err)
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}

	for _, nid := range req.NodeIDs {
		if err := h.accessRequests.Resolve(network, tag, nid, "denied"); err != nil {
			slog.Error("deny access request", "node", nid, "network", network, "tag", tag, "error", err)
		}
		if err := h.areaSubscriptions.SetStatus(nid, network, tag, "denied"); err != nil {
			slog.Error("set subscription denied", "node", nid, "network", network, "tag", tag, "error", err)
		}

		// Notify denied node.
		ev, _ := protocol.NewEvent(protocol.EventSubscriptionDenied, protocol.SubscriptionDeniedPayload{
			Network: network,
			Tag:     tag,
		})
		h.broadcaster.Publish(network, ev)
	}

	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleRemoveFromDenyList removes nodes from the deny list (manager-only).
func (h *Hub) handleRemoveFromDenyList(w http.ResponseWriter, r *http.Request) {
	network := extractNetwork(r.URL.Path)
	tag := extractAreaTag(r.URL.Path)
	nodeID := r.Header.Get(headerNodeID)

	if !h.isAreaManager(network, tag, nodeID) {
		http.Error(w, `{"error":"area manager only"}`, http.StatusForbidden)
		return
	}

	var req protocol.NodeIDsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
		return
	}

	if err := h.updateNALArea(network, tag, func(area *protocol.Area) {
		for _, nid := range req.NodeIDs {
			area.Access.DenyList = removeStr(area.Access.DenyList, nid)
		}
	}); err != nil {
		slog.Error("remove from deny list", "error", err)
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// updateNALArea loads the NAL, applies a mutation to the named area, re-signs,
// persists, and fans out an nal_updated event.
func (h *Hub) updateNALArea(network, tag string, mutate func(area *protocol.Area)) error {
	currentNAL, err := h.nalStore.Get(network)
	if err != nil {
		return fmt.Errorf("get nal: %w", err)
	}
	if currentNAL == nil {
		return fmt.Errorf("no NAL for network %s", network)
	}

	area := currentNAL.FindArea(tag)
	if area == nil {
		return fmt.Errorf("area %s not found", tag)
	}

	mutate(area)

	if err := nal.Sign(currentNAL, h.cfg.Keystore); err != nil {
		return fmt.Errorf("sign nal: %w", err)
	}
	if err := h.nalStore.Put(network, currentNAL); err != nil {
		return fmt.Errorf("put nal: %w", err)
	}

	ev, _ := protocol.NewEvent(protocol.EventNALUpdated, protocol.NALUpdatedPayload{
		Network:   network,
		Updated:   currentNAL.Updated,
		AreaCount: len(currentNAL.Areas),
	})
	h.broadcaster.Publish(network, ev)

	return nil
}

// extractAreaTag pulls the area tag from paths like:
// /v3net/v1/{network}/areas/{tag}/access
func extractAreaTag(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	// v3net/v1/{network}/areas/{tag}/...
	for i, p := range parts {
		if p == "areas" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}

func containsStr(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

func removeStr(ss []string, s string) []string {
	result := make([]string, 0, len(ss))
	for _, v := range ss {
		if v != s {
			result = append(result, v)
		}
	}
	return result
}
