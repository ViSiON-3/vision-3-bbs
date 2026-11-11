package hub

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// Central activity alerts on a hub (see protocol.CentralAlert). The hub
// checks each alert a board sends as it checks the board's listing, then
// sends it on, byte for byte, to every board on the network as a
// central_alert event. It stores no alert: it keeps only the IDs it has
// relayed, until the alerts expire, so a board's retry of an alert the
// hub already sent, or a replay of one, reaches no one twice.

// Alert limits, per node and network. A board sends one alert per caller
// action, so a busy board may send a few together, but not a stream.
const (
	centralAlertInterval = time.Second
	centralAlertBurst    = 30
	// maxCentralAlertBytes bounds an alert POST. The largest alert within
	// protocol's limits is under 1KB.
	maxCentralAlertBytes = 4 * 1024
	// centralAlertPruneInterval is how often expired IDs are forgotten.
	centralAlertPruneInterval = time.Minute
)

// alertSeen is the IDs of the alerts the hub has relayed, by node and
// network, each until its alert expires. A replay after that is refused
// as expired, so nothing is forgotten that could be sent again.
type alertSeen struct {
	mu  sync.Mutex
	ids map[string]time.Time // node \x00 network \x00 id -> expires
}

func newAlertSeen() *alertSeen { return &alertSeen{ids: make(map[string]time.Time)} }

// claim records the alert's ID and reports whether it is new. A repeat
// returns false and changes nothing.
func (s *alertSeen) claim(a *protocol.CentralAlert) bool {
	key := a.NodeID + "\x00" + a.Network + "\x00" + a.ID
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.ids[key]; ok {
		return false
	}
	s.ids[key] = a.Expires()
	return true
}

// prune forgets the IDs whose alerts have expired at now.
func (s *alertSeen) prune(now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, exp := range s.ids {
		if !exp.After(now) {
			delete(s.ids, k)
		}
	}
}

// startAlertPruner forgets expired alert IDs until ctx ends.
func (h *Hub) startAlertPruner(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(centralAlertPruneInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				h.alertSeen.prune(now)
			}
		}
	}()
}

// handlePostCentralAlert: POST /v3net/v1/{network}/central/alerts (auth
// required). The body is one of the node's signed alerts. It is accepted
// when it validates for the network, is the authenticated node's, its
// signature verifies under the node's registered key, it was not published
// in the future and has not expired. A new alert is sent on to the
// network; one already sent is acknowledged as a duplicate and not sent
// again.
func (h *Hub) handlePostCentralAlert(w http.ResponseWriter, r *http.Request, network string) {
	nodeID := r.Header.Get(headerNodeID)
	if !h.alertLimiter.Allow(nodeID + ":" + network) {
		jsonError(w, "too many alerts; try again shortly", http.StatusTooManyRequests)
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxCentralAlertBytes+1))
	if err != nil || len(raw) > maxCentralAlertBytes {
		jsonError(w, "alert too large", http.StatusRequestEntityTooLarge)
		return
	}
	var a protocol.CentralAlert
	if err := json.Unmarshal(raw, &a); err != nil {
		jsonError(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if err := a.Validate(network); err != nil {
		jsonError(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	if a.NodeID != nodeID {
		jsonError(w, "node_id does not match authenticated node", http.StatusForbidden)
		return
	}
	if err := a.Verify(); err != nil {
		slog.Warn("v3net hub: rejected central alert with bad signature", "node", nodeID, "network", network, "error", err)
		jsonError(w, "alert signature invalid: "+err.Error(), http.StatusForbidden)
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
		jsonError(w, "alert has already expired", http.StatusUnprocessableEntity)
		return
	}
	if !h.alertSeen.claim(&a) {
		writeJSON(w, http.StatusOK, protocol.CentralAlertResponse{OK: true, Duplicate: true})
		return
	}
	// The alert goes on as the board sent it, compacted to one line: its
	// signature covers the parsed fields, not the bytes, and an event's
	// data is one SSE line, so a newline in the body would split the event
	// the hub signs from the one a leaf reads, and every leaf would drop
	// its stream. The event stream's own signature covers the relay.
	var line bytes.Buffer
	if err := json.Compact(&line, raw); err != nil {
		jsonError(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	h.broadcaster.Publish(network, protocol.Event{Type: protocol.EventCentralAlert, Data: json.RawMessage(line.Bytes())})
	writeJSON(w, http.StatusOK, protocol.CentralAlertResponse{OK: true})
}
