package hub

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// maxCentralListingBytes bounds the body of a listing PUT. The largest
// listing within protocol's limits is well under this.
const maxCentralListingBytes = 16 * 1024

// hubFeatures names the optional features this hub serves on every
// network, for NetworkInfo.Features.
var hubFeatures = []string{protocol.FeatureCentralDirectory, protocol.FeatureCentralPresence, protocol.FeatureCentralLastCallers}

// handlePutCentralListing: PUT /v3net/v1/{network}/central/listing (auth
// required). The body is the node's signed listing. It is accepted when it
// validates for the network, its signature verifies, its key is the key the
// node subscribed with, its node_id is the authenticated node, it was not
// published in the future, and its seq is above the stored one. The listing
// is stored as sent and a central_listing_updated event published.
func (h *Hub) handlePutCentralListing(w http.ResponseWriter, r *http.Request, network string) {
	nodeID := r.Header.Get(headerNodeID)
	if !h.centralLimiter.Allow(nodeID + ":" + network) {
		jsonError(w, "too many listing updates; try again shortly", http.StatusTooManyRequests)
		return
	}

	raw, err := io.ReadAll(io.LimitReader(r.Body, maxCentralListingBytes+1))
	if err != nil || len(raw) > maxCentralListingBytes {
		jsonError(w, "listing too large", http.StatusRequestEntityTooLarge)
		return
	}
	var listing protocol.CentralListing
	if err := json.Unmarshal(raw, &listing); err != nil {
		jsonError(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if err := listing.Validate(network); err != nil {
		jsonError(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	if listing.NodeID != nodeID {
		jsonError(w, "node_id does not match authenticated node", http.StatusForbidden)
		return
	}
	if err := listing.Verify(); err != nil {
		slog.Warn("v3net hub: rejected central listing with bad signature", "node", nodeID, "network", network, "error", err)
		jsonError(w, "listing signature invalid: "+err.Error(), http.StatusForbidden)
		return
	}
	pub, _ := listing.OriginPubKey() // Verify decoded it
	if registered := h.subscribers.GetPubKey(nodeID, network); registered == nil || !registered.Equal(pub) {
		jsonError(w, "origin_pubkey_b64 is not the node's registered key", http.StatusForbidden)
		return
	}
	now := time.Now()
	if listing.Published().After(now.Add(protocol.MaxCentralClockSkew)) {
		jsonError(w, "published_at is in the future", http.StatusUnprocessableEntity)
		return
	}
	if listing.Expired(now) {
		jsonError(w, "listing has already expired", http.StatusUnprocessableEntity)
		return
	}

	switch err := h.central.Put(&listing, raw, now); {
	case errors.Is(err, ErrCentralStale):
		jsonError(w, "seq is not above the stored listing's", http.StatusConflict)
		return
	case errors.Is(err, ErrCentralFull):
		jsonError(w, "directory is full", http.StatusUnprocessableEntity)
		return
	case err != nil:
		slog.Error("v3net hub: store central listing", "node", nodeID, "network", network, "error", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	h.publishCentralEvent(network, protocol.EventCentralListingUpdated, nodeID, listing.Seq)
	writeJSON(w, http.StatusOK, protocol.CentralPublishResponse{OK: true, Seq: listing.Seq})
}

// handleDeleteCentralListing: DELETE /v3net/v1/{network}/central/listing
// (auth required) withdraws the node's listing. It answers 204 whether or
// not there was one, so a withdrawal can be repeated; an event is published
// only when a listing went.
func (h *Hub) handleDeleteCentralListing(w http.ResponseWriter, r *http.Request, network string) {
	nodeID := r.Header.Get(headerNodeID)
	removed, err := h.central.Delete(nodeID, network)
	if err != nil {
		slog.Error("v3net hub: delete central listing", "node", nodeID, "network", network, "error", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	if removed {
		h.publishCentralEvent(network, protocol.EventCentralListingWithdrawn, nodeID, 0)
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleGetCentralDirectory: GET /v3net/v1/{network}/central/listings (auth
// required) serves every unexpired listing on the network, each as stored,
// with the hub's last-seen time and whether the node is connected now.
func (h *Hub) handleGetCentralDirectory(w http.ResponseWriter, r *http.Request, network string) {
	now := time.Now()
	rows, err := h.central.List(network, now)
	if err != nil {
		slog.Error("v3net hub: list central listings", "network", network, "error", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	entries := make([]json.RawMessage, 0, len(rows))
	for _, row := range rows {
		// The listing is spliced in as stored so its signature still
		// covers it; the hub's fields go around it.
		entry, err := json.Marshal(struct {
			Listing    json.RawMessage `json:"listing"`
			LastSeenAt string          `json:"last_seen_at,omitempty"`
			Online     bool            `json:"online"`
		}{row.Raw, row.LastSeenAt, h.online.online(row.NodeID, network)})
		if err != nil {
			continue
		}
		entries = append(entries, entry)
	}
	writeJSON(w, http.StatusOK, struct {
		Network     string            `json:"network"`
		GeneratedAt string            `json:"generated_at"`
		Listings    []json.RawMessage `json:"listings"`
	}{network, now.UTC().Format(time.RFC3339), entries})
}

// publishCentralEvent broadcasts a Central listing event for nodeID with
// fresh event metadata.
func (h *Hub) publishCentralEvent(network, eventType, nodeID string, seq int64) {
	meta, err := protocol.NewEventMeta(protocol.CentralEventTTL)
	if err != nil {
		slog.Error("v3net hub: central event id", "error", err)
		return
	}
	ev, err := protocol.NewEvent(eventType, protocol.CentralListingEventPayload{
		EventMeta: meta, Network: network, NodeID: nodeID, Seq: seq,
	})
	if err != nil {
		return
	}
	h.broadcaster.Publish(network, ev)
}

// noteNodeSeen records that nodeID is in contact now, for the directory's
// last-seen time. Only a node with a listing has anything to update.
func (h *Hub) noteNodeSeen(nodeID, network string) {
	if err := h.central.TouchSeen(nodeID, network, time.Now()); err != nil {
		slog.Debug("v3net hub: touch central listing", "node", nodeID, "error", err)
	}
}
