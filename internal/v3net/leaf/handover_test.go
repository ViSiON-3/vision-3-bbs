package leaf

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/hub"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/keystore"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/nal"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// handoverChain returns the signed handovers keys[0]->keys[1]->... .
func handoverChain(t *testing.T, keys ...*keystore.Keystore) []protocol.CoordHandover {
	t.Helper()
	var hs []protocol.CoordHandover
	for i := 0; i+1 < len(keys); i++ {
		h, err := protocol.NewCoordHandover(keys[i], "testnet", keys[i+1].PubKeyBase64(), time.Now())
		if err != nil {
			t.Fatalf("NewCoordHandover: %v", err)
		}
		hs = append(hs, *h)
	}
	return hs
}

// A NAL signed by an unpinned key is accepted when the handovers served
// with it lead from the pin to that key, over one step or several: the pin
// moves, and OnKeyPinned is told the new key and the one it replaced.
func TestNALHandoverMovesPin(t *testing.T) {
	for _, steps := range []int{1, 2} {
		keys := []*keystore.Keystore{newCoordKey(t)}
		for i := 0; i < steps; i++ {
			keys = append(keys, newCoordKey(t))
		}
		first, last := keys[0].PubKeyBase64(), keys[steps].PubKeyBase64()
		srv := &nalServer{}
		ts := srv.start(t)
		l, rec := pinnedLeaf(t, ts, first)

		n := signedNAL(t, keys[steps], "gen.moved")
		n.Handovers = handoverChain(t, keys...)
		srv.set(n)
		if err := l.refreshNAL(context.Background()); err != nil {
			t.Fatalf("%d steps: refreshNAL: %v", steps, err)
		}
		if got := l.PinnedKey(KeyCoordinator); got != last {
			t.Errorf("%d steps: pin = %s, want the new key", steps, got)
		}
		rec.mu.Lock()
		pinned, changed := rec.pinned, rec.changed
		rec.mu.Unlock()
		if len(pinned) != 1 || pinned[0].Pinned != last || pinned[0].Previous != first || pinned[0].Kind != KeyCoordinator {
			t.Errorf("%d steps: OnKeyPinned events = %+v, want one moving %s to %s", steps, pinned, first, last)
		}
		if len(changed) != 0 {
			t.Errorf("%d steps: OnKeyChanged events = %+v, want none", steps, changed)
		}

		// Once stored, later fetches do not report the move again.
		if err := l.refreshNAL(context.Background()); err != nil {
			t.Fatal(err)
		}
		if p, _ := rec.counts(); p != 1 {
			t.Errorf("%d steps: OnKeyPinned ran %d times after a second fetch, want 1", steps, p)
		}
	}
}

// Handovers that do not start at the pin change nothing: the NAL is
// rejected and its key recorded as pending, as with no handover at all.
func TestNALHandoverNotFromPinRejected(t *testing.T) {
	pinnedKS, k1, k2 := newCoordKey(t), newCoordKey(t), newCoordKey(t)
	srv := &nalServer{}
	ts := srv.start(t)
	l, rec := pinnedLeaf(t, ts, pinnedKS.PubKeyBase64())

	n := signedNAL(t, k2, "gen.evil")
	n.Handovers = handoverChain(t, k1, k2)
	srv.set(n)
	if err := l.refreshNAL(context.Background()); !errors.Is(err, nal.ErrCoordKeyMismatch) {
		t.Fatalf("refreshNAL = %v, want ErrCoordKeyMismatch", err)
	}
	if got := l.PinnedKey(KeyCoordinator); got != pinnedKS.PubKeyBase64() {
		t.Errorf("pin moved to %s", got)
	}
	if got := l.PendingKey(KeyCoordinator); got != k2.PubKeyBase64() {
		t.Errorf("pending = %q, want the offered key", got)
	}
	if p, c := rec.counts(); p != 0 || c != 1 {
		t.Errorf("events: %d pinned, %d changed; want 0 and 1", p, c)
	}
}

// A key first offered without a handover is pending; when the handover
// arrives the pin moves to it and the pending record goes.
func TestNALHandoverClearsPending(t *testing.T) {
	k0, k1 := newCoordKey(t), newCoordKey(t)
	srv := &nalServer{}
	ts := srv.start(t)
	l, rec := pinnedLeaf(t, ts, k0.PubKeyBase64())

	n := signedNAL(t, k1, "gen.general")
	srv.set(n)
	if err := l.refreshNAL(context.Background()); !errors.Is(err, nal.ErrCoordKeyMismatch) {
		t.Fatalf("refreshNAL without a handover = %v, want ErrCoordKeyMismatch", err)
	}
	withHandover := *n
	withHandover.Handovers = handoverChain(t, k0, k1)
	srv.set(&withHandover)
	if err := l.refreshNAL(context.Background()); err != nil {
		t.Fatalf("refreshNAL with the handover: %v", err)
	}
	if got := l.PendingKey(KeyCoordinator); got != "" {
		t.Errorf("pending = %q, want none", got)
	}
	if p, c := rec.counts(); p != 1 || c != 1 {
		t.Errorf("events: %d pinned, %d changed; want 1 and 1", p, c)
	}
}

// End to end: the coordinator hands the network to a new key through the
// hub, the hub restarts with that key, and a leaf pinned to the old key for
// both the hub and the coordinator moves both pins without pausing.
func TestHandoverThroughHubMovesBothPins(t *testing.T) {
	dir := t.TempDir()
	oldKS, err := loadKeystoreAt(filepath.Join(dir, "old.key"))
	if err != nil {
		t.Fatal(err)
	}
	newKS, err := loadKeystoreAt(filepath.Join(dir, "new.key"))
	if err != nil {
		t.Fatal(err)
	}
	var current atomic.Pointer[hub.Hub]
	open := func(ks *keystore.Keystore) *hub.Hub {
		t.Helper()
		h, err := hub.New(hub.Config{ListenAddr: ":0", DataDir: dir, Keystore: ks, AutoApprove: true,
			RequireSignedSubscribe: true, Networks: []hub.NetworkConfig{{Name: "testnet"}}})
		if err != nil {
			t.Fatalf("create hub: %v", err)
		}
		current.Store(h)
		return h
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current.Load().Mux().ServeHTTP(w, r)
	}))
	t.Cleanup(ts.Close)

	h := open(oldKS)
	seedHubNAL(t, h, oldKS, "gen.general")
	l := subscribedLeaf(t, ts, "Leaf BBS")
	rec := &keyRecorder{}
	rec.attach(l)
	ctx := context.Background()
	if err := l.checkHubKey(ctx); err != nil {
		t.Fatalf("pin hub key: %v", err)
	}
	if err := l.refreshNAL(ctx); err != nil {
		t.Fatalf("pin coordinator key: %v", err)
	}

	ho, err := protocol.NewCoordHandover(oldKS, "testnet", newKS.PubKeyBase64(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(ho)
	resp, err := l.signedPostWithResponse(ctx, "/v3net/v1/testnet/nal/handover", body)
	if err != nil {
		t.Fatalf("post handover: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("post handover: status %d", resp.StatusCode)
	}

	_ = h.Close()
	h = open(newKS)
	t.Cleanup(func() { _ = h.Close() })

	if err := l.checkHubKey(ctx); err != nil {
		t.Fatalf("checkHubKey after the handover: %v", err)
	}
	if err := l.refreshNAL(ctx); err != nil {
		t.Fatalf("refreshNAL after the handover: %v", err)
	}
	want := newKS.PubKeyBase64()
	if got := l.PinnedKey(KeyHub); got != want {
		t.Errorf("hub pin = %s, want the new key", got)
	}
	if got := l.PinnedKey(KeyCoordinator); got != want {
		t.Errorf("coordinator pin = %s, want the new key", got)
	}
	if l.HubPaused() {
		t.Error("sync with the hub is paused")
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	moved := 0
	for _, ev := range rec.pinned {
		if ev.Previous == oldKS.PubKeyBase64() && ev.Pinned == want {
			moved++
		}
	}
	if moved != 2 || len(rec.changed) != 0 {
		t.Errorf("events: pinned %+v, changed %+v; want both keys moved and nothing rejected", rec.pinned, rec.changed)
	}
}

// End to end, in the other order: the hub restarts with the new key first,
// so a leaf pinned to the old key pauses on the changed hub key. Once the
// old key's handover is posted, the hub re-signs the NAL with the new key,
// and the leaf moves both pins and resumes. The same holds when the hub
// already re-signed the NAL with the new key before the handover came, as
// merging initialAreas at startup does: the leaf rejects that NAL until the
// handover bridges its pin to the new key.
func TestLateHandoverThroughHubMovesBothPins(t *testing.T) {
	t.Run("NAL still the old key's", func(t *testing.T) { testLateHandoverThroughHub(t, false) })
	t.Run("NAL re-signed before the handover", func(t *testing.T) { testLateHandoverThroughHub(t, true) })
}

// testLateHandoverThroughHub runs TestLateHandoverThroughHubMovesBothPins,
// re-signing the NAL with the new key before the handover when resign is
// set.
func testLateHandoverThroughHub(t *testing.T, resign bool) {
	dir := t.TempDir()
	oldKS, err := loadKeystoreAt(filepath.Join(dir, "old.key"))
	if err != nil {
		t.Fatal(err)
	}
	newKS, err := loadKeystoreAt(filepath.Join(dir, "new.key"))
	if err != nil {
		t.Fatal(err)
	}
	var current atomic.Pointer[hub.Hub]
	open := func(ks *keystore.Keystore) *hub.Hub {
		t.Helper()
		h, err := hub.New(hub.Config{ListenAddr: ":0", DataDir: dir, Keystore: ks, AutoApprove: true,
			RequireSignedSubscribe: true, Networks: []hub.NetworkConfig{{Name: "testnet"}}})
		if err != nil {
			t.Fatalf("create hub: %v", err)
		}
		current.Store(h)
		return h
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current.Load().Mux().ServeHTTP(w, r)
	}))
	t.Cleanup(ts.Close)

	h := open(oldKS)
	seedHubNAL(t, h, oldKS, "gen.general")
	l := subscribedLeaf(t, ts, "Leaf BBS")
	poster := subscribedLeaf(t, ts, "Poster BBS")
	rec := &keyRecorder{}
	rec.attach(l)
	ctx := context.Background()
	if err := l.checkHubKey(ctx); err != nil {
		t.Fatalf("pin hub key: %v", err)
	}
	if err := l.refreshNAL(ctx); err != nil {
		t.Fatalf("pin coordinator key: %v", err)
	}

	_ = h.Close()
	h = open(newKS)
	t.Cleanup(func() { _ = h.Close() })
	if err := l.checkHubKey(ctx); !errors.Is(err, ErrHubKeyMismatch) {
		t.Fatalf("checkHubKey before the handover = %v, want ErrHubKeyMismatch", err)
	}
	if !l.HubPaused() {
		t.Fatal("sync with the hub is not paused on the changed hub key")
	}
	if resign {
		// What the initialAreas merge does at startup: add an area, sign
		// through SignNAL, store.
		n, err := h.NALStore().Get("testnet")
		if err != nil || n == nil {
			t.Fatalf("get NAL: %v", err)
		}
		n.Areas = append(n.Areas, protocol.Area{Tag: "gen.new", Name: "New", Language: "en",
			ManagerNodeID: newKS.NodeID(), ManagerPubKeyB64: newKS.PubKeyBase64(),
			Access: protocol.AreaAccess{Mode: protocol.AccessModeOpen},
			Policy: protocol.AreaPolicy{MaxBodyBytes: protocol.MaxBodyBytes}})
		if err := h.SignNAL(n); err != nil {
			t.Fatalf("SignNAL after the switch: %v", err)
		}
		if err := h.NALStore().Put("testnet", n); err != nil {
			t.Fatal(err)
		}
		if err := l.refreshNAL(ctx); !errors.Is(err, nal.ErrCoordKeyMismatch) {
			t.Fatalf("refreshNAL of the re-signed NAL = %v, want ErrCoordKeyMismatch", err)
		}
	}

	ho, err := protocol.NewCoordHandover(oldKS, "testnet", newKS.PubKeyBase64(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(ho)
	resp, err := poster.signedPostWithResponse(ctx, "/v3net/v1/testnet/nal/handover", body)
	if err != nil {
		t.Fatalf("post handover: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("post handover: status %d", resp.StatusCode)
	}

	if err := l.checkHubKey(ctx); err != nil {
		t.Fatalf("checkHubKey after the handover: %v", err)
	}
	if err := l.refreshNAL(ctx); err != nil {
		t.Fatalf("refreshNAL after the handover: %v", err)
	}
	want := newKS.PubKeyBase64()
	if got := l.PinnedKey(KeyHub); got != want {
		t.Errorf("hub pin = %s, want the new key", got)
	}
	if got := l.PinnedKey(KeyCoordinator); got != want {
		t.Errorf("coordinator pin = %s, want the new key", got)
	}
	for _, kind := range []KeyKind{KeyHub, KeyCoordinator} {
		if got := l.PendingKey(kind); got != "" {
			t.Errorf("pending %s key = %s, want none", kind, got)
		}
	}
	if l.HubPaused() {
		t.Error("sync with the hub is still paused")
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	moved := 0
	for _, ev := range rec.pinned {
		if ev.Previous == oldKS.PubKeyBase64() && ev.Pinned == want {
			moved++
		}
	}
	if moved != 2 {
		t.Errorf("events: pinned %+v; want both keys moved", rec.pinned)
	}
}

// Two handovers in quick succession, A to B and B to C, saved by
// overlapping calls: C's save waits for B's and then replaces B, so the
// stored pin ends at C. The store mimics persistKeyEvent, which writes a
// moved pin only over the key it moved from.
func TestSuccessiveHandoverSavesAreSerialized(t *testing.T) {
	a, b, c := newCoordKey(t), newCoordKey(t), newCoordKey(t)
	l, _ := setupLeaf(t, "http://unused.invalid", &mockJAMWriter{})
	l.pins.coord = a.PubKeyBase64()

	var mu sync.Mutex
	stored := a.PubKeyBase64()
	entered, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	l.cfg.OnKeyPinned = func(ev KeyEvent) error {
		mu.Lock()
		if stored == "" || stored == ev.Previous {
			stored = ev.Pinned
		}
		mu.Unlock()
		if calls.Add(1) == 1 {
			// B is stored; hold its save open while C's arrives. An
			// overlapping C save would name A as the key it replaces.
			close(entered)
			<-release
		}
		return nil
	}

	hs := handoverChain(t, a, b, c)
	if !l.followHandovers(KeyCoordinator, b.PubKeyBase64(), hs[:1]) {
		t.Fatal("A to B not followed")
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); l.savePin(KeyCoordinator) }()
	<-entered
	if !l.followHandovers(KeyCoordinator, c.PubKeyBase64(), hs) {
		t.Fatal("B to C not followed")
	}
	go func() { defer wg.Done(); l.savePin(KeyCoordinator) }()
	time.Sleep(20 * time.Millisecond) // let C's save reach the lock
	close(release)
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	if stored != c.PubKeyBase64() {
		t.Errorf("stored pin = %s, want C (B is %s)", stored, b.PubKeyBase64())
	}
	l.pinMu.Lock()
	defer l.pinMu.Unlock()
	if l.pins.coordUnsaved || l.pins.coordMovedFrom != "" {
		t.Errorf("save state left: unsaved %v, moved from %q", l.pins.coordUnsaved, l.pins.coordMovedFrom)
	}
}

// loadKeystoreAt creates a keystore at path.
func loadKeystoreAt(path string) (*keystore.Keystore, error) {
	ks, _, err := keystore.Load(path)
	return ks, err
}
