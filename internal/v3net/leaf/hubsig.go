package leaf

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// A hub signs its responses to signed requests, and each SSE event, with
// the key /info serves (see protocol.ResponseSignedPayload). The leaf checks
// them against its pinned hub key. A signature that fails, or an /info key
// that differs from the pin, pauses everything the leaf exchanges with the
// hub for the network (polling, the SSE stream and every signed request)
// until the hub's key matches the pin again: because the sysop accepted the
// offered key, or the hub went back to the pinned one. The offered key is
// reported through OnKeyChanged by checkHubKey, as before.
//
// A hub that sends no signature predates signing; its responses are taken
// unverified, as they always were, and the leaf says so once in the log.
// Stripping the signature header looks the same, so an unsigned reply is
// refused, and pauses syncing as a bad signature does, when
// Config.RequireSignedHub is set or once the hub has been seen signing
// (Config.HubSigned). The leaf hands the latter to OnHubSigned the first
// time a hub signature verifies, so a restart does not forget it.

// ErrHubPaused is returned by signed requests while syncing with the hub is
// paused over its key. Check for it with errors.Is.
var ErrHubPaused = errors.New("leaf: sync with the hub is paused until its key matches the pin")

// ErrHubSignature means a hub response or SSE event carried a signature
// that does not verify against the pinned hub key, or an event arrived out
// of sequence. Check for it with errors.Is.
var ErrHubSignature = errors.New("leaf: hub signature does not verify against the pinned hub key")

// ErrHubUnsigned means a hub response or SSE stream carried no signature
// the leaf could check, although this hub's replies must be signed
// (Config.RequireSignedHub, or the hub was seen signing). Check for it with
// errors.Is.
var ErrHubUnsigned = errors.New("leaf: hub reply is unsigned, but this hub's replies must be signed")

// hubRecheckInterval is how often a paused leaf re-checks /info, so a hub
// that goes back to the pinned key, or a check that failed for a network
// error, does not leave it paused for good. Variable so tests can shorten it.
var hubRecheckInterval = 5 * time.Minute

// hubGate is whether syncing with the hub is paused, guarded by Leaf.hubMu.
type hubGate struct {
	paused bool
	// unsignedOnly is set while every cause of the current pause was an
	// unsigned reply refused under hubMustSign, so the pause can lift once
	// signatures are no longer required, even for a hub whose /info serves
	// no key to resume on. A key mismatch or bad signature clears it.
	unsignedOnly bool
	// pausedCh is closed when a pause begins, so an open SSE stream drops;
	// resumedCh is closed when it ends. Each is replaced once closed.
	pausedCh, resumedCh chan struct{}
}

func newHubGate() hubGate {
	resumed := make(chan struct{})
	close(resumed)
	return hubGate{pausedCh: make(chan struct{}), resumedCh: resumed}
}

// HubPaused reports whether syncing with the hub is paused because its key
// or signatures do not match the pinned hub key. Safe for concurrent use.
func (l *Leaf) HubPaused() bool {
	l.hubMu.Lock()
	defer l.hubMu.Unlock()
	return l.hub.paused
}

// hubChans returns the channels that announce the next pause and the end of
// the current one.
func (l *Leaf) hubChans() (paused, resumed <-chan struct{}) {
	l.hubMu.Lock()
	defer l.hubMu.Unlock()
	return l.hub.pausedCh, l.hub.resumedCh
}

// pauseHub stops syncing with the hub over its key or a bad signature,
// logging reason the first time.
func (l *Leaf) pauseHub(reason string) {
	l.pause(reason, false)
}

// pause stops syncing with the hub, logging reason the first time. unsigned
// says the cause is an unsigned reply refused under hubMustSign; any other
// cause, now or added to a pause already in force, keeps the pause until the
// hub's key matches the pin.
func (l *Leaf) pause(reason string, unsigned bool) {
	l.hubMu.Lock()
	if l.hub.paused {
		l.hub.unsignedOnly = l.hub.unsignedOnly && unsigned
		l.hubMu.Unlock()
		return
	}
	l.hub.paused = true
	l.hub.unsignedOnly = unsigned
	close(l.hub.pausedCh)
	l.hub.resumedCh = make(chan struct{})
	l.hubMu.Unlock()
	slog.Error("leaf: pausing sync with the hub", "network", l.cfg.Network, "hub", l.cfg.HubURL, "reason", reason,
		"resume", "verify the hub's new key with the network operator, then accept it as the hub key in v3net.json")
}

// resumeHub lifts a pause, once the hub's key matches the pin again.
func (l *Leaf) resumeHub() {
	if l.resume(false) {
		slog.Info("leaf: hub key matches the pin; resuming sync", "network", l.cfg.Network, "hub", l.cfg.HubURL)
	}
}

// liftUnsignedPause lifts a pause caused only by unsigned replies, once
// hubMustSign no longer holds: the sysop cleared hubSigned with
// requireSignedHub off, after the hub really stopped signing.
func (l *Leaf) liftUnsignedPause() {
	if must, _ := l.hubMustSign(); must {
		return
	}
	if l.resume(true) {
		slog.Info("leaf: hub signatures no longer required; resuming sync", "network", l.cfg.Network, "hub", l.cfg.HubURL)
	}
}

// resume lifts the pause, or with unsignedOnly only a pause whose every
// cause was an unsigned reply. It reports whether it lifted one.
func (l *Leaf) resume(unsignedOnly bool) bool {
	l.hubMu.Lock()
	defer l.hubMu.Unlock()
	if !l.hub.paused || (unsignedOnly && !l.hub.unsignedOnly) {
		return false
	}
	l.hub.paused = false
	l.hub.unsignedOnly = false
	close(l.hub.resumedCh)
	l.hub.pausedCh = make(chan struct{})
	return true
}

// waitHubTrusted returns at once unless syncing is paused; then it waits for
// the pause to end, re-checking /info every hubRecheckInterval. It returns
// ctx's error if ctx ends first.
func (l *Leaf) waitHubTrusted(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		_, resumed := l.hubChans()
		if !l.HubPaused() {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-resumed:
		case <-time.After(hubRecheckInterval):
			if err := l.checkHubKey(ctx); err != nil {
				slog.Debug("leaf: hub key re-check while paused", "network", l.cfg.Network, "error", err)
			}
		}
	}
}

// hubSignatureFailed pauses syncing after a bad or missing hub signature
// (reason says which, and on what), then checks /info. A changed key there
// is reported to the sysop as pending and keeps the pause. If /info still
// serves the pinned key, only the bad response is refused and the pause
// lifts; if /info cannot be fetched, or serves no key, the pause holds until
// a later check succeeds, or, with unsigned (the signature was missing),
// until signatures are no longer required (liftUnsignedPause).
func (l *Leaf) hubSignatureFailed(ctx context.Context, reason string, unsigned bool) {
	l.pause(reason, unsigned)
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	if err := l.checkHubKey(ctx); err != nil && !errors.Is(err, ErrHubKeyMismatch) {
		slog.Warn("leaf: hub key check after a bad signature failed; staying paused",
			"network", l.cfg.Network, "hub", l.cfg.HubURL, "error", err)
	}
}

// pinnedHubPubKey returns the pinned hub key, or false when none is pinned
// (or the pin is not a valid key, which checkHubKey reports as a mismatch).
func (l *Leaf) pinnedHubPubKey() (ed25519.PublicKey, bool) {
	raw, err := base64.StdEncoding.DecodeString(l.PinnedKey(KeyHub))
	if err != nil || len(raw) != ed25519.PublicKeySize {
		return nil, false
	}
	return ed25519.PublicKey(raw), true
}

// noteUnsignedHub logs, once per leaf, that the hub sent an unsigned
// response to a signed request: it predates hub signing.
func (l *Leaf) noteUnsignedHub() {
	if l.unsignedHubLogged.CompareAndSwap(false, true) {
		slog.Info("leaf: hub does not sign its responses (older hub); accepting them unverified",
			"network", l.cfg.Network, "hub", l.cfg.HubURL)
	}
}

// hubMustSign reports whether this hub's replies must carry a signature
// that verifies: because Config.RequireSignedHub is set, or because the hub
// has been seen signing. why names which, for the log.
func (l *Leaf) hubMustSign() (must bool, why string) {
	if l.cfg.RequireSignedHub {
		return true, "requireSignedHub is set"
	}
	if l.HubSigned() {
		return true, "the hub has signed before (hubSigned)"
	}
	return false, ""
}

// HubSigned reports whether the leaf has seen the hub sign, and so refuses
// unsigned replies. Safe for concurrent use.
func (l *Leaf) HubSigned() bool {
	l.pinMu.Lock()
	defer l.pinMu.Unlock()
	return l.pins.hubSigned
}

// hubUnsigned refuses a reply to a signed request that has no signature
// the leaf can check, while hubMustSign holds, and pauses syncing as a bad
// signature does. The first refusal is logged with how to recover; later
// ones go to debug until a signature verifies again.
func (l *Leaf) hubUnsigned(ctx context.Context, path, why string) error {
	if l.unsignedRefusedLogged.CompareAndSwap(false, true) {
		slog.Error("leaf: refusing an unsigned reply from the hub",
			"network", l.cfg.Network, "hub", l.cfg.HubURL, "path", path, "because", why,
			"recover", "if the hub really stopped signing (it went back to an older version), "+
				"turn off requireSignedHub and clear hubSigned for this network in v3net.json; "+
				"otherwise something between here and the hub is removing its signatures")
	} else {
		slog.Debug("leaf: refusing an unsigned reply from the hub", "network", l.cfg.Network, "path", path)
	}
	l.hubSignatureFailed(ctx, "missing hub signature on "+path, true)
	return fmt.Errorf("%w: response to %s (%s)", ErrHubUnsigned, path, why)
}

// noteHubSigned records that a hub signature verified. The first time, it
// sets hubSigned and hands it to OnHubSigned to store; a store that failed
// is retried at each later verified reply until it succeeds, or until a
// reload in which the sysop wrote hubSigned drops the record (see
// applyHubSigned). On a retired leaf the first time goes to its replacement
// instead (see HandLateRepliesTo).
func (l *Leaf) noteHubSigned() {
	l.unsignedRefusedLogged.Store(false)
	if !l.markHubSigned() {
		return
	}
	// Held across the store and the record of it, so applyHubSigned never
	// reads the store between the two (see there).
	l.hubSignedSaveMu.Lock()
	defer l.hubSignedSaveMu.Unlock()
	l.storeHubSigned()
}

// lateHubSigned records a reply a predecessor verified, as noteHubSigned does a
// reply of this leaf's own, unless the sysop's clear has reached this leaf. It
// holds the save lock throughout, which applyHubSigned and Retire hold while
// they act on a clear, so a clear is either seen here or finds the record to
// clear.
func (l *Leaf) lateHubSigned() {
	l.hubSignedSaveMu.Lock()
	defer l.hubSignedSaveMu.Unlock()
	l.pinMu.Lock()
	cleared := l.pins.hubSignedCleared
	l.pinMu.Unlock()
	if !cleared && l.markHubSigned() {
		l.storeHubSigned()
	}
}

// markHubSigned sets the record, the first time handing it on from a retired
// leaf, and reports whether it waits to be stored.
func (l *Leaf) markHubSigned() (save bool) {
	l.pinMu.Lock()
	first := !l.pins.hubSigned
	if first {
		l.pins.hubSigned = true
		l.pins.hubSignedUnsaved = true
	}
	save = l.pins.hubSignedUnsaved
	late := first && l.pins.retired
	if late {
		l.pins.hubSignedLate = true
	}
	next := l.pins.hubSignedNext
	l.pinMu.Unlock()
	if first {
		slog.Info("leaf: hub signs its responses; unsigned replies from it are refused from now on",
			"network", l.cfg.Network, "hub", l.cfg.HubURL)
	}
	if late && next != nil {
		next.lateHubSigned()
	}
	return save
}

// storeHubSigned hands a record waiting to be stored to OnHubSigned. The caller
// holds the save lock.
func (l *Leaf) storeHubSigned() {
	l.pinMu.Lock()
	save := l.pins.hubSigned && l.pins.hubSignedUnsaved && !l.pins.retired
	l.pinMu.Unlock()
	if !save {
		// A save that ran ahead of this one stored it, or the leaf that
		// replaced this one now owns the store, or the subscription is gone
		// (see Retire, EndStores).
		return
	}
	if l.cfg.OnHubSigned != nil {
		if err := l.cfg.OnHubSigned(); err != nil {
			slog.Warn("leaf: could not record that the hub signs; will retry at the next reply",
				"network", l.cfg.Network, "hub", l.cfg.HubURL, "error", err)
			return
		}
	}
	l.pinMu.Lock()
	if l.pins.hubSigned {
		l.pins.hubSignedUnsaved = false
		l.pins.hubSignedCfg = true
	}
	l.pinMu.Unlock()
}

// reqBinding is what a hub's signatures on a reply are bound to: the
// request's X-V3Net-Signature and the nonce sent with it.
type reqBinding struct {
	sig, nonce string
}

// verifyHubResponse checks a response's X-V3Net-Hub-Signature against the
// pinned hub key, for the signed request req and the body as read. An
// unsigned response, or one from a hub whose key is not pinned, is accepted
// unless hubMustSign holds; then it is refused with an error wrapping
// ErrHubUnsigned. A bad signature pauses syncing and returns an error
// wrapping ErrHubSignature.
func (l *Leaf) verifyHubResponse(ctx context.Context, resp *http.Response, path string, req reqBinding, body []byte) error {
	sig := resp.Header.Get(protocol.HeaderHubSignature)
	pub, pinned := l.pinnedHubPubKey()
	if sig == "" || !pinned {
		if must, why := l.hubMustSign(); must {
			if sig != "" {
				why += "; no hub key is pinned to check its signature against"
			}
			return l.hubUnsigned(ctx, path, why)
		}
		if sig == "" {
			l.noteUnsignedHub()
		}
		return nil
	}
	if protocol.VerifyResponse(pub, req.sig, req.nonce, resp.StatusCode, body, sig) != nil {
		l.hubSignatureFailed(ctx, "bad hub signature on "+path, false)
		return fmt.Errorf("%w: response to %s", ErrHubSignature, path)
	}
	l.noteHubSigned()
	return nil
}

// eventVerifier checks the signed events of one SSE stream.
type eventVerifier struct {
	pub     ed25519.PublicKey
	network string
	req     reqBinding
	last    uint64 // sequence number of the last event accepted
}

// newEventVerifier checks the signature on an SSE response's headers and
// returns a verifier for its events, or nil for a stream from a hub that
// does not sign or whose key is not pinned. A bad signature pauses syncing
// and returns an error wrapping ErrHubSignature; a missing one does the
// same with ErrHubUnsigned while hubMustSign holds.
func (l *Leaf) newEventVerifier(ctx context.Context, resp *http.Response, path string, req reqBinding) (*eventVerifier, error) {
	if err := l.verifyHubResponse(ctx, resp, path, req, nil); err != nil {
		return nil, err
	}
	pub, ok := l.pinnedHubPubKey()
	if !ok || resp.Header.Get(protocol.HeaderHubSignature) == "" {
		return nil, nil
	}
	return &eventVerifier{pub: pub, network: l.cfg.Network, req: req}, nil
}

// check accepts an event whose id is the next sequence number and whose sig
// verifies. A nil verifier accepts everything.
func (v *eventVerifier) check(id, sig, eventType string, data []byte) error {
	if v == nil {
		return nil
	}
	seq, err := strconv.ParseUint(id, 10, 64)
	if err != nil || seq != v.last+1 {
		return fmt.Errorf("%w: %s event id %q out of sequence, want %d", ErrHubSignature, eventType, id, v.last+1)
	}
	if protocol.VerifyEvent(v.pub, v.network, v.req.sig, v.req.nonce, seq, eventType, data, sig) != nil {
		return fmt.Errorf("%w: %s event %d", ErrHubSignature, eventType, seq)
	}
	v.last = seq
	return nil
}
