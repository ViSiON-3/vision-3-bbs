package hub

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/nal"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

const nalSchema = `
CREATE TABLE IF NOT EXISTS network_nal (
	network      TEXT PRIMARY KEY,
	nal_json     TEXT NOT NULL,
	verified_at  DATETIME DEFAULT (datetime('now'))
);
`

// NALStore handles SQLite-persisted NAL storage for the hub.
type NALStore struct {
	db *sql.DB
}

// NewNALStore initializes the NAL table.
func NewNALStore(db *sql.DB) (*NALStore, error) {
	if _, err := db.Exec(nalSchema); err != nil {
		return nil, fmt.Errorf("hub: create nal table: %w", err)
	}
	return &NALStore{db: db}, nil
}

// Get returns the stored NAL for a network, or nil if not found.
func (ns *NALStore) Get(network string) (*protocol.NAL, error) {
	var data string
	err := ns.db.QueryRow("SELECT nal_json FROM network_nal WHERE network = ?", network).Scan(&data)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("hub: get nal: %w", err)
	}
	var n protocol.NAL
	if err := json.Unmarshal([]byte(data), &n); err != nil {
		return nil, fmt.Errorf("hub: unmarshal nal: %w", err)
	}
	return &n, nil
}

// Put stores (upserts) a NAL for a network.
func (ns *NALStore) Put(network string, n *protocol.NAL) error {
	data, err := json.Marshal(n)
	if err != nil {
		return fmt.Errorf("hub: marshal nal: %w", err)
	}
	_, err = ns.db.Exec(
		`INSERT INTO network_nal (network, nal_json) VALUES (?, ?)
		 ON CONFLICT(network) DO UPDATE SET nal_json = excluded.nal_json, verified_at = datetime('now')`,
		network, string(data),
	)
	if err != nil {
		return fmt.Errorf("hub: put nal: %w", err)
	}
	return nil
}

// handleGetNAL serves the current signed NAL for a network (public, no auth).
func (h *Hub) handleGetNAL(w http.ResponseWriter, r *http.Request) {
	network := extractNetwork(r.URL.Path)
	if h.findNetwork(network) == nil {
		http.Error(w, `{"error":"network not found"}`, http.StatusNotFound)
		return
	}

	n, err := h.nalStore.Get(network)
	if err != nil {
		slog.Error("get nal", "error", err)
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}
	if n == nil {
		http.Error(w, `{"error":"no NAL available"}`, http.StatusNotFound)
		return
	}

	writeJSON(w, http.StatusOK, n)
}

// handlePostNAL accepts a new signed NAL from the coordinator (auth required).
func (h *Hub) handlePostNAL(w http.ResponseWriter, r *http.Request) {
	network := extractNetwork(r.URL.Path)
	nodeID := r.Header.Get(headerNodeID)

	if h.findNetwork(network) == nil {
		http.Error(w, `{"error":"network not found"}`, http.StatusNotFound)
		return
	}

	var n protocol.NAL
	if err := json.NewDecoder(r.Body).Decode(&n); err != nil {
		http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
		return
	}

	if n.Network != network {
		http.Error(w, `{"error":"network mismatch"}`, http.StatusBadRequest)
		return
	}

	// Check that the submitter is the coordinator.
	existing, err := h.nalStore.Get(network)
	if err != nil {
		slog.Error("get nal for coord check", "error", err)
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}
	if existing != nil && existing.CoordNodeID != nodeID {
		http.Error(w, `{"error":"only the coordinator may update the NAL"}`, http.StatusForbidden)
		return
	}
	// For initial NAL creation, only the hub itself may bootstrap the coordinator role.
	// This prevents any subscriber from claiming coordinator authority.
	if existing == nil {
		hubNodeID := h.cfg.Keystore.NodeID()
		if nodeID != hubNodeID {
			http.Error(w, `{"error":"only the hub operator may create the initial NAL"}`, http.StatusForbidden)
			return
		}
		if n.CoordNodeID != nodeID {
			http.Error(w, `{"error":"coordinator node ID must match sender for initial NAL"}`, http.StatusBadRequest)
			return
		}
	}

	if err := nal.Verify(&n); err != nil {
		http.Error(w, `{"error":"NAL signature verification failed"}`, http.StatusUnprocessableEntity)
		return
	}

	if err := h.nalStore.Put(network, &n); err != nil {
		slog.Error("put nal", "error", err)
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}

	// Fan out nal_updated event.
	ev, _ := protocol.NewEvent(protocol.EventNALUpdated, protocol.NALUpdatedPayload{
		Network:   network,
		Updated:   n.Updated,
		AreaCount: len(n.Areas),
	})
	h.broadcaster.Publish(network, ev)

	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// isCoordinator checks if the given node ID is the coordinator for the network.
func (h *Hub) isCoordinator(network, nodeID string) bool {
	n, err := h.nalStore.Get(network)
	if err != nil || n == nil {
		return false
	}
	return n.CoordNodeID == nodeID
}

// isAreaManager checks if the given node ID is the manager of the specified area.
func (h *Hub) isAreaManager(network, tag, nodeID string) bool {
	n, err := h.nalStore.Get(network)
	if err != nil || n == nil {
		return false
	}
	area := n.FindArea(tag)
	if area == nil {
		return false
	}
	return area.ManagerNodeID == nodeID
}
