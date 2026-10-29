package leaf

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/keystore"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/nal"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// A leaf pins two keys per network: the coordinator key that signs the NAL,
// and the hub key /info serves. A NAL carries its signer's key, so verifying
// it against that key alone proves nothing about who signed it; comparing
// the key with a pinned one does. Keys come from the sysop's config (pasted
// in, or copied from the registry when the network was joined) or, failing
// that, are pinned on first use. A key that later differs from the pin is
// rejected and reported, never adopted: the sysop accepts it by replacing or
// blanking the pin in v3net.json.

// KeyKind names which of a network's keys a KeyEvent is about.
type KeyKind string

// The keys a leaf pins.
const (
	// KeyCoordinator is the network coordinator's key, which signs the NAL.
	KeyCoordinator KeyKind = "coordinator"
	// KeyHub is the hub's own node key, served by /info.
	KeyHub KeyKind = "hub"
)

// ErrHubKeyMismatch means the hub's /info served a key other than the one
// pinned for it. Check for it with errors.Is.
var ErrHubKeyMismatch = errors.New("leaf: hub key does not match the pinned key")

// ErrHubNodeIDMismatch means /info's hub_node_id is not the node ID derived
// from its hub_pubkey_b64.
var ErrHubNodeIDMismatch = errors.New("leaf: hub node ID does not match the hub public key")

// KeyEvent describes a key the leaf pinned on first use, or a key the hub
// offered that differs from the pin. It is passed to Config.OnKeyPinned and
// Config.OnKeyChanged.
type KeyEvent struct {
	Network string
	HubURL  string
	Kind    KeyKind
	// Pinned is the key in force: for OnKeyPinned the key just pinned, for
	// OnKeyChanged the key the leaf keeps trusting.
	Pinned string
	// Offered is the key the hub served. For OnKeyPinned it equals Pinned.
	Offered string
}

// keyPins is the leaf's pinned and pending keys, guarded by Leaf.pinMu.
type keyPins struct {
	coord, hub               string
	pendingCoord, pendingHub string
}

// slots returns pointers to the pin and the pending key for kind.
func (p *keyPins) slots(kind KeyKind) (pin, pending *string) {
	if kind == KeyHub {
		return &p.hub, &p.pendingHub
	}
	return &p.coord, &p.pendingCoord
}

// PinnedKey returns the key currently pinned for kind, or "" when none is
// pinned yet. Safe for concurrent use.
func (l *Leaf) PinnedKey(kind KeyKind) string {
	l.pinMu.Lock()
	defer l.pinMu.Unlock()
	pin, _ := l.pins.slots(kind)
	return *pin
}

// PendingKey returns the most recent key offered for kind that differs from
// the pin, or "" when there is none. Safe for concurrent use.
func (l *Leaf) PendingKey(kind KeyKind) string {
	l.pinMu.Lock()
	defer l.pinMu.Unlock()
	_, pending := l.pins.slots(kind)
	return *pending
}

// checkNAL accepts a signature-verified NAL only if its coordinator node ID
// matches its key and that key is the pinned one, pinning it first if no key
// is pinned yet. A mismatch returns an error wrapping nal.ErrCoordKeyMismatch
// or nal.ErrCoordNodeIDMismatch.
func (l *Leaf) checkNAL(n *protocol.NAL) error {
	if err := nal.VerifyCoordNodeID(n); err != nil {
		return err
	}
	pinned, ok := l.checkPin(KeyCoordinator, n.CoordPubKeyB64)
	if !ok {
		return fmt.Errorf("%w: pinned %s, NAL signed by %s", nal.ErrCoordKeyMismatch, pinned, n.CoordPubKeyB64)
	}
	return nil
}

// checkPin compares offered with the pinned key for kind. With no pin, it
// pins offered (trust on first use), tells OnKeyPinned and returns true; it
// also tells OnKeyPinned when offered matches the pin and was the pending
// key, which means the sysop accepted it and the pending record can go.
// With a different pin, it records offered as pending, reports it through
// OnKeyChanged the first time it is offered, and returns false with the
// pinned key. The callbacks run without pinMu held.
func (l *Leaf) checkPin(kind KeyKind, offered string) (pinned string, ok bool) {
	l.pinMu.Lock()
	pin, pending := l.pins.slots(kind)
	if *pin == "" || *pin == offered {
		first := *pin == ""
		// A pending key that is now the pin was accepted by the sysop.
		accepted := *pending != "" && *pending == offered
		*pin = offered
		if accepted {
			*pending = ""
		}
		l.pinMu.Unlock()
		switch {
		case first:
			slog.Info("leaf: pinned key on first use",
				"network", l.cfg.Network, "hub", l.cfg.HubURL, "key", string(kind), "pubkey", offered)
		case accepted:
			slog.Info("leaf: sysop-accepted key now in use",
				"network", l.cfg.Network, "hub", l.cfg.HubURL, "key", string(kind), "pubkey", offered)
		default:
			return offered, true
		}
		if l.cfg.OnKeyPinned != nil {
			l.cfg.OnKeyPinned(KeyEvent{Network: l.cfg.Network, HubURL: l.cfg.HubURL, Kind: kind, Pinned: offered, Offered: offered})
		}
		return offered, true
	}
	pinned = *pin
	reported := *pending == offered
	*pending = offered
	l.pinMu.Unlock()

	if reported {
		slog.Warn("leaf: hub still offers a changed key; rejected",
			"network", l.cfg.Network, "hub", l.cfg.HubURL, "key", string(kind),
			"pinned", pinned, "offered", offered)
		return pinned, false
	}
	slog.Error("leaf: key changed; rejected until the sysop accepts it",
		"network", l.cfg.Network, "hub", l.cfg.HubURL, "key", string(kind),
		"pinned", pinned, "offered", offered,
		"accept", "verify the new key with the network operator, then replace or blank the pin in v3net.json")
	if l.cfg.OnKeyChanged != nil {
		l.cfg.OnKeyChanged(KeyEvent{Network: l.cfg.Network, HubURL: l.cfg.HubURL, Kind: kind, Pinned: pinned, Offered: offered})
	}
	return pinned, false
}

// checkHubKey fetches the network's /info and checks its hub key against the
// pin, pinning it on first use. A hub that does not publish its key (older
// hubs) is skipped. A mismatch returns an error wrapping ErrHubKeyMismatch;
// nothing else in the leaf depends on the hub key yet, so callers log it and
// carry on.
func (l *Leaf) checkHubKey(ctx context.Context) error {
	path := fmt.Sprintf("/v3net/v1/%s/info", l.cfg.Network)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, l.cfg.HubURL+path, nil)
	if err != nil {
		return fmt.Errorf("leaf: create GET request: %w", err)
	}
	resp, err := l.client.Do(req)
	if err != nil {
		return fmt.Errorf("leaf: GET %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }() // read side
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("leaf: GET %s: status %d", path, resp.StatusCode)
	}
	body, err := readBody(resp.Body, maxRespBytes)
	if err != nil {
		return fmt.Errorf("leaf: GET %s: %w", path, err)
	}
	var info protocol.NetworkInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return fmt.Errorf("leaf: decode network info: %w", err)
	}
	if info.HubPubKeyB64 == "" {
		slog.Debug("leaf: hub does not publish its key; not pinned", "network", l.cfg.Network, "hub", l.cfg.HubURL)
		return nil
	}
	raw, err := base64.StdEncoding.DecodeString(info.HubPubKeyB64)
	if err != nil || len(raw) != ed25519.PublicKeySize {
		return fmt.Errorf("leaf: network info has an invalid hub key")
	}
	if want := keystore.NodeIDFromPubKey(raw); info.HubNodeID != want {
		return fmt.Errorf("%w: info names %q, key derives %q", ErrHubNodeIDMismatch, info.HubNodeID, want)
	}
	if pinned, ok := l.checkPin(KeyHub, info.HubPubKeyB64); !ok {
		return fmt.Errorf("%w: pinned %s, hub offered %s", ErrHubKeyMismatch, pinned, info.HubPubKeyB64)
	}
	return nil
}

// normalizeKey trims the whitespace a sysop may paste around a key.
func normalizeKey(k string) string {
	return strings.TrimSpace(k)
}
