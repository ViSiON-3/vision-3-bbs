package leaf

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

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

// A record that could not be stored is still enforced, and offered to
// OnHubSigned again at the next verified reply.
func TestHubSignedStoreRetried(t *testing.T) {
	l, _, p, _ := proxiedLeaf(t, (*keystore.Keystore).PubKeyBase64)
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

// logonRecorder collects the handles of the logon events a leaf dispatches.
type logonRecorder struct {
	mu      sync.Mutex
	handles []string
}

func (r *logonRecorder) attach(l *Leaf) {
	l.SetOnEvent(func(ev protocol.Event) {
		var p protocol.LogonPayload
		if ev.Type != protocol.EventLogon || json.Unmarshal(ev.Data, &p) != nil {
			return
		}
		r.mu.Lock()
		r.handles = append(r.handles, p.Handle)
		r.mu.Unlock()
	})
}

func (r *logonRecorder) has(handle string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, h := range r.handles {
		if h == handle {
			return true
		}
	}
	return false
}

// An event stream whose signature was stripped, opened while hub
// signatures were not required, is dropped before its next event is
// dispatched once they are: because a verified reply showed the hub signs,
// or because the sysop turned on Hub Signs in place.
func TestUnsignedStreamDroppedOnceSignaturesRequired(t *testing.T) {
	for name, require := range map[string]func(t *testing.T, l *Leaf, hubKS *keystore.Keystore){
		"hub seen signing": func(t *testing.T, l *Leaf, _ *keystore.Keystore) {
			if _, err := l.Poll(context.Background()); err != nil {
				t.Fatalf("Poll: %v", err)
			}
			if !l.HubSigned() {
				t.Fatal("a verified poll did not record that the hub signs")
			}
		},
		"Hub Signs turned on": func(t *testing.T, l *Leaf, hubKS *keystore.Keystore) {
			l.UpdatePins(context.Background(), Pins{Hub: hubKS.PubKeyBase64(), HubSigned: true})
			if !l.HubSigned() {
				t.Fatal("Hub Signs set in the config did not reach the leaf")
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			l, _, p, hubKS := proxiedLeaf(t, (*keystore.Keystore).PubKeyBase64)
			p.stripEvents.Store(true)
			rec := &logonRecorder{}
			rec.attach(l)
			other := subscribedLeaf(t, p.ts, "Other BBS")

			done := make(chan error, 1)
			go func() { done <- l.connectSSE(context.Background(), false) }()
			// The unsigned stream is live, and accepted while nothing requires
			// signatures.
			waitFor(t, "the unsigned stream to deliver", func() bool {
				_ = other.SendLogon("probe")
				return rec.has("probe")
			})

			require(t, l, hubKS)
			if err := other.SendLogon("forged"); err != nil {
				t.Fatalf("SendLogon: %v", err)
			}
			select {
			case err := <-done:
				if !errors.Is(err, ErrHubUnsigned) {
					t.Errorf("connectSSE = %v, want ErrHubUnsigned", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("unsigned stream still open once signatures were required")
			}
			if rec.has("forged") {
				t.Error("an event from the unsigned stream was dispatched once signatures were required")
			}
		})
	}
}

// A hub rolled back to a version that neither signs nor publishes a key
// leaves the leaf paused, as nothing on /info can resume it. Clearing
// hubSigned (with requireSignedHub off) lifts that pause.
func TestRollbackRecoveryResumesWithoutHubKey(t *testing.T) {
	mh := &mockHub{ks: newCoordKey(t), messages: []protocol.Message{testMessage(testUUID1)}}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/info") {
			_ = json.NewEncoder(w).Encode(protocol.NetworkInfo{Name: "testnet"})
			return
		}
		mh.handler(w, r)
	}))
	t.Cleanup(ts.Close)
	l, _ := setupLeaf(t, ts.URL, &mockJAMWriter{})
	pin := newCoordKey(t).PubKeyBase64()
	l.pins.hub = pin
	l.pins.hubSigned, l.pins.hubSignedCfg = true, true
	ctx := context.Background()

	if _, err := l.Poll(ctx); !errors.Is(err, ErrHubUnsigned) {
		t.Fatalf("Poll of the rolled-back hub = %v, want ErrHubUnsigned", err)
	}
	if err := l.checkHubKey(ctx); err != nil || !l.HubPaused() {
		t.Fatalf("re-check of /info = %v, paused=%v; want it to find nothing to resume on", err, l.HubPaused())
	}

	l.UpdatePins(ctx, Pins{Hub: pin, HubSigned: false})
	if l.HubPaused() {
		t.Fatal("still paused after the sysop cleared hubSigned")
	}
	if n, err := l.Poll(ctx); err != nil || n != 1 {
		t.Errorf("Poll after recovery = %d, %v; want the hub's message", n, err)
	}
}

// Clearing hubSigned does not lift a pause over the hub's key: one from a
// bad signature, or one where the check after an unsigned reply found a
// changed key.
func TestClearingHubSignedKeepsKeyPause(t *testing.T) {
	for name, strip := range map[string]bool{"bad signature": false, "unsigned reply, changed key": true} {
		t.Run(name, func(t *testing.T) {
			oldKey := newCoordKey(t).PubKeyBase64()
			l, _, p, hubKS := proxiedLeaf(t, func(*keystore.Keystore) string { return oldKey })
			l.pins.hubSigned, l.pins.hubSignedCfg = true, true
			p.stripSig.Store(strip)
			ctx := context.Background()

			if _, err := l.Poll(ctx); err == nil {
				t.Fatal("Poll succeeded against a hub signing with another key")
			}
			if !l.HubPaused() {
				t.Fatal("not paused")
			}
			l.UpdatePins(ctx, Pins{Hub: oldKey, PendingHub: hubKS.PubKeyBase64(), HubSigned: false})
			if l.HubSigned() {
				t.Error("hubSigned not cleared")
			}
			if !l.HubPaused() {
				t.Error("clearing hubSigned lifted a pause over the hub's key")
			}
		})
	}
}

// A reload that read the config before the leaf stored hubSigned, and is
// applied after, is not taken for the sysop clearing it: the leaf checks the
// store, which holds the record. A real clear still applies.
func TestStaleReloadDoesNotClearHubSigned(t *testing.T) {
	l, _, _, hubKS := proxiedLeaf(t, (*keystore.Keystore).PubKeyBase64)
	var file atomic.Bool // what v3net.json holds
	l.cfg.OnHubSigned = func() error { file.Store(true); return nil }
	l.cfg.HubSignedStored = func() (bool, error) { return file.Load(), nil }
	ctx := context.Background()

	stale := Pins{Hub: hubKS.PubKeyBase64(), HubSigned: file.Load()} // read before the store
	if _, err := l.Poll(ctx); err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if !file.Load() {
		t.Fatal("hubSigned not stored")
	}
	l.UpdatePins(ctx, stale)
	if !l.HubSigned() {
		t.Fatal("a reload read before the store cleared hubSigned")
	}

	file.Store(false) // the sysop clears it
	l.UpdatePins(ctx, Pins{Hub: hubKS.PubKeyBase64(), HubSigned: false})
	if l.HubSigned() {
		t.Error("the sysop's clear did not apply")
	}
}

// fileRecorder stands in for v3net.json behind OnHubSigned and
// HubSignedStored. While fail is set a store reports a failure and leaves
// the file as it is.
type fileRecorder struct {
	calls atomic.Int32
	fail  atomic.Bool
	file  atomic.Bool // hubSigned as the file holds it
}

func (r *fileRecorder) attach(l *Leaf) {
	l.cfg.OnHubSigned = func() error {
		r.calls.Add(1)
		if r.fail.Load() {
			return errors.New("disk full")
		}
		r.file.Store(true)
		return nil
	}
	l.cfg.HubSignedStored = func() (bool, error) { return r.file.Load(), nil }
}

// The sysop clearing hubSigned while the leaf's store of it is waiting to be
// retried drops the record: the retry is not made, so the clear is not
// undone, and unsigned replies are accepted again. Once the hub signs again
// the leaf records it again and stores it.
func TestHubSignedClearCancelsRetry(t *testing.T) {
	l, _, p, hubKS := proxiedLeaf(t, (*keystore.Keystore).PubKeyBase64)
	rec := &fileRecorder{}
	rec.attach(l)
	rec.fail.Store(true)
	ctx := context.Background()

	if _, err := l.Poll(ctx); err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if !l.HubSigned() || rec.file.Load() {
		t.Fatalf("hubSigned=%v file=%v; want enforced but not stored", l.HubSigned(), rec.file.Load())
	}
	p.stripSig.Store(true) // the hub went back to a version that does not sign
	if _, err := l.Poll(ctx); !errors.Is(err, ErrHubUnsigned) {
		t.Fatalf("Poll of the rolled-back hub = %v, want ErrHubUnsigned", err)
	}

	rec.fail.Store(false) // a retry now would store it
	logs := captureLogs(t)
	l.UpdatePins(ctx, Pins{Hub: hubKS.PubKeyBase64(), HubSigned: false})
	if l.HubSigned() {
		t.Fatal("the sysop's clear did not drop the record not yet stored")
	}
	if !strings.Contains(logs.String(), "dropping the record not yet stored") {
		t.Errorf("no log of the dropped record:\n%s", logs)
	}
	for range 2 {
		if _, err := l.Poll(ctx); err != nil {
			t.Fatalf("Poll after the sysop cleared hubSigned: %v", err)
		}
	}
	if l.HubPaused() || l.HubSigned() || rec.file.Load() || rec.calls.Load() != 1 {
		t.Fatalf("paused=%v hubSigned=%v file=%v stores=%d; want unsigned replies accepted and no retried store",
			l.HubPaused(), l.HubSigned(), rec.file.Load(), rec.calls.Load())
	}

	p.stripSig.Store(false) // the hub is upgraded again
	if _, err := l.Poll(ctx); err != nil {
		t.Fatalf("Poll after the hub signs again: %v", err)
	}
	if !l.HubSigned() || !rec.file.Load() || rec.calls.Load() != 2 {
		t.Errorf("hubSigned=%v file=%v stores=%d; want the hub recorded and stored again",
			l.HubSigned(), rec.file.Load(), rec.calls.Load())
	}
}

// A reload that read the config while a store of hubSigned was waiting to
// be retried, and is applied after the retry succeeded, does not drop the
// record: the file then holds it.
func TestStaleReloadKeepsRetriedHubSigned(t *testing.T) {
	l, _, p, hubKS := proxiedLeaf(t, (*keystore.Keystore).PubKeyBase64)
	rec := &fileRecorder{}
	rec.attach(l)
	rec.fail.Store(true)
	ctx := context.Background()

	if _, err := l.Poll(ctx); err != nil {
		t.Fatalf("Poll: %v", err)
	}
	stale := Pins{Hub: hubKS.PubKeyBase64(), HubSigned: rec.file.Load()} // read while the store is pending
	rec.fail.Store(false)
	if _, err := l.Poll(ctx); err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if !rec.file.Load() {
		t.Fatal("retried store of hubSigned did not succeed")
	}
	l.UpdatePins(ctx, stale)
	if !l.HubSigned() {
		t.Fatal("a reload read before the retried store dropped hubSigned")
	}
	p.stripSig.Store(true)
	if _, err := l.Poll(ctx); !errors.Is(err, ErrHubUnsigned) {
		t.Errorf("Poll with the signature stripped = %v, want ErrHubUnsigned", err)
	}
}

// The sysop setting hubSigned by hand while the leaf's store of it is
// waiting to be retried leaves it set, and the retry is no longer needed.
func TestSysopSetsHubSignedWhileStorePending(t *testing.T) {
	l, _, _, hubKS := proxiedLeaf(t, (*keystore.Keystore).PubKeyBase64)
	rec := &fileRecorder{}
	rec.attach(l)
	rec.fail.Store(true)
	ctx := context.Background()

	if _, err := l.Poll(ctx); err != nil {
		t.Fatalf("Poll: %v", err)
	}
	rec.file.Store(true) // the sysop sets Hub Signs
	l.UpdatePins(ctx, Pins{Hub: hubKS.PubKeyBase64(), HubSigned: true})
	if _, err := l.Poll(ctx); err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if !l.HubSigned() || rec.calls.Load() != 1 {
		t.Errorf("hubSigned=%v stores=%d; want it kept and not stored again", l.HubSigned(), rec.calls.Load())
	}
}

// A change to hubSigned alone, while syncing is paused over the hub's key,
// is logged as that, not as a change of the pinned keys.
func TestHubSignedOnlyChangeLog(t *testing.T) {
	oldKey := newCoordKey(t).PubKeyBase64()
	l, _, _, _ := proxiedLeaf(t, func(*keystore.Keystore) string { return oldKey })
	l.pins.hubSigned, l.pins.hubSignedCfg = true, true
	ctx := context.Background()
	if _, err := l.Poll(ctx); err == nil || !l.HubPaused() {
		t.Fatalf("Poll = %v, paused=%v; want a pause over the hub's key", err, l.HubPaused())
	}

	for _, signed := range []bool{false, true} {
		logs := captureLogs(t)
		l.UpdatePins(ctx, Pins{Hub: oldKey, PendingHub: l.PendingKey(KeyHub), HubSigned: signed})
		out := logs.String()
		if !strings.Contains(out, "hub signing record changed in config") {
			t.Errorf("hubSigned=%v: no log of the change:\n%s", signed, out)
		}
		if strings.Contains(out, "pinned keys changed") {
			t.Errorf("hubSigned=%v: logged as a change of the pinned keys:\n%s", signed, out)
		}
	}
}
