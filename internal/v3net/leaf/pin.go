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
// blanking the pin in v3net.json. A changed hub key also pauses syncing with
// the hub until then (see hubsig.go).
//
// The one exception is a signed coordinator handover (see
// protocol.CoordHandover). The hub serves the handovers it holds with the
// NAL; when they lead, each signed by the key before, from the pinned key to
// the key now offered, the leaf moves the pin itself, logs the move and
// hands the new pin to OnKeyPinned with the key it replaced. The hub key
// follows the same chain when the hub signs the NAL with its own key, which
// is how every hub runs today. A stolen old key can hand the network to
// someone else this way, but it could already sign NALs.

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
	// Previous is set for OnKeyPinned when a signed coordinator handover
	// moved the pin: it is the pin the move replaced, which the caller may
	// overwrite with Pinned. Empty otherwise.
	Previous string
}

// Pins is the set of keys a leaf holds for its network, as kept in its
// v3net.json entry. Empty strings mean "none".
type Pins struct {
	Coord, Hub               string
	PendingCoord, PendingHub string
}

// keyPins is the leaf's pinned and pending keys, guarded by Leaf.pinMu.
type keyPins struct {
	coord, hub               string
	pendingCoord, pendingHub string
	// coordUnsaved and hubUnsaved mark an in-memory pin, or the clearing of
	// an accepted pending key, that OnKeyPinned has not yet stored. It is
	// offered to OnKeyPinned again on every matching fetch until it is.
	coordUnsaved, hubUnsaved bool
	// coordMovedFrom and hubMovedFrom are the pin a handover replaced,
	// until OnKeyPinned stores the move. Several moves before a store keep
	// the first pin, the one the caller still holds.
	coordMovedFrom, hubMovedFrom string
}

// slots returns pointers to the pin, the pending key and the unsaved flag
// for kind.
func (p *keyPins) slots(kind KeyKind) (pin, pending *string, unsaved *bool) {
	if kind == KeyHub {
		return &p.hub, &p.pendingHub, &p.hubUnsaved
	}
	return &p.coord, &p.pendingCoord, &p.coordUnsaved
}

// movedFrom returns a pointer to the pin a handover replaced for kind.
func (p *keyPins) movedFrom(kind KeyKind) *string {
	if kind == KeyHub {
		return &p.hubMovedFrom
	}
	return &p.coordMovedFrom
}

// UpdatePins installs pins edited in v3net.json on a running leaf, without
// rebuilding it, so its last accepted NAL stays in effect. A pin whose value
// changed replaces the leaf's pin and its pending key (the sysop acted on
// it); an unchanged pin keeps the leaf's own pending key, which may be newer
// than the file. When the coordinator pin changed and ctx is non-nil, the
// NAL is re-fetched in the background so an accepted key takes effect at
// once; likewise /info is re-checked when the hub pin changed. Pass a nil
// ctx for a leaf that is not running. Safe for concurrent use.
func (l *Leaf) UpdatePins(ctx context.Context, p Pins) {
	l.pinMu.Lock()
	coordChanged := normalizeKey(p.Coord) != l.pins.coord
	hubChanged := normalizeKey(p.Hub) != l.pins.hub
	if coordChanged {
		l.pins.coord = normalizeKey(p.Coord)
		l.pins.pendingCoord = normalizeKey(p.PendingCoord)
		l.pins.coordUnsaved = false
		l.pins.coordMovedFrom = ""
	}
	if hubChanged {
		l.pins.hub = normalizeKey(p.Hub)
		l.pins.pendingHub = normalizeKey(p.PendingHub)
		l.pins.hubUnsaved = false
		l.pins.hubMovedFrom = ""
	}
	l.pinMu.Unlock()

	if ctx == nil || (!coordChanged && !hubChanged) {
		return
	}
	slog.Info("leaf: pinned keys changed in config",
		"network", l.cfg.Network, "hub", l.cfg.HubURL, "coordinator", coordChanged, "hub_key", hubChanged)
	go func() {
		if hubChanged {
			if err := l.checkHubKey(ctx); err != nil {
				slog.Warn("leaf: hub key check failed", "network", l.cfg.Network, "hub", l.cfg.HubURL, "error", err)
			}
		}
		if coordChanged {
			if err := l.refreshNAL(ctx); err != nil {
				slog.Warn("leaf: NAL re-fetch after pin change failed", "network", l.cfg.Network, "error", err)
			}
		}
	}()
}

// LastNAL returns the last NAL this leaf accepted, however old, or nil.
// The service uses it to carry a trusted NAL across a leaf rebuild.
func (l *Leaf) LastNAL() *protocol.NAL {
	if l.nalCache == nil {
		return nil
	}
	return l.nalCache.Last(l.cfg.Network)
}

// SeedNAL installs n as this leaf's last accepted NAL, provided it is for
// this network and signed by the pinned coordinator key; it reports whether
// it did. Call it before Start, with a NAL an earlier leaf for the same
// subscription accepted, so a rebuild does not lose the trusted area list
// while the hub serves a key the sysop has not accepted.
func (l *Leaf) SeedNAL(n *protocol.NAL) bool {
	if n == nil || l.nalCache == nil || n.Network != l.cfg.Network {
		return false
	}
	if pin := l.PinnedKey(KeyCoordinator); pin == "" || pin != n.CoordPubKeyB64 {
		return false
	}
	l.nalCache.Put(l.cfg.Network, n)
	return true
}

// PinnedKey returns the key currently pinned for kind, or "" when none is
// pinned yet. Safe for concurrent use.
func (l *Leaf) PinnedKey(kind KeyKind) string {
	l.pinMu.Lock()
	defer l.pinMu.Unlock()
	pin, _, _ := l.pins.slots(kind)
	return *pin
}

// PendingKey returns the most recent key offered for kind that differs from
// the pin, or "" when there is none. Safe for concurrent use.
func (l *Leaf) PendingKey(kind KeyKind) string {
	l.pinMu.Lock()
	defer l.pinMu.Unlock()
	_, pending, _ := l.pins.slots(kind)
	return *pending
}

// checkNAL accepts a signature-verified NAL only if its coordinator node ID
// matches its key and that key is the pinned one, pinning it first if no key
// is pinned yet, or moving the pin to it along the NAL's handovers. A
// mismatch returns an error wrapping nal.ErrCoordKeyMismatch or
// nal.ErrCoordNodeIDMismatch.
func (l *Leaf) checkNAL(n *protocol.NAL) error {
	if err := nal.VerifyCoordNodeID(n); err != nil {
		return err
	}
	l.followHandovers(KeyCoordinator, n.CoordPubKeyB64, n.Handovers)
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
// If OnKeyPinned fails, the pin still holds in memory and the store is
// retried on each later matching fetch until it succeeds.
// With a different pin, it records offered as pending, reports it through
// OnKeyChanged the first time it is offered, and returns false with the
// pinned key. The callbacks run without pinMu held.
func (l *Leaf) checkPin(kind KeyKind, offered string) (pinned string, ok bool) {
	l.pinMu.Lock()
	pin, pending, unsaved := l.pins.slots(kind)
	if *pin == "" || *pin == offered {
		first := *pin == ""
		// A pending key that is now the pin was accepted by the sysop.
		accepted := *pending != "" && *pending == offered
		*pin = offered
		if accepted {
			*pending = ""
		}
		if first || accepted {
			*unsaved = true
		}
		retry := *unsaved
		l.pinMu.Unlock()
		switch {
		case first:
			slog.Info("leaf: pinned key on first use",
				"network", l.cfg.Network, "hub", l.cfg.HubURL, "key", string(kind), "pubkey", offered)
		case accepted:
			slog.Info("leaf: sysop-accepted key now in use",
				"network", l.cfg.Network, "hub", l.cfg.HubURL, "key", string(kind), "pubkey", offered)
		}
		if retry {
			l.savePin(kind, offered)
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

// savePin hands an unsaved pin to OnKeyPinned and, if it was stored (or
// there is nowhere to store it), clears the unsaved flag, provided the pin
// is still the same key. A failure leaves the flag set for the next fetch.
func (l *Leaf) savePin(kind KeyKind, key string) {
	if l.cfg.OnKeyPinned != nil {
		l.pinMu.Lock()
		previous := *l.pins.movedFrom(kind)
		l.pinMu.Unlock()
		ev := KeyEvent{Network: l.cfg.Network, HubURL: l.cfg.HubURL, Kind: kind, Pinned: key, Offered: key, Previous: previous}
		if err := l.cfg.OnKeyPinned(ev); err != nil {
			slog.Warn("leaf: could not store pinned key; will retry at the next fetch",
				"network", l.cfg.Network, "hub", l.cfg.HubURL, "key", string(kind), "error", err)
			return
		}
	}
	l.pinMu.Lock()
	defer l.pinMu.Unlock()
	pin, _, unsaved := l.pins.slots(kind)
	switch moved := l.pins.movedFrom(kind); {
	case *pin == key:
		*unsaved = false
		*moved = ""
	case *moved != "":
		// A later handover moved the pin on meanwhile; the caller now
		// holds key, so that is what the next store replaces.
		*moved = key
	}
}

// followHandovers moves the pin for kind to offered when the handovers lead
// from the pin to it (see nal.FollowHandovers), and reports whether it did.
// With no pin, or offered already pinned, there is nothing to follow; with
// no chain the pin stays and checkPin rejects offered as before. The moved
// pin is marked unsaved, so the next checkPin hands it to OnKeyPinned.
func (l *Leaf) followHandovers(kind KeyKind, offered string, hs []protocol.CoordHandover) bool {
	from := l.PinnedKey(kind)
	if from == "" || from == offered || len(hs) == 0 {
		return false
	}
	chain, err := nal.FollowHandovers(l.cfg.Network, from, offered, hs)
	if err != nil {
		slog.Debug("leaf: handovers do not lead to the offered key",
			"network", l.cfg.Network, "hub", l.cfg.HubURL, "key", string(kind), "error", err)
		return false
	}

	l.pinMu.Lock()
	pin, pending, unsaved := l.pins.slots(kind)
	if *pin != from {
		// The sysop changed the pin while the chain was checked.
		l.pinMu.Unlock()
		return false
	}
	*pin = offered
	if *pending == offered {
		*pending = ""
	}
	if moved := l.pins.movedFrom(kind); *moved == "" {
		*moved = from
	}
	*unsaved = true
	l.pinMu.Unlock()

	slog.Warn("leaf: key moved by a signed coordinator handover",
		"network", l.cfg.Network, "hub", l.cfg.HubURL, "key", string(kind),
		"from", from, "to", offered, "steps", len(chain))
	return true
}

// followHubHandover moves the hub pin to offered, the key /info now serves,
// when the hub signs its NAL with that key and the NAL's handovers lead to
// it from the pinned hub key. That is a hub acting as its own coordinator
// after a handover: one key change, which the coordinator pin follows when
// the NAL is next checked. A NAL that cannot be fetched or verified leaves
// the pin as it is.
func (l *Leaf) followHubHandover(ctx context.Context, offered string) {
	if pin := l.PinnedKey(KeyHub); pin == "" || pin == offered {
		return
	}
	n, err := nal.Fetch(ctx, l.cfg.HubURL+fmt.Sprintf("/v3net/v1/%s/nal", l.cfg.Network), l.client)
	if err != nil {
		slog.Debug("leaf: no NAL to check the hub key's handovers against", "network", l.cfg.Network, "error", err)
		return
	}
	if n.Network != l.cfg.Network || n.CoordPubKeyB64 != offered || nal.Verify(n) != nil {
		return
	}
	l.followHandovers(KeyHub, offered, n.Handovers)
}

// checkHubKey fetches the network's /info and checks its hub key against the
// pin, pinning it on first use or moving it along a signed handover. A hub that does not publish its key (older
// hubs) is skipped. A mismatch pauses syncing with the hub and returns an
// error wrapping ErrHubKeyMismatch; a key that matches lifts any pause.
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
	l.followHubHandover(ctx, info.HubPubKeyB64)
	if pinned, ok := l.checkPin(KeyHub, info.HubPubKeyB64); !ok {
		l.pauseHub("hub key does not match the pinned key")
		return fmt.Errorf("%w: pinned %s, hub offered %s", ErrHubKeyMismatch, pinned, info.HubPubKeyB64)
	}
	l.resumeHub()
	return nil
}

// normalizeKey trims the whitespace a sysop may paste around a key.
func normalizeKey(k string) string {
	return strings.TrimSpace(k)
}
