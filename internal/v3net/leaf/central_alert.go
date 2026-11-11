package leaf

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// V3Net Central activity alerts on a leaf (see protocol.CentralAlert).
//
// Sending: the BBS asks the leaf to send an alert when a caller who agreed
// to it does something (SendAlert); the leaf checks that the sysop has
// that kind of alert on for this network and the hub relays alerts, signs
// the alert and posts it, retrying a failed post under the same ID until
// the alert would expire. The hub drops a retry it has already relayed.
// The BBS decides whether the caller agreed; the leaf never sends an alert
// on its own, so a stream that drops, or a hub that drops the board's
// presence, is never reported as anyone logging off.
//
// Receiving: each central_alert event the hub relays is checked again
// here, as the board signed it: its form, its signature, its expiry, and
// that it is from another board, is a kind the sysop has on, and was not
// seen before. Chat joins also need network chat on here, and an alert
// from a caller on the sysop's chat block list is dropped. What survives
// goes to Config.OnAlert, which decides who on the BBS sees it.

// Alert timing.
var (
	// centralAlertTTL is how long an alert is valid for: news a caller can
	// still use, inside protocol.MaxCentralAlertTTL.
	centralAlertTTL = 2 * time.Minute
	// centralAlertRetryMin and Max bound the backoff of a failed send.
	centralAlertRetryMin = 2 * time.Second
	centralAlertRetryMax = 20 * time.Second
	// centralAlertMargin is how long before its expiry an alert stops
	// being retried: one that reaches the hub later is no use to anyone.
	centralAlertMargin = 15 * time.Second
)

// centralAlertsSeen is how many alert IDs a leaf remembers, to drop a
// repeat. Alerts live minutes, so this covers a busy network's.
const centralAlertsSeen = 1024

// Alert send errors.
var (
	// ErrAlertsOff is returned by SendAlert when the sysop does not have
	// that kind of alert on for this network.
	ErrAlertsOff = errors.New("leaf: alerts of that kind are off on this network")
	// ErrAlertsUnsupported is returned by SendAlert when the hub does not
	// relay alerts.
	ErrAlertsUnsupported = errors.New("leaf: hub does not relay activity alerts")
)

// AlertSpec is one alert the BBS asks a leaf to send. ID is the event's
// ID (protocol.NewCentralAlertID), the same on every network the event is
// sent to. Room is set for a chat join, Area for a post.
type AlertSpec struct {
	Kind   string
	ID     string
	Handle string
	Room   string
	Area   string
}

// alertState is the leaf's alert bookkeeping.
type alertState struct {
	mu       sync.Mutex
	seen     map[string]bool // node \x00 id, of alerts acted on
	order    []string        // seen's keys, oldest first, at most centralAlertsSeen
	lastSent time.Time
	lastErr  string
}

// AlertsEnabled reports whether the sysop has alerts of kind on for this
// network. Every kind needs Central on; a chat join also needs network
// chat.
func (l *Leaf) AlertsEnabled(kind string) bool {
	if !l.cfg.CentralEnabled || !l.cfg.CentralAlerts[kind] {
		return false
	}
	return kind != protocol.CentralAlertChatJoin || l.cfg.CentralChat
}

// AlertStatus is what a leaf knows about activity alerts.
type AlertStatus struct {
	// Enabled is the sysop's switches by kind, as AlertsEnabled reads
	// them; Supported whether the hub relays alerts.
	Enabled   map[string]bool
	Supported bool
	// LastSent is when an alert last reached the hub (zero if none this
	// run) and LastError the latest failure.
	LastSent  time.Time
	LastError string
}

// AlertStatus returns the leaf's alert state.
func (l *Leaf) AlertStatus() AlertStatus {
	st := AlertStatus{Enabled: map[string]bool{}}
	for _, k := range protocol.CentralAlertKinds {
		st.Enabled[k] = l.AlertsEnabled(k)
	}
	st.Supported, _ = l.HubSupports(protocol.FeatureCentralAlerts)
	l.alerts.mu.Lock()
	st.LastSent, st.LastError = l.alerts.lastSent, l.alerts.lastErr
	l.alerts.mu.Unlock()
	return st
}

// SendAlert signs spec as an alert from this board and posts it to the
// hub, retrying a failure under the same ID until the alert would expire
// or ctx ends. It returns ErrAlertsOff or ErrAlertsUnsupported, sending
// nothing, when the alert is not offered here, and an error for a spec
// that does not make a valid alert or a hub that refuses it.
func (l *Leaf) SendAlert(ctx context.Context, spec AlertSpec) error {
	if !l.AlertsEnabled(spec.Kind) {
		return ErrAlertsOff
	}
	if ok, _ := l.HubSupports(protocol.FeatureCentralAlerts); !ok {
		return ErrAlertsUnsupported
	}
	// A board with no name is named by its node ID, as the hub names it.
	bbs := protocol.CleanCentralText(l.cfg.BBSName, protocol.MaxBBSNameLen)
	if bbs == "" {
		bbs = l.cfg.Keystore.NodeID()
	}
	now := time.Now().UTC().Truncate(time.Second)
	a := &protocol.CentralAlert{
		V3NetCentral: protocol.CentralAlertVersion,
		Kind:         spec.Kind,
		ID:           spec.ID,
		Network:      l.cfg.Network,
		BBS:          bbs,
		Handle:       protocol.CleanCentralText(spec.Handle, protocol.MaxCentralHandleLen),
		Room:         spec.Room,
		Area:         spec.Area,
		PublishedAt:  now.Format(time.RFC3339),
		ExpiresAt:    now.Add(centralAlertTTL).Format(time.RFC3339),
	}
	if err := a.Sign(l.cfg.Keystore); err != nil {
		return err
	}
	if err := a.Validate(l.cfg.Network); err != nil {
		return fmt.Errorf("leaf: alert: %w", err)
	}
	data, err := json.Marshal(a)
	if err != nil {
		return fmt.Errorf("leaf: marshal alert: %w", err)
	}
	deadline := a.Expires().Add(-centralAlertMargin)
	wait := centralAlertRetryMin
	for {
		retry, err := l.postAlert(ctx, data)
		l.alerts.mu.Lock()
		if err == nil {
			l.alerts.lastSent, l.alerts.lastErr = time.Now(), ""
		} else {
			l.alerts.lastErr = err.Error()
		}
		l.alerts.mu.Unlock()
		if err == nil || !retry {
			return err
		}
		if time.Now().Add(wait).After(deadline) {
			return fmt.Errorf("leaf: alert not sent before it went stale: %w", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
		wait = min(wait*2, centralAlertRetryMax)
	}
}

// postAlert posts one signed alert. retry is whether a failure may pass:
// the hub could not be reached, was busy, or failed on its side. A refusal
// of the alert itself is final.
func (l *Leaf) postAlert(ctx context.Context, data []byte) (retry bool, err error) {
	path := fmt.Sprintf("/v3net/v1/%s/central/alerts", l.cfg.Network)
	resp, err := l.signedDo(ctx, http.MethodPost, path, data)
	if err != nil {
		return true, err
	}
	body, _ := readBody(resp.Body, maxRespBytes)
	_ = resp.Body.Close() // read side; already read in full
	switch {
	case resp.StatusCode == http.StatusOK:
		return false, nil
	case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
		return true, fmt.Errorf("leaf: POST %s returned %d: %s", path, resp.StatusCode, hubErrorText(body))
	default:
		return false, fmt.Errorf("leaf: POST %s returned %d: %s", path, resp.StatusCode, hubErrorText(body))
	}
}

// dispatchAlertEvent checks a central_alert event and hands what survives
// to Config.OnAlert (see the notes at the top of this file).
func (l *Leaf) dispatchAlertEvent(ev protocol.Event) {
	if ev.Type != protocol.EventCentralAlert || l.cfg.OnAlert == nil {
		return
	}
	var a protocol.CentralAlert
	if err := json.Unmarshal(ev.Data, &a); err != nil {
		return
	}
	var why string
	switch {
	case a.Validate(l.cfg.Network) != nil:
		why = "invalid"
	case a.Expired(time.Now()):
		why = "expired"
	case a.Published().After(time.Now().Add(protocol.MaxCentralClockSkew)):
		why = "dated ahead"
	case a.Verify() != nil:
		why = "signature failed"
	}
	if why != "" {
		slog.Debug("leaf: dropped central alert", "network", l.cfg.Network, "node", a.NodeID, "reason", why)
		return
	}
	if a.NodeID == l.cfg.Keystore.NodeID() || !l.AlertsEnabled(a.Kind) || l.chatSessions.isBlocked(a.Handle, a.NodeID) ||
		!l.rosterAdmits(a.NodeID, a.OriginPubKeyB64) {
		return
	}
	if !l.alerts.claim(a.NodeID, a.ID) {
		slog.Debug("leaf: dropped repeated central alert", "network", l.cfg.Network, "node", a.NodeID, "id", a.ID)
		return
	}
	l.cfg.OnAlert(a)
}

// claim records an alert as acted on and reports whether it is new.
func (s *alertState) claim(node, id string) bool {
	key := node + "\x00" + id
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.seen[key] {
		return false
	}
	if s.seen == nil {
		s.seen = make(map[string]bool)
	}
	if len(s.order) >= centralAlertsSeen {
		delete(s.seen, s.order[0])
		s.order = s.order[1:]
	}
	s.seen[key] = true
	s.order = append(s.order, key)
	return true
}
