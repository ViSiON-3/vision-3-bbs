package hub

import (
	"crypto/ed25519"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/nal"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

const coordTransferSchema = `
CREATE TABLE IF NOT EXISTS coordinator_transfers (
	network       TEXT PRIMARY KEY,
	new_node_id   TEXT NOT NULL,
	new_pubkey_b64 TEXT NOT NULL,
	token         TEXT NOT NULL,
	created_at    DATETIME DEFAULT (datetime('now'))
);
`

// CoordTransferStore manages pending coordinator transfers.
type CoordTransferStore struct {
	db *sql.DB
}

// NewCoordTransferStore initializes the coordinator_transfers table.
func NewCoordTransferStore(db *sql.DB) (*CoordTransferStore, error) {
	if _, err := db.Exec(coordTransferSchema); err != nil {
		return nil, fmt.Errorf("hub: create coordinator_transfers table: %w", err)
	}
	return &CoordTransferStore{db: db}, nil
}

// handleCoordTransfer initiates a coordinator transfer (coordinator-only).
func (h *Hub) handleCoordTransfer(w http.ResponseWriter, r *http.Request) {
	network := extractNetwork(r.URL.Path)
	nodeID := r.Header.Get(headerNodeID)

	if !h.isCoordinator(network, nodeID) {
		http.Error(w, `{"error":"coordinator only"}`, http.StatusForbidden)
		return
	}

	var req protocol.CoordTransferRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
		return
	}

	if req.NewNodeID == "" || req.NewPubKeyB64 == "" {
		http.Error(w, `{"error":"new_node_id and new_pubkey_b64 are required"}`, http.StatusBadRequest)
		return
	}

	// Generate a transfer token: sign(new_node_id + new_pubkey_b64 + timestamp)
	timestamp := time.Now().UTC().Format(time.RFC3339)
	payload := req.NewNodeID + req.NewPubKeyB64 + timestamp
	sigBytes, err := h.cfg.Keystore.SignRaw([]byte(payload))
	if err != nil {
		slog.Error("sign transfer token", "error", err)
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}
	token := base64.StdEncoding.EncodeToString(sigBytes)

	// Store pending transfer.
	_, err = h.db.Exec(
		`INSERT OR REPLACE INTO coordinator_transfers (network, new_node_id, new_pubkey_b64, token)
		 VALUES (?, ?, ?, ?)`,
		network, req.NewNodeID, req.NewPubKeyB64, token,
	)
	if err != nil {
		slog.Error("store transfer", "error", err)
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}

	// Notify the new coordinator via SSE.
	ev, _ := protocol.NewEvent(protocol.EventCoordTransferPending, protocol.CoordTransferPendingPayload{
		Network:   network,
		NewNodeID: req.NewNodeID,
	})
	h.broadcaster.Publish(network, ev)

	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "token": token})
}

// handleCoordAccept accepts a coordinator transfer (new coordinator).
func (h *Hub) handleCoordAccept(w http.ResponseWriter, r *http.Request) {
	network := extractNetwork(r.URL.Path)
	nodeID := r.Header.Get(headerNodeID)

	var req protocol.CoordAcceptRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
		return
	}

	// Look up the pending transfer. Tokens expire after 24 hours.
	var newNodeID, newPubKeyB64, storedToken, createdAt string
	err := h.db.QueryRow(
		`SELECT new_node_id, new_pubkey_b64, token, created_at FROM coordinator_transfers WHERE network = ?`,
		network,
	).Scan(&newNodeID, &newPubKeyB64, &storedToken, &createdAt)
	if err == sql.ErrNoRows {
		http.Error(w, `{"error":"no pending transfer"}`, http.StatusNotFound)
		return
	}
	if err != nil {
		slog.Error("get transfer", "error", err)
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}

	// Enforce 24-hour TTL on transfer tokens.
	if t, parseErr := time.Parse("2006-01-02 15:04:05", createdAt); parseErr == nil {
		if time.Since(t) > 24*time.Hour {
			h.db.Exec("DELETE FROM coordinator_transfers WHERE network = ?", network)
			http.Error(w, `{"error":"transfer token expired"}`, http.StatusGone)
			return
		}
	}

	// Use the same error message for both mismatches to avoid leaking
	// information about pending transfers and their targets.
	if nodeID != newNodeID || req.Token != storedToken {
		http.Error(w, `{"error":"invalid transfer credentials"}`, http.StatusForbidden)
		return
	}

	// Serialize NAL read-modify-write to prevent concurrent updates.
	h.nalMu.Lock()
	defer h.nalMu.Unlock()

	// Update the NAL with the new coordinator.
	currentNAL, err := h.nalStore.Get(network)
	if err != nil || currentNAL == nil {
		http.Error(w, `{"error":"NAL not found"}`, http.StatusInternalServerError)
		return
	}

	// Decode the new pubkey to validate it.
	newPubKey, err := base64.StdEncoding.DecodeString(newPubKeyB64)
	if err != nil || len(newPubKey) != ed25519.PublicKeySize {
		http.Error(w, `{"error":"invalid new public key"}`, http.StatusBadRequest)
		return
	}

	currentNAL.CoordNodeID = newNodeID
	currentNAL.CoordPubKeyB64 = newPubKeyB64

	// Re-sign with the hub's key, preserving the new coordinator's identity.
	if err := nal.SignPreserveCoord(currentNAL, h.cfg.Keystore); err != nil {
		slog.Error("sign nal after coord transfer", "error", err)
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}

	if err := h.nalStore.Put(network, currentNAL); err != nil {
		slog.Error("put nal after coord transfer", "error", err)
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}

	// Clean up the transfer.
	h.db.Exec("DELETE FROM coordinator_transfers WHERE network = ?", network)

	// Fan out nal_updated event.
	ev, _ := protocol.NewEvent(protocol.EventNALUpdated, protocol.NALUpdatedPayload{
		Network:   network,
		Updated:   currentNAL.Updated,
		AreaCount: len(currentNAL.Areas),
	})
	h.broadcaster.Publish(network, ev)

	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
