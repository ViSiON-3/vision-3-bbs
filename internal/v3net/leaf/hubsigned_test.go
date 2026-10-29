package leaf

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/keystore"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// signedRecorder stands in for the store behind OnHubSigned. While fail is
// set it reports a storage failure.
type signedRecorder struct {
	calls  atomic.Int32
	fail   atomic.Bool
	stored atomic.Bool
}

func (r *signedRecorder) attach(l *Leaf) {
	l.cfg.OnHubSigned = func() error {
		r.calls.Add(1)
		if r.fail.Load() {
			return errors.New("disk full")
		}
		r.stored.Store(true)
		return nil
	}
}

// pausedBy reports whether fn paused syncing with the hub, even if the pause
// lifted again before fn returned.
func pausedBy(l *Leaf, fn func()) bool {
	paused, _ := l.hubChans()
	fn()
	select {
	case <-paused:
		return true
	default:
		return false
	}
}

// restartLeaf builds a new leaf from l's config, as the BBS does on a
// restart, with the hub pin l holds and hubSigned as stored.
func restartLeaf(t *testing.T, l *Leaf, hubSigned bool) *Leaf {
	t.Helper()
	cfg := l.cfg
	cfg.HubPubKeyB64 = l.PinnedKey(KeyHub)
	cfg.HubSigned = hubSigned
	nl := New(cfg)
	t.Cleanup(nl.Close)
	return nl
}

// With requireSignedHub off, a hub that does not sign (here, one whose
// signatures are stripped from the start) is accepted and not recorded as
// signing.
func TestRequireSignedHubOffAcceptsUnsigned(t *testing.T) {
	l, _, p, _ := proxiedLeaf(t, (*keystore.Keystore).PubKeyBase64)
	rec := &signedRecorder{}
	rec.attach(l)
	p.stripSig.Store(true)

	if _, err := l.Poll(context.Background()); err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if err := l.SendLogon("tester"); err != nil {
		t.Fatalf("SendLogon: %v", err)
	}
	if l.HubPaused() || l.HubSigned() || rec.calls.Load() != 0 {
		t.Errorf("paused=%v hubSigned=%v stores=%d; want an unsigned hub accepted as before",
			l.HubPaused(), l.HubSigned(), rec.calls.Load())
	}
}

// With requireSignedHub on, an unsigned reply or stream is refused and
// pauses syncing like a bad signature. /info still serves the pinned key,
// so the pause lifts and only the unsigned replies are lost.
func TestRequireSignedHubRefusesUnsigned(t *testing.T) {
	l, _, p, _ := proxiedLeaf(t, (*keystore.Keystore).PubKeyBase64)
	l.cfg.RequireSignedHub = true
	ctx := context.Background()
	p.stripSig.Store(true)

	var err error
	if !pausedBy(l, func() { _, err = l.Poll(ctx) }) {
		t.Error("an unsigned page did not pause syncing")
	}
	if !errors.Is(err, ErrHubUnsigned) {
		t.Fatalf("Poll of an unsigned page = %v, want ErrHubUnsigned", err)
	}
	if err := l.SendLogon("tester"); !errors.Is(err, ErrHubUnsigned) {
		t.Errorf("SendLogon with an unsigned reply = %v, want ErrHubUnsigned", err)
	}
	if err := l.connectSSE(ctx, false); !errors.Is(err, ErrHubUnsigned) {
		t.Errorf("connectSSE to an unsigned stream = %v, want ErrHubUnsigned", err)
	}
	if l.HubPaused() {
		t.Error("still paused although /info serves the pinned key")
	}

	p.stripSig.Store(false)
	if _, err := l.Poll(ctx); err != nil {
		t.Errorf("Poll once the hub's signatures arrive again: %v", err)
	}
}

// With requireSignedHub on, a hub that cannot sign at all (it publishes no
// key) keeps syncing paused.
func TestRequireSignedHubPausesOnOlderHub(t *testing.T) {
	ts, _ := newMockHub(t, []protocol.Message{testMessage("550e8400-e29b-41d4-a716-446655440000")})
	l, _ := setupLeaf(t, ts.URL, &mockJAMWriter{})
	l.cfg.RequireSignedHub = true
	ctx := context.Background()

	if _, err := l.Poll(ctx); !errors.Is(err, ErrHubUnsigned) {
		t.Fatalf("Poll = %v, want ErrHubUnsigned", err)
	}
	if !l.HubPaused() {
		t.Fatal("not paused by a hub that does not sign")
	}
	if _, err := l.Poll(ctx); !errors.Is(err, ErrHubPaused) {
		t.Errorf("second Poll = %v, want ErrHubPaused", err)
	}
}

// Once a hub signature verifies, the leaf records that the hub signs, once,
// and from then on refuses unsigned replies and streams from it with
// requireSignedHub off.
func TestHubSeenSigningRefusesDowngrade(t *testing.T) {
	l, _, p, _ := proxiedLeaf(t, (*keystore.Keystore).PubKeyBase64)
	rec := &signedRecorder{}
	rec.attach(l)
	ctx := context.Background()
	if l.HubSigned() {
		t.Fatal("hub recorded as signing before any signature was checked")
	}

	for range 2 {
		if _, err := l.Poll(ctx); err != nil {
			t.Fatalf("Poll: %v", err)
		}
	}
	if !l.HubSigned() || !rec.stored.Load() || rec.calls.Load() != 1 {
		t.Fatalf("hubSigned=%v stored=%v stores=%d; want it recorded and stored once",
			l.HubSigned(), rec.stored.Load(), rec.calls.Load())
	}

	p.stripSig.Store(true)
	if _, err := l.Poll(ctx); !errors.Is(err, ErrHubUnsigned) {
		t.Errorf("Poll with the signature stripped = %v, want ErrHubUnsigned", err)
	}
	if err := l.SendLogon("tester"); !errors.Is(err, ErrHubUnsigned) {
		t.Errorf("SendLogon with the signature stripped = %v, want ErrHubUnsigned", err)
	}
	if err := l.connectSSE(ctx, false); !errors.Is(err, ErrHubUnsigned) {
		t.Errorf("connectSSE with the signature stripped = %v, want ErrHubUnsigned", err)
	}
}

// The stored record is what keeps a restart from accepting unsigned replies
// again: a leaf started with it refuses them, one started without it (as
// before this record existed) does not.
func TestHubSignedSurvivesRestart(t *testing.T) {
	l, _, p, _ := proxiedLeaf(t, (*keystore.Keystore).PubKeyBase64)
	rec := &signedRecorder{}
	rec.attach(l)
	ctx := context.Background()
	if _, err := l.Poll(ctx); err != nil {
		t.Fatalf("Poll: %v", err)
	}
	p.stripSig.Store(true)

	restarted := restartLeaf(t, l, rec.stored.Load())
	if !restarted.HubSigned() {
		t.Fatal("restarted leaf does not know the hub signs")
	}
	if _, err := restarted.Poll(ctx); !errors.Is(err, ErrHubUnsigned) {
		t.Errorf("Poll after a restart with the signature stripped = %v, want ErrHubUnsigned", err)
	}

	forgetful := restartLeaf(t, l, false)
	if _, err := forgetful.Poll(ctx); err != nil {
		t.Errorf("Poll by a leaf with no record = %v; the record should be what refuses it", err)
	}
}

// A record that could not be stored is still enforced, offered to
// OnHubSigned again at the next verified reply, and not undone by a config
// reload that predates it.
func TestHubSignedStoreRetried(t *testing.T) {
	l, _, p, hubKS := proxiedLeaf(t, (*keystore.Keystore).PubKeyBase64)
	rec := &signedRecorder{}
	rec.attach(l)
	rec.fail.Store(true)
	ctx := context.Background()

	if _, err := l.Poll(ctx); err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if !l.HubSigned() || rec.stored.Load() {
		t.Fatalf("hubSigned=%v stored=%v; want enforced but not stored", l.HubSigned(), rec.stored.Load())
	}
	l.UpdatePins(ctx, Pins{Hub: hubKS.PubKeyBase64()})
	if !l.HubSigned() {
		t.Fatal("a reload of the config as it was cleared a record not yet stored")
	}
	p.stripSig.Store(true)
	if _, err := l.Poll(ctx); !errors.Is(err, ErrHubUnsigned) {
		t.Errorf("Poll with the signature stripped = %v, want ErrHubUnsigned", err)
	}

	p.stripSig.Store(false)
	rec.fail.Store(false)
	for range 2 {
		if _, err := l.Poll(ctx); err != nil {
			t.Fatalf("Poll: %v", err)
		}
	}
	if !rec.stored.Load() || rec.calls.Load() != 2 {
		t.Errorf("stored=%v after %d stores; want stored at the second try and not again", rec.stored.Load(), rec.calls.Load())
	}
}

// Recovery from a hub that really stopped signing (a rollback): the leaf
// refuses its replies until the sysop clears hubSigned, then accepts them
// unverified, and records the hub as signing again once it does.
func TestHubSignedClearedBySysop(t *testing.T) {
	first, _, p, hubKS := proxiedLeaf(t, (*keystore.Keystore).PubKeyBase64)
	l := restartLeaf(t, first, true)
	rec := &signedRecorder{}
	rec.attach(l)
	ctx := context.Background()

	p.stripSig.Store(true) // the hub went back to a version that does not sign
	if _, err := l.Poll(ctx); !errors.Is(err, ErrHubUnsigned) {
		t.Fatalf("Poll of the rolled-back hub = %v, want ErrHubUnsigned", err)
	}

	l.UpdatePins(ctx, Pins{Hub: hubKS.PubKeyBase64(), HubSigned: false})
	if l.HubSigned() {
		t.Fatal("clearing hubSigned in the config did not reach the leaf")
	}
	if _, err := l.Poll(ctx); err != nil {
		t.Fatalf("Poll after the sysop cleared hubSigned: %v", err)
	}
	if l.HubPaused() {
		t.Error("still paused after the sysop cleared hubSigned")
	}

	p.stripSig.Store(false) // the hub is upgraded again
	if _, err := l.Poll(ctx); err != nil {
		t.Fatalf("Poll after the hub signs again: %v", err)
	}
	if !l.HubSigned() || rec.calls.Load() != 1 {
		t.Errorf("hubSigned=%v stores=%d; want the hub recorded as signing again", l.HubSigned(), rec.calls.Load())
	}
}
