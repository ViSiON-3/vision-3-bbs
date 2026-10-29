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

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/keystore"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/nal"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// newCoordKey returns a fresh keystore to sign NALs with.
func newCoordKey(t *testing.T) *keystore.Keystore {
	t.Helper()
	ks, _, err := keystore.Load(filepath.Join(t.TempDir(), "coord.key"))
	if err != nil {
		t.Fatalf("load keystore: %v", err)
	}
	return ks
}

// signedNAL returns a testnet NAL with the given area tags, signed by ks.
func signedNAL(t *testing.T, ks *keystore.Keystore, tags ...string) *protocol.NAL {
	t.Helper()
	n := &protocol.NAL{V3NetNAL: "1.0", Network: "testnet", Areas: []protocol.Area{}}
	for _, tag := range tags {
		n.Areas = append(n.Areas, protocol.Area{Tag: tag, Name: tag, Access: protocol.AreaAccess{Mode: protocol.AccessModeOpen}})
	}
	if err := nal.Sign(n, ks); err != nil {
		t.Fatalf("sign NAL: %v", err)
	}
	return n
}

// nalServer serves whatever NAL was last stored in it.
type nalServer struct {
	mu  sync.Mutex
	nal *protocol.NAL
}

func (s *nalServer) set(n *protocol.NAL) {
	s.mu.Lock()
	s.nal = n
	s.mu.Unlock()
}

func (s *nalServer) start(t *testing.T) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		_ = json.NewEncoder(w).Encode(s.nal)
	}))
	t.Cleanup(ts.Close)
	return ts
}

// keyRecorder collects the key events a leaf reports.
type keyRecorder struct {
	mu              sync.Mutex
	pinned, changed []KeyEvent
}

func (r *keyRecorder) attach(l *Leaf) {
	l.cfg.OnKeyPinned = func(ev KeyEvent) { r.mu.Lock(); r.pinned = append(r.pinned, ev); r.mu.Unlock() }
	l.cfg.OnKeyChanged = func(ev KeyEvent) { r.mu.Lock(); r.changed = append(r.changed, ev); r.mu.Unlock() }
}

func (r *keyRecorder) counts() (pinned, changed int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.pinned), len(r.changed)
}

// pinnedLeaf returns a leaf on ts whose coordinator key is pinned to pin.
func pinnedLeaf(t *testing.T, ts *httptest.Server, pin string) (*Leaf, *keyRecorder) {
	t.Helper()
	l, _ := setupLeaf(t, ts.URL, &mockJAMWriter{})
	l.pins.coord = pin
	rec := &keyRecorder{}
	rec.attach(l)
	return l, rec
}

// A NAL with a perfectly valid signature is still rejected when it is signed
// by a key other than the pinned coordinator key, and the last good NAL stays.
func TestNALSignedByUnpinnedKeyIsRejected(t *testing.T) {
	trusted, impostor := newCoordKey(t), newCoordKey(t)
	srv := &nalServer{}
	ts := srv.start(t)
	l, rec := pinnedLeaf(t, ts, trusted.PubKeyBase64())
	var onNAL atomic.Int32
	l.cfg.OnNAL = func(*protocol.NAL) { onNAL.Add(1) }
	ctx := context.Background()

	srv.set(signedNAL(t, trusted, "gen.good"))
	if err := l.refreshNAL(ctx); err != nil {
		t.Fatalf("refresh with the pinned key: %v", err)
	}

	forged := signedNAL(t, impostor, "gen.evil")
	if err := nal.Verify(forged); err != nil {
		t.Fatalf("test setup: forged NAL should carry a valid signature: %v", err)
	}
	srv.set(forged)

	if _, err := l.FetchNAL(ctx); !errors.Is(err, nal.ErrCoordKeyMismatch) {
		t.Errorf("FetchNAL = %v, want ErrCoordKeyMismatch", err)
	}
	if err := l.refreshNAL(ctx); !errors.Is(err, nal.ErrCoordKeyMismatch) {
		t.Errorf("refreshNAL = %v, want ErrCoordKeyMismatch", err)
	}
	if got := onNAL.Load(); got != 1 {
		t.Errorf("OnNAL ran %d times, want 1 (the forged NAL must not reach it)", got)
	}
	areas, err := l.Areas(ctx)
	if err != nil || len(areas) != 1 || areas[0].Tag != "gen.good" {
		t.Errorf("Areas = %+v, %v; want the last good NAL's gen.good", areas, err)
	}

	// The pin is unchanged, the offered key is recorded and reported once.
	if got := l.PinnedKey(KeyCoordinator); got != trusted.PubKeyBase64() {
		t.Errorf("pin moved to %q", got)
	}
	if got := l.PendingKey(KeyCoordinator); got != impostor.PubKeyBase64() {
		t.Errorf("pending key = %q, want the impostor's", got)
	}
	if pinned, changed := rec.counts(); pinned != 0 || changed != 1 {
		t.Fatalf("events: %d pinned, %d changed; want 0 and 1", pinned, changed)
	}
	ev := rec.changed[0]
	if ev.Kind != KeyCoordinator || ev.Network != "testnet" || ev.Pinned != trusted.PubKeyBase64() || ev.Offered != impostor.PubKeyBase64() {
		t.Errorf("change event = %+v", ev)
	}
}

// A NAL whose coordinator_node_id is not derived from its key is rejected,
// and does not get its key pinned.
func TestNALWithMismatchedCoordNodeIDIsRejected(t *testing.T) {
	ks := newCoordKey(t)
	l, rec := pinnedLeaf(t, cannedHub(t, http.StatusOK, "{}"), "")

	n := signedNAL(t, ks, "gen.general")
	n.CoordNodeID = "0123456789abcdef"
	if err := l.checkNAL(n); !errors.Is(err, nal.ErrCoordNodeIDMismatch) {
		t.Errorf("checkNAL = %v, want ErrCoordNodeIDMismatch", err)
	}
	if got := l.PinnedKey(KeyCoordinator); got != "" {
		t.Errorf("a rejected NAL pinned %q", got)
	}
	if pinned, changed := rec.counts(); pinned != 0 || changed != 0 {
		t.Errorf("events: %d pinned, %d changed; want none", pinned, changed)
	}

	n.CoordNodeID = ks.NodeID()
	if err := l.checkNAL(n); err != nil {
		t.Errorf("checkNAL with the right node ID: %v", err)
	}
}

// A leaf with no pin (a new subscription, or one from before pinning) pins
// the first verified NAL's key and holds every later NAL to it.
func TestFirstNALPinsCoordinatorKey(t *testing.T) {
	first, other := newCoordKey(t), newCoordKey(t)
	srv := &nalServer{}
	ts := srv.start(t)
	l, rec := pinnedLeaf(t, ts, "")
	ctx := context.Background()

	srv.set(signedNAL(t, first, "gen.general"))
	if err := l.refreshNAL(ctx); err != nil {
		t.Fatalf("first refresh: %v", err)
	}
	if got := l.PinnedKey(KeyCoordinator); got != first.PubKeyBase64() {
		t.Fatalf("pinned %q, want the first NAL's key", got)
	}
	if pinned, changed := rec.counts(); pinned != 1 || changed != 0 {
		t.Fatalf("events: %d pinned, %d changed; want 1 and 0", pinned, changed)
	}
	if ev := rec.pinned[0]; ev.Kind != KeyCoordinator || ev.Pinned != first.PubKeyBase64() || ev.Offered != ev.Pinned {
		t.Errorf("pin event = %+v", ev)
	}

	// The same key again is not a new pin.
	if err := l.refreshNAL(ctx); err != nil {
		t.Fatalf("second refresh: %v", err)
	}
	if pinned, _ := rec.counts(); pinned != 1 {
		t.Errorf("re-pinned on a matching NAL (%d pin events)", pinned)
	}

	srv.set(signedNAL(t, other, "gen.general"))
	if err := l.refreshNAL(ctx); !errors.Is(err, nal.ErrCoordKeyMismatch) {
		t.Errorf("refresh with another key = %v, want ErrCoordKeyMismatch", err)
	}
}

// A changed key is reported once, however often the hub serves it, and the
// sysop accepting it (pinning the pending key) clears the pending record.
func TestChangedKeyReportedOnceAndAccepted(t *testing.T) {
	oldKey, newKey := newCoordKey(t), newCoordKey(t)
	srv := &nalServer{}
	srv.set(signedNAL(t, newKey, "gen.general"))
	ts := srv.start(t)
	l, rec := pinnedLeaf(t, ts, oldKey.PubKeyBase64())
	ctx := context.Background()

	for range 3 {
		if err := l.refreshNAL(ctx); !errors.Is(err, nal.ErrCoordKeyMismatch) {
			t.Fatalf("refresh = %v, want ErrCoordKeyMismatch", err)
		}
	}
	if _, changed := rec.counts(); changed != 1 {
		t.Errorf("reported the change %d times, want once", changed)
	}

	// A leaf restarted with the pending key from config does not report it
	// again either.
	l2, rec2 := pinnedLeaf(t, ts, oldKey.PubKeyBase64())
	l2.pins.pendingCoord = newKey.PubKeyBase64()
	if err := l2.refreshNAL(ctx); !errors.Is(err, nal.ErrCoordKeyMismatch) {
		t.Fatalf("refresh = %v, want ErrCoordKeyMismatch", err)
	}
	if _, changed := rec2.counts(); changed != 0 {
		t.Errorf("an already-pending key was reported again")
	}

	// The sysop accepts: the pin is now the new key, pending still set.
	l3, rec3 := pinnedLeaf(t, ts, newKey.PubKeyBase64())
	l3.pins.pendingCoord = newKey.PubKeyBase64()
	if err := l3.refreshNAL(ctx); err != nil {
		t.Fatalf("refresh after accepting: %v", err)
	}
	if got := l3.PendingKey(KeyCoordinator); got != "" {
		t.Errorf("pending key %q not cleared after acceptance", got)
	}
	if pinned, _ := rec3.counts(); pinned != 1 {
		t.Errorf("acceptance raised %d pin events, want 1 (to clear the stored pending key)", pinned)
	}
}

// The hub key from /info is pinned on first use and a different one is
// rejected with ErrHubKeyMismatch.
func TestHubKeyPinnedFromInfo(t *testing.T) {
	ts, _, hubKS := newTestHub(t, true)
	l, rec := pinnedLeaf(t, ts, "")
	ctx := context.Background()

	if err := l.checkHubKey(ctx); err != nil {
		t.Fatalf("checkHubKey: %v", err)
	}
	if got := l.PinnedKey(KeyHub); got != hubKS.PubKeyBase64() {
		t.Errorf("hub pin = %q, want the hub's key", got)
	}
	if pinned, _ := rec.counts(); pinned != 1 || rec.pinned[0].Kind != KeyHub {
		t.Errorf("pin events = %+v", rec.pinned)
	}

	l2, rec2 := pinnedLeaf(t, ts, "")
	l2.pins.hub = newCoordKey(t).PubKeyBase64()
	if err := l2.checkHubKey(ctx); !errors.Is(err, ErrHubKeyMismatch) {
		t.Errorf("checkHubKey = %v, want ErrHubKeyMismatch", err)
	}
	if _, changed := rec2.counts(); changed != 1 || rec2.changed[0].Offered != hubKS.PubKeyBase64() {
		t.Errorf("change events = %+v", rec2.changed)
	}
}

// Pins handed to New are trimmed of pasted whitespace and enforced.
func TestNewSeedsPinsFromConfig(t *testing.T) {
	ks := newCoordKey(t)
	l, _ := setupLeaf(t, "http://unused.invalid", &mockJAMWriter{})
	cfg := l.cfg
	cfg.CoordPubKeyB64 = "  " + ks.PubKeyBase64() + "\n"
	cfg.PendingHubPubKeyB64 = "pending"
	l = New(cfg)
	if got := l.PinnedKey(KeyCoordinator); got != ks.PubKeyBase64() {
		t.Errorf("coordinator pin = %q", got)
	}
	if got := l.PendingKey(KeyHub); got != "pending" {
		t.Errorf("pending hub key = %q", got)
	}
	if err := l.checkNAL(signedNAL(t, newCoordKey(t))); !errors.Is(err, nal.ErrCoordKeyMismatch) {
		t.Errorf("checkNAL = %v, want ErrCoordKeyMismatch", err)
	}
}
