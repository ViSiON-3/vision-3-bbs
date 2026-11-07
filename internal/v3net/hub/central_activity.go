package hub

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// Central activity on a hub: each board's latest signed presence and
// last-callers snapshots (see protocol.CentralActivity), one row per node,
// network and kind, served back byte for byte. As for listings, the
// highest seq a node has published is kept in its own table, so a
// withdrawal does not let an older snapshot be replayed over it.
//
// Presence is only ever served for a node whose event stream is open, and
// the node's presence row is deleted when its last stream closes: a board
// that drops off the network takes its callers off Who's Online at once.
// Every snapshot also carries its own short expiry, after which it is
// neither served nor kept.

const centralActivitySchema = `
CREATE TABLE IF NOT EXISTS central_activity (
	node_id    TEXT NOT NULL,
	network    TEXT NOT NULL,
	kind       TEXT NOT NULL,
	seq        INTEGER NOT NULL,
	snapshot   TEXT NOT NULL,
	expires_at TEXT NOT NULL,
	PRIMARY KEY (node_id, network, kind)
);
CREATE TABLE IF NOT EXISTS central_activity_seq (
	node_id TEXT NOT NULL,
	network TEXT NOT NULL,
	kind    TEXT NOT NULL,
	seq     INTEGER NOT NULL,
	PRIMARY KEY (node_id, network, kind)
);
`

// Activity publication limits, per node, network and kind. A board
// publishes on each logon and logoff (coalesced) and on a heartbeat, so a
// busy board may send several in a row, but not a stream of them.
const (
	centralActivityInterval = 2 * time.Second
	centralActivityBurst    = 10
	// maxCentralActivityBytes bounds a snapshot PUT. The largest snapshot
	// within protocol's limits is under 12KB.
	maxCentralActivityBytes = 32 * 1024
)

// CentralActivityStore keeps activity snapshots in SQLite.
type CentralActivityStore struct {
	db *sql.DB
	mu sync.Mutex // serializes Put's read-check-write
}

// NewCentralActivityStore creates the activity tables if needed.
func NewCentralActivityStore(db *sql.DB) (*CentralActivityStore, error) {
	if _, err := db.Exec(centralActivitySchema); err != nil {
		return nil, fmt.Errorf("hub: create central_activity table: %w", err)
	}
	return &CentralActivityStore{db: db}, nil
}

// Put stores a validated, verified snapshot as the node's entry for its
// network and kind, raw being its JSON as received. It returns
// ErrCentralStale if the node's seq watermark for the kind is not lower,
// and ErrCentralFull if the network already holds
// protocol.MaxCentralListings snapshots of the kind and this node has none.
func (cs *CentralActivityStore) Put(a *protocol.CentralActivity, raw []byte) error {
	expires, err := time.Parse(time.RFC3339, a.ExpiresAt)
	if err != nil {
		return fmt.Errorf("hub: activity expires_at: %w", err)
	}
	cs.mu.Lock()
	defer cs.mu.Unlock()

	var mark int64
	err = cs.db.QueryRow(
		"SELECT seq FROM central_activity_seq WHERE node_id = ? AND network = ? AND kind = ?",
		a.NodeID, a.Network, a.Kind).Scan(&mark)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("hub: read activity watermark: %w", err)
	}
	if err == nil && a.Seq <= mark {
		return ErrCentralStale
	}
	var rows int
	if err := cs.db.QueryRow(
		"SELECT COUNT(*) FROM central_activity WHERE node_id = ? AND network = ? AND kind = ?",
		a.NodeID, a.Network, a.Kind).Scan(&rows); err != nil {
		return fmt.Errorf("hub: read activity: %w", err)
	}
	if rows == 0 {
		var n int
		if err := cs.db.QueryRow(
			"SELECT COUNT(*) FROM central_activity WHERE network = ? AND kind = ?", a.Network, a.Kind).Scan(&n); err != nil {
			return fmt.Errorf("hub: count activity: %w", err)
		}
		if n >= protocol.MaxCentralListings {
			return ErrCentralFull
		}
	}
	if _, err := cs.db.Exec(
		`INSERT INTO central_activity_seq (node_id, network, kind, seq) VALUES (?, ?, ?, ?)
		 ON CONFLICT(node_id, network, kind) DO UPDATE SET seq = excluded.seq`,
		a.NodeID, a.Network, a.Kind, a.Seq); err != nil {
		return fmt.Errorf("hub: store activity watermark: %w", err)
	}
	if _, err := cs.db.Exec(
		`INSERT INTO central_activity (node_id, network, kind, seq, snapshot, expires_at)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(node_id, network, kind) DO UPDATE SET
		   seq = excluded.seq, snapshot = excluded.snapshot, expires_at = excluded.expires_at`,
		a.NodeID, a.Network, a.Kind, a.Seq, string(raw), expires.UTC().Format(time.RFC3339)); err != nil {
		return fmt.Errorf("hub: store activity: %w", err)
	}
	return nil
}

// Delete removes the node's snapshot of kind on network, keeping its seq
// watermark. It reports whether there was one.
func (cs *CentralActivityStore) Delete(nodeID, network, kind string) (bool, error) {
	res, err := cs.db.Exec("DELETE FROM central_activity WHERE node_id = ? AND network = ? AND kind = ?", nodeID, network, kind)
	if err != nil {
		return false, fmt.Errorf("hub: delete activity: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// CentralActivityRow is one stored snapshot.
type CentralActivityRow struct {
	NodeID string
	Raw    json.RawMessage
}

// List returns the network's unexpired snapshots of kind at now, ordered
// by node ID, as stored.
func (cs *CentralActivityStore) List(network, kind string, now time.Time) ([]CentralActivityRow, error) {
	rows, err := cs.db.Query(
		`SELECT node_id, snapshot FROM central_activity
		 WHERE network = ? AND kind = ? AND expires_at > ? ORDER BY node_id`,
		network, kind, now.UTC().Format(time.RFC3339))
	if err != nil {
		return nil, fmt.Errorf("hub: list activity: %w", err)
	}
	defer func() { _ = rows.Close() }() // read-only
	var out []CentralActivityRow
	for rows.Next() {
		var r CentralActivityRow
		var raw string
		if err := rows.Scan(&r.NodeID, &raw); err != nil {
			return nil, fmt.Errorf("hub: scan activity: %w", err)
		}
		r.Raw = json.RawMessage(raw)
		out = append(out, r)
	}
	return out, rows.Err()
}

// PruneExpired removes every snapshot whose expires_at is at or before
// now, returning how many went.
func (cs *CentralActivityStore) PruneExpired(now time.Time) (int64, error) {
	res, err := cs.db.Exec("DELETE FROM central_activity WHERE expires_at <= ?", now.UTC().Format(time.RFC3339))
	if err != nil {
		return 0, fmt.Errorf("hub: prune activity: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// handlePutCentralActivity: PUT /v3net/v1/{network}/central/{kind} (auth
// required). The body is the node's signed snapshot of kind. It is
// accepted on the same terms as a listing: it validates, its signature
// verifies under the node's registered key, it is the authenticated
// node's, it was not published in the future or has already expired, and
// its seq is above the stored one.
func (h *Hub) handlePutCentralActivity(w http.ResponseWriter, r *http.Request, network, kind string) {
	nodeID := r.Header.Get(headerNodeID)
	if !h.activityLimiter.Allow(nodeID + ":" + network + ":" + kind) {
		jsonError(w, "too many updates; try again shortly", http.StatusTooManyRequests)
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxCentralActivityBytes+1))
	if err != nil || len(raw) > maxCentralActivityBytes {
		jsonError(w, "snapshot too large", http.StatusRequestEntityTooLarge)
		return
	}
	var a protocol.CentralActivity
	if err := json.Unmarshal(raw, &a); err != nil {
		jsonError(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if err := a.Validate(network, kind); err != nil {
		jsonError(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	if a.NodeID != nodeID {
		jsonError(w, "node_id does not match authenticated node", http.StatusForbidden)
		return
	}
	if err := a.Verify(); err != nil {
		slog.Warn("v3net hub: rejected central activity with bad signature", "node", nodeID, "network", network, "kind", kind, "error", err)
		jsonError(w, "snapshot signature invalid: "+err.Error(), http.StatusForbidden)
		return
	}
	pub, _ := a.OriginPubKey() // Verify decoded it
	if registered := h.subscribers.GetPubKey(nodeID, network); registered == nil || !registered.Equal(pub) {
		jsonError(w, "origin_pubkey_b64 is not the node's registered key", http.StatusForbidden)
		return
	}
	now := time.Now()
	if a.Published().After(now.Add(protocol.MaxCentralClockSkew)) {
		jsonError(w, "published_at is in the future", http.StatusUnprocessableEntity)
		return
	}
	if a.Expired(now) {
		jsonError(w, "snapshot has already expired", http.StatusUnprocessableEntity)
		return
	}
	switch err := h.activity.Put(&a, raw); {
	case errors.Is(err, ErrCentralStale):
		jsonError(w, "seq is not above the stored snapshot's", http.StatusConflict)
		return
	case errors.Is(err, ErrCentralFull):
		jsonError(w, "network is full", http.StatusUnprocessableEntity)
		return
	case err != nil:
		slog.Error("v3net hub: store central activity", "node", nodeID, "network", network, "kind", kind, "error", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	h.publishActivityEvent(network, kind, nodeID, a.Seq)
	writeJSON(w, http.StatusOK, protocol.CentralPublishResponse{OK: true, Seq: a.Seq})
}

// handleDeleteCentralActivity: DELETE /v3net/v1/{network}/central/{kind}
// (auth required) withdraws the node's snapshot of kind. It answers 204
// whether or not there was one.
func (h *Hub) handleDeleteCentralActivity(w http.ResponseWriter, r *http.Request, network, kind string) {
	nodeID := r.Header.Get(headerNodeID)
	removed, err := h.activity.Delete(nodeID, network, kind)
	if err != nil {
		slog.Error("v3net hub: delete central activity", "node", nodeID, "network", network, "kind", kind, "error", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	if removed {
		h.publishActivityEvent(network, kind, nodeID, 0)
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleGetCentralActivity: GET /v3net/v1/{network}/central/{kind} (auth
// required) serves every unexpired snapshot of kind, each as stored, with
// whether its board is connected now. Presence is served only for
// connected boards.
func (h *Hub) handleGetCentralActivity(w http.ResponseWriter, r *http.Request, network, kind string) {
	now := time.Now()
	rows, err := h.activity.List(network, kind, now)
	if err != nil {
		slog.Error("v3net hub: list central activity", "network", network, "kind", kind, "error", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	boards := make([]json.RawMessage, 0, len(rows))
	for _, row := range rows {
		online := h.online.online(row.NodeID, network)
		if kind == protocol.CentralKindPresence && !online {
			continue
		}
		b, err := json.Marshal(struct {
			Activity json.RawMessage `json:"activity"`
			Online   bool            `json:"online"`
		}{row.Raw, online})
		if err != nil {
			continue
		}
		boards = append(boards, b)
	}
	writeJSON(w, http.StatusOK, struct {
		Network     string            `json:"network"`
		Kind        string            `json:"kind"`
		GeneratedAt string            `json:"generated_at"`
		Boards      []json.RawMessage `json:"boards"`
	}{network, kind, now.UTC().Format(time.RFC3339), boards})
}

// dropPresence removes a node's presence on network when its last event
// stream has closed, announcing it if there was any. The caller holds
// chatStreamMu, so a replacement stream cannot have opened meanwhile.
func (h *Hub) dropPresence(nodeID, network string) {
	removed, err := h.activity.Delete(nodeID, network, protocol.CentralKindPresence)
	if err != nil {
		slog.Warn("v3net hub: drop presence of disconnected node", "node", nodeID, "network", network, "error", err)
		return
	}
	if removed {
		h.publishActivityEvent(network, protocol.CentralKindPresence, nodeID, 0)
	}
}

// publishActivityEvent broadcasts a central_activity_updated event.
func (h *Hub) publishActivityEvent(network, kind, nodeID string, seq int64) {
	meta, err := protocol.NewEventMeta(protocol.CentralEventTTL)
	if err != nil {
		slog.Error("v3net hub: central event id", "error", err)
		return
	}
	ev, err := protocol.NewEvent(protocol.EventCentralActivityUpdated, protocol.CentralActivityEventPayload{
		EventMeta: meta, Network: network, NodeID: nodeID, Kind: kind, Seq: seq,
	})
	if err != nil {
		return
	}
	h.broadcaster.Publish(network, ev)
}

// centralActivityPruneInterval is how often expired snapshots are removed.
// Presence lives minutes, so this runs more often than the listing pruner;
// expired rows are left out of every feed served meanwhile anyway.
const centralActivityPruneInterval = 5 * time.Minute

// startActivityPruner removes expired snapshots until ctx ends.
func (h *Hub) startActivityPruner(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(centralActivityPruneInterval)
		defer ticker.Stop()
		for {
			if n, err := h.activity.PruneExpired(time.Now()); err != nil {
				slog.Warn("hub: prune expired central activity", "error", err)
			} else if n > 0 {
				slog.Debug("hub: pruned expired central activity", "count", n)
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}
