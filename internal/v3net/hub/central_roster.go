package hub

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// The Central board roster on a hub (see protocol.CentralRoster): the
// coordinator's latest signed roster per network, stored and served byte for
// byte. Only the coordinator in force (the holder of the key the network's
// NAL and handovers lead to) may publish one, and each must have a higher
// seq than the one stored, so an older roster cannot be put back. Boards
// check the coordinator's signature themselves, against the key they pin;
// the hub's checks only keep it from storing what boards would refuse.

const centralRosterSchema = `
CREATE TABLE IF NOT EXISTS central_roster (
	network TEXT PRIMARY KEY,
	seq     INTEGER NOT NULL,
	roster  TEXT NOT NULL
);
`

// maxCentralRosterBytes bounds a roster POST: MaxCentralRosterMembers
// members of about 120 bytes each, with room for the rest.
const maxCentralRosterBytes = 96 * 1024

// CentralRosterStore keeps each network's roster in SQLite.
type CentralRosterStore struct {
	db *sql.DB
	mu sync.Mutex // serializes Put's read-check-write
}

// NewCentralRosterStore creates the roster table if needed.
func NewCentralRosterStore(db *sql.DB) (*CentralRosterStore, error) {
	if _, err := db.Exec(centralRosterSchema); err != nil {
		return nil, fmt.Errorf("hub: create central_roster table: %w", err)
	}
	return &CentralRosterStore{db: db}, nil
}

// Put stores raw, r's JSON as received, as network's roster. It returns
// ErrCentralStale unless r.Seq is above the stored roster's.
func (rs *CentralRosterStore) Put(r *protocol.CentralRoster, raw []byte) error {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	var seq int64
	switch err := rs.db.QueryRow("SELECT seq FROM central_roster WHERE network = ?", r.Network).Scan(&seq); {
	case errors.Is(err, sql.ErrNoRows):
	case err != nil:
		return fmt.Errorf("hub: read roster: %w", err)
	case r.Seq <= seq:
		return ErrCentralStale
	}
	if _, err := rs.db.Exec(
		`INSERT INTO central_roster (network, seq, roster) VALUES (?, ?, ?)
		 ON CONFLICT(network) DO UPDATE SET seq = excluded.seq, roster = excluded.roster`,
		r.Network, r.Seq, string(raw)); err != nil {
		return fmt.Errorf("hub: store roster: %w", err)
	}
	return nil
}

// Get returns network's roster as stored, or nil when it has none.
func (rs *CentralRosterStore) Get(network string) (json.RawMessage, error) {
	var raw string
	switch err := rs.db.QueryRow("SELECT roster FROM central_roster WHERE network = ?", network).Scan(&raw); {
	case errors.Is(err, sql.ErrNoRows):
		return nil, nil
	case err != nil:
		return nil, fmt.Errorf("hub: read roster: %w", err)
	}
	return json.RawMessage(raw), nil
}

// handlePostCentralRoster: POST /v3net/v1/{network}/central/roster (auth
// required). The body is the coordinator's signed roster. It is accepted
// from the coordinator in force only, signed by that key, valid for the
// network, and with a seq above the stored one; a central_roster_updated
// event follows.
func (h *Hub) handlePostCentralRoster(w http.ResponseWriter, r *http.Request, network string) {
	nodeID := r.Header.Get(headerNodeID)
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxCentralRosterBytes+1))
	if err != nil || len(raw) > maxCentralRosterBytes {
		jsonError(w, "roster too large", http.StatusRequestEntityTooLarge)
		return
	}
	var roster protocol.CentralRoster
	if err := json.Unmarshal(raw, &roster); err != nil {
		jsonError(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if err := roster.Validate(network); err != nil {
		jsonError(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	if err := roster.Verify(); err != nil {
		jsonError(w, "roster signature invalid: "+err.Error(), http.StatusForbidden)
		return
	}

	// The coordinator is the holder of the key in force, as for the NAL.
	h.nalMu.Lock()
	existing, err := h.nalStore.Get(network)
	var hs []protocol.CoordHandover
	if err == nil && existing != nil {
		hs, err = h.nalStore.Handovers(network)
	}
	h.nalMu.Unlock()
	if err != nil {
		slog.Error("v3net hub: read NAL for roster", "network", network, "error", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	if existing == nil {
		jsonError(w, "the network has no NAL, so no coordinator", http.StatusConflict)
		return
	}
	key := coordKeyInForce(existing, hs)
	if nodeIDOf(key) != nodeID {
		jsonError(w, "only the coordinator may publish the roster", http.StatusForbidden)
		return
	}
	if roster.CoordPubKeyB64 != key {
		jsonError(w, "roster is not signed by the coordinator key in force", http.StatusForbidden)
		return
	}

	switch err := h.roster.Put(&roster, raw); {
	case errors.Is(err, ErrCentralStale):
		jsonError(w, "seq is not above the stored roster's", http.StatusConflict)
		return
	case err != nil:
		slog.Error("v3net hub: store roster", "network", network, "error", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	meta, err := protocol.NewEventMeta(protocol.CentralEventTTL)
	if err == nil {
		if ev, err := protocol.NewEvent(protocol.EventCentralRosterUpdated, protocol.CentralRosterEventPayload{
			EventMeta: meta, Network: network, Seq: roster.Seq,
		}); err == nil {
			h.broadcaster.Publish(network, ev)
		}
	}
	writeJSON(w, http.StatusOK, protocol.CentralPublishResponse{OK: true, Seq: roster.Seq})
}

// handleGetCentralRoster: GET /v3net/v1/{network}/central/roster (auth
// required) serves the stored roster as the coordinator signed it, or 404
// when the network has none.
func (h *Hub) handleGetCentralRoster(w http.ResponseWriter, r *http.Request, network string) {
	raw, err := h.roster.Get(network)
	if err != nil {
		slog.Error("v3net hub: get roster", "network", network, "error", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	if raw == nil {
		jsonError(w, "no roster", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(raw)
}
