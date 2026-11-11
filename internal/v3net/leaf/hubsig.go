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
// them against its pinned hub key, which Start pins from /info before the
// first signed request. A signature that fails, or an /info key that
// differs from the pin, pauses everything the leaf exchanges with the hub
// for the network (polling, the SSE stream and every signed request) until
// the hub's key matches the pin again: because the sysop accepted the
// offered key, or the hub went back to the pinned one. The offered key is
// reported through OnKeyChanged by checkHubKey, as before.
//
// Every hub since v0.9.6 signs, so a reply with no signature is from a hub
// too old for this leaf, or something between the two is stripping headers.
// Either way it is refused, and logged once with what to do. Syncing is not
// paused for it: there is no key for the sysop to accept, and the next poll
// or reconnect tries again.

// ErrHubPaused is returned by signed requests while syncing with the hub is
// paused over its key. Check for it with errors.Is.
var ErrHubPaused = errors.New("leaf: sync with the hub is paused until its key matches the pin")

// ErrHubSignature means a hub response or SSE event carried a signature
// that does not verify against the pinned hub key, or an event arrived out
// of sequence. Check for it with errors.Is.
var ErrHubSignature = errors.New("leaf: hub signature does not verify against the pinned hub key")

// ErrHubUnsigned means a hub response or SSE stream carried no signature.
// Check for it with errors.Is.
var ErrHubUnsigned = errors.New("leaf: hub reply is unsigned; hub replies must be signed")

// hubRecheckInterval is how often a paused leaf re-checks /info, so a hub
// that goes back to the pinned key, or a check that failed for a network
// error, does not leave it paused for good. Variable so tests can shorten it.
var hubRecheckInterval = 5 * time.Minute

// hubGate is whether syncing with the hub is paused, guarded by Leaf.hubMu.
type hubGate struct {
	paused bool
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
// logging reason the first time. The pause holds until the hub's key
// matches the pin.
func (l *Leaf) pauseHub(reason string) {
	l.hubMu.Lock()
	if l.hub.paused {
		l.hubMu.Unlock()
		return
	}
	l.hub.paused = true
	close(l.hub.pausedCh)
	l.hub.resumedCh = make(chan struct{})
	l.hubMu.Unlock()
	slog.Error("leaf: pausing sync with the hub", "network", l.cfg.Network, "hub", l.cfg.HubURL, "reason", reason,
		"resume", "verify the hub's new key with the network operator, then accept it as the hub key in v3net.json")
}

// resumeHub lifts a pause, once the hub's key matches the pin again.
func (l *Leaf) resumeHub() {
	l.hubMu.Lock()
	if !l.hub.paused {
		l.hubMu.Unlock()
		return
	}
	l.hub.paused = false
	close(l.hub.resumedCh)
	l.hub.pausedCh = make(chan struct{})
	l.hubMu.Unlock()
	slog.Info("leaf: hub key matches the pin; resuming sync", "network", l.cfg.Network, "hub", l.cfg.HubURL)
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

// hubSignatureFailed pauses syncing after a bad hub signature, or one with
// no pinned key to check it against (reason says which, and on what), then
// checks /info. A changed key there is reported to the sysop as pending and
// keeps the pause. If /info serves the pinned key, or pins one on first
// use, only the bad response is refused and the pause lifts; if /info
// cannot be fetched, or serves no key, the pause holds until a later check
// succeeds.
func (l *Leaf) hubSignatureFailed(ctx context.Context, reason string) {
	l.pauseHub(reason)
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

// hubUnsigned refuses a reply to a signed request, or an SSE stream, that
// carries no signature. The first refusal is logged with how to recover;
// later ones go to debug until a signature verifies again.
func (l *Leaf) hubUnsigned(path string) error {
	if l.unsignedRefusedLogged.CompareAndSwap(false, true) {
		slog.Error("leaf: refusing an unsigned reply from the hub",
			"network", l.cfg.Network, "hub", l.cfg.HubURL, "path", path,
			"recover", "hubs sign their replies since v0.9.6: ask the hub's operator to upgrade; "+
				"if the hub is current, something between here and the hub is removing its signatures")
	} else {
		slog.Debug("leaf: refusing an unsigned reply from the hub", "network", l.cfg.Network, "path", path)
	}
	return fmt.Errorf("%w: response to %s", ErrHubUnsigned, path)
}

// reqBinding is what a hub's signatures on a reply are bound to: the
// request's X-V3Net-Signature and the nonce sent with it.
type reqBinding struct {
	sig, nonce string
}

// verifyHubResponse checks a response's X-V3Net-Hub-Signature against the
// pinned hub key, for the signed request req and the body as read. An
// unsigned response is refused with an error wrapping ErrHubUnsigned. A bad
// signature, or one with no pinned key to check it against, pauses syncing
// and returns an error wrapping ErrHubSignature.
func (l *Leaf) verifyHubResponse(ctx context.Context, resp *http.Response, path string, req reqBinding, body []byte) error {
	sig := resp.Header.Get(protocol.HeaderHubSignature)
	if sig == "" {
		return l.hubUnsigned(path)
	}
	pub, pinned := l.pinnedHubPubKey()
	if !pinned {
		// Start pins the key before the first signed request, so /info
		// could not be read then; the check pins it now and lifts the pause.
		l.hubSignatureFailed(ctx, "no hub key is pinned to check the signature on "+path)
		return fmt.Errorf("%w: response to %s: no hub key is pinned", ErrHubSignature, path)
	}
	if protocol.VerifyResponse(pub, req.sig, req.nonce, resp.StatusCode, body, sig) != nil {
		l.hubSignatureFailed(ctx, "bad hub signature on "+path)
		return fmt.Errorf("%w: response to %s", ErrHubSignature, path)
	}
	l.unsignedRefusedLogged.Store(false)
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
// returns a verifier for its events. An unsigned stream, or one from a hub
// whose key is not pinned, is refused as verifyHubResponse refuses a reply.
func (l *Leaf) newEventVerifier(ctx context.Context, resp *http.Response, path string, req reqBinding) (*eventVerifier, error) {
	if err := l.verifyHubResponse(ctx, resp, path, req, nil); err != nil {
		return nil, err
	}
	pub, _ := l.pinnedHubPubKey()
	return &eventVerifier{pub: pub, network: l.cfg.Network, req: req}, nil
}

// check accepts an event whose id is the next sequence number and whose sig
// verifies.
func (v *eventVerifier) check(id, sig, eventType string, data []byte) error {
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
