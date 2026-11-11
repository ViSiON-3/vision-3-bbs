package leaf

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/keystore"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// coordLeaf is a leaf running as the hub's own node, which coordinates
// testnet, subscribed and knowing the hub's features.
func coordLeaf(t *testing.T, ts *httptest.Server, hubKS *keystore.Keystore) *Leaf {
	t.Helper()
	l, _ := setupLeaf(t, ts.URL, &mockJAMWriter{})
	l.cfg.Keystore = hubKS
	l.cfg.BBSName = "Hub"
	if err := l.checkHubKey(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := l.subscribe(context.Background()); err != nil {
		t.Fatal(err)
	}
	pinCoord(t, l, hubKS)
	t.Cleanup(l.Close)
	return l
}

// pinCoord pins ks as l's coordinator key and reads the hub's features.
func pinCoord(t *testing.T, l *Leaf, ks *keystore.Keystore) {
	t.Helper()
	l.pinMu.Lock()
	l.pins.coord = ks.PubKeyBase64()
	l.pinMu.Unlock()
	if err := l.fetchHubFeatures(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func memberOf(l *Leaf) protocol.CentralRosterMember {
	return protocol.CentralRosterMember{NodeID: l.cfg.Keystore.NodeID(), PubKeyB64: l.cfg.Keystore.PubKeyBase64()}
}

// With a roster, a board shows Central listings and alerts only from boards
// on it, matched on key; without one, it shows them all.
func TestRosterFiltersCentral(t *testing.T) {
	ts, h, hubKS := newTestHub(t, true)
	seedHubNAL(t, h, hubKS)
	coord := coordLeaf(t, ts, hubKS)
	alpha := subscribedLeaf(t, ts, "Alpha")
	beta := subscribedLeaf(t, ts, "Beta")
	gamma := subscribedLeaf(t, ts, "Gamma")
	for _, l := range []*Leaf{alpha, beta, gamma} {
		pinCoord(t, l, hubKS)
	}

	// Alpha and Gamma publish listings.
	for _, l := range []*Leaf{alpha, gamma} {
		l.cfg.CentralListing = &CentralListingSpec{Name: l.cfg.BBSName, Addresses: []string{"telnet://" + l.cfg.BBSHost + ":23"}}
		if err := l.publishCentralListing(context.Background()); err != nil {
			t.Fatalf("publish %s: %v", l.cfg.BBSName, err)
		}
	}
	listed := func() map[string]bool {
		t.Helper()
		beta.central.dirStale = true
		dir, err := beta.FetchCentralDirectory(context.Background(), true)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]bool{}
		for _, e := range dir {
			out[e.Listing.Name] = true
		}
		return out
	}
	if got := listed(); !got["Alpha"] || !got["Gamma"] {
		t.Fatalf("no roster: %v", got)
	}

	// The coordinator admits Alpha and Beta, not Gamma.
	if _, err := coord.PublishRoster(context.Background(), 0, []protocol.CentralRosterMember{memberOf(alpha), memberOf(beta)}); err != nil {
		t.Fatal(err)
	}
	if err := beta.fetchRoster(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := listed(); !got["Alpha"] || got["Gamma"] {
		t.Errorf("with a roster: %v, want Alpha and not Gamma", got)
	}

	// Alerts: Gamma's is dropped, Alpha's kept.
	got := receiveAlerts(t, beta)
	for _, l := range []*Leaf{alpha, gamma} {
		a := protocol.CentralAlert{V3NetCentral: protocol.CentralAlertVersion, Kind: protocol.CentralAlertLogon, Network: "testnet",
			BBS: l.cfg.BBSName, Handle: "Caller", PublishedAt: time.Now().UTC().Format(time.RFC3339),
			ExpiresAt: time.Now().UTC().Add(time.Minute).Format(time.RFC3339)}
		a.ID, _ = protocol.NewCentralAlertID()
		if err := a.Sign(l.cfg.Keystore); err != nil {
			t.Fatal(err)
		}
		data, _ := json.Marshal(a)
		beta.dispatchAlertEvent(protocol.Event{Type: protocol.EventCentralAlert, Data: data})
	}
	select {
	case a := <-got:
		if a.BBS != "Alpha" {
			t.Errorf("got %s's alert", a.BBS)
		}
	default:
		t.Fatal("the member's alert was dropped")
	}
	noAlert(t, got, "an alert from a board not on the roster")
}

// A roster is taken only when signed by the pinned coordinator key, an
// older one never replaces a newer one, and the one taken survives a
// restart and a hub that stops serving it.
func TestRosterTrustAndKeeping(t *testing.T) {
	ts, h, hubKS := newTestHub(t, true)
	seedHubNAL(t, h, hubKS)
	coord := coordLeaf(t, ts, hubKS)
	alpha := subscribedLeaf(t, ts, "Alpha")
	path := filepath.Join(t.TempDir(), "roster.json")
	alpha.cfg.RosterPath = path
	pinCoord(t, alpha, hubKS)

	first, err := coord.PublishRoster(context.Background(), 0, []protocol.CentralRosterMember{memberOf(alpha)})
	if err != nil {
		t.Fatal(err)
	}
	if err := alpha.fetchRoster(context.Background()); err != nil {
		t.Fatal(err)
	}
	if st := alpha.RosterStatus(); !st.Held || st.Seq != first.Seq || st.Members != 1 {
		t.Fatalf("status %+v", st)
	}

	// A roster signed by another key, as a lying hub might serve, is refused.
	other := subscribedLeaf(t, ts, "Other")
	fake := &protocol.CentralRoster{V3NetCentral: protocol.CentralRosterVersion, Network: "testnet", Seq: first.Seq + 10,
		PublishedAt: time.Now().UTC().Format(time.RFC3339)}
	if err := fake.Sign(other.cfg.Keystore); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(fake)
	if err := alpha.takeRoster(fake, raw); err == nil {
		t.Error("a roster not signed by the pinned coordinator was taken")
	}
	// An older roster from the coordinator does not replace the newer.
	older := *first
	older.Seq = first.Seq - 1
	older.Members = nil
	_ = older.Sign(hubKS)
	raw, _ = json.Marshal(&older)
	_ = alpha.takeRoster(&older, raw)
	if alpha.RosterStatus().Seq != first.Seq {
		t.Error("an older roster replaced the newer one")
	}

	// A restart loads the kept roster before any fetch.
	again := New(alpha.cfg)
	if st := again.RosterStatus(); !st.Held || st.Seq != first.Seq {
		t.Errorf("after restart: %+v", st)
	}
}

// A board on a hub that does not serve rosters fetches nothing and filters
// nothing.
func TestRosterUnsupportedHub(t *testing.T) {
	ts, _, _ := newTestHub(t, true)
	alpha := subscribedLeaf(t, ts, "Alpha")
	alpha.noteHubFeatures([]string{protocol.FeatureCentralDirectory})
	if err := alpha.fetchRoster(context.Background()); err != nil {
		t.Errorf("fetch on an older hub: %v", err)
	}
	if alpha.RosterStatus().Held || !alpha.rosterAdmits("anyone", "anykey") {
		t.Error("filtering without a roster")
	}
}

// waitRoster waits until l holds a roster at seq.
func waitRoster(t *testing.T, l *Leaf, seq int64, what string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for l.RosterStatus().Seq != seq {
		if time.Now().After(deadline) {
			t.Fatalf("%s: holding seq %d, want %d", what, l.RosterStatus().Seq, seq)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// An update announced while a fetch is running is not lost: the fetch runs
// once more after it.
func TestRosterUpdateDuringFetchNotLost(t *testing.T) {
	ts, h, hubKS := newTestHub(t, true)
	seedHubNAL(t, h, hubKS)
	coord := coordLeaf(t, ts, hubKS)
	alpha := subscribedLeaf(t, ts, "Alpha")
	pinCoord(t, alpha, hubKS)

	// A second request while one runs is remembered.
	alpha.rosterState.fetching = true
	alpha.refreshRosterSoon(context.Background())
	if !alpha.rosterState.again {
		t.Fatal("a request during a fetch was dropped")
	}
	alpha.rosterState.fetching, alpha.rosterState.again = false, false

	first, err := coord.PublishRoster(context.Background(), 0, []protocol.CentralRosterMember{memberOf(alpha)})
	if err != nil {
		t.Fatal(err)
	}
	alpha.refreshRosterSoon(context.Background())
	second, err := coord.PublishRoster(context.Background(), first.Seq, nil)
	if err != nil {
		t.Fatal(err)
	}
	alpha.refreshRosterSoon(context.Background())
	waitRoster(t, alpha, second.Seq, "after two updates")
}

// A roster refused because the pin had not yet followed a handover is
// fetched again once a NAL refresh moves the pin.
func TestRosterRetriedAfterPinMoves(t *testing.T) {
	ts, h, hubKS := newTestHub(t, true)
	seedHubNAL(t, h, hubKS)
	coord := coordLeaf(t, ts, hubKS)
	alpha := subscribedLeaf(t, ts, "Alpha")
	stale := subscribedLeaf(t, ts, "OldCoord")
	pinCoord(t, alpha, stale.cfg.Keystore) // pinned to the coordinator before the handover

	r, err := coord.PublishRoster(context.Background(), 0, []protocol.CentralRosterMember{memberOf(alpha)})
	if err != nil {
		t.Fatal(err)
	}
	if err := alpha.fetchRoster(context.Background()); err == nil {
		t.Fatal("a roster from another key than the pin was taken")
	}
	// The NAL refresh follows the handover and moves the pin.
	alpha.pinMu.Lock()
	alpha.pins.coord = hubKS.PubKeyBase64()
	alpha.pinMu.Unlock()
	alpha.rosterAfterNAL(context.Background())
	waitRoster(t, alpha, r.Seq, "after the pin moved")
}

// A roster taken but not written to disk is written on the next sight of
// it, so a restart cannot fall back to an older one.
func TestRosterUnsavedRetried(t *testing.T) {
	ts, h, hubKS := newTestHub(t, true)
	seedHubNAL(t, h, hubKS)
	coord := coordLeaf(t, ts, hubKS)
	alpha := subscribedLeaf(t, ts, "Alpha")
	dir := t.TempDir()
	blocker := filepath.Join(dir, "rosters")
	if err := os.WriteFile(blocker, []byte("a file where the directory goes"), 0o644); err != nil {
		t.Fatal(err)
	}
	alpha.cfg.RosterPath = filepath.Join(blocker, "testnet.json")
	pinCoord(t, alpha, hubKS)

	r, err := coord.PublishRoster(context.Background(), 0, []protocol.CentralRosterMember{memberOf(alpha)})
	if err != nil {
		t.Fatal(err)
	}
	if err := alpha.fetchRoster(context.Background()); err == nil {
		t.Fatal("keeping the roster should have failed")
	}
	if !alpha.RosterStatus().Held {
		t.Fatal("the roster should be in force in memory")
	}
	// The disk recovers; the next fetch of the same roster keeps it.
	if err := os.Remove(blocker); err != nil {
		t.Fatal(err)
	}
	if err := alpha.fetchRoster(context.Background()); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if again := New(alpha.cfg); again.RosterStatus().Seq != r.Seq {
		t.Errorf("after restart holding seq %d, want %d", again.RosterStatus().Seq, r.Seq)
	}
}

// An edit started from a roster that has since changed publishes nothing
// and says so; one from the current roster goes through.
func TestPublishRosterChangedSinceEdit(t *testing.T) {
	ts, h, hubKS := newTestHub(t, true)
	seedHubNAL(t, h, hubKS)
	coord := coordLeaf(t, ts, hubKS)
	alpha := subscribedLeaf(t, ts, "Alpha")

	first, err := coord.PublishRoster(context.Background(), 0, []protocol.CentralRosterMember{memberOf(alpha)})
	if err != nil {
		t.Fatal(err)
	}
	// Another session, still showing no roster, edits from nothing.
	if _, err := coord.PublishRoster(context.Background(), 0, nil); !errors.Is(err, ErrRosterChanged) {
		t.Errorf("edit from a stale view: %v, want ErrRosterChanged", err)
	}
	// A second coordinator session that never fetched sees the hub's roster
	// first, and so refuses the stale edit too.
	other := New(coord.cfg)
	other.noteHubFeatures([]string{protocol.FeatureCentralRoster})
	pinCoord(t, other, hubKS)
	if _, err := other.PublishRoster(context.Background(), 0, nil); !errors.Is(err, ErrRosterChanged) {
		t.Errorf("unfetched session: %v, want ErrRosterChanged", err)
	}
	if _, err := coord.PublishRoster(context.Background(), first.Seq, nil); err != nil {
		t.Errorf("edit from the current roster: %v", err)
	}
}

// The coordinator's sequence never wraps: at the top it refuses to publish
// rather than send a negative seq.
func TestPublishRosterSeqExhausted(t *testing.T) {
	ts, h, hubKS := newTestHub(t, true)
	seedHubNAL(t, h, hubKS)
	coord := coordLeaf(t, ts, hubKS)
	top := &protocol.CentralRoster{V3NetCentral: protocol.CentralRosterVersion, Network: "testnet", Seq: math.MaxInt64,
		PublishedAt: time.Now().UTC().Format(time.RFC3339)}
	if err := top.Sign(hubKS); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(top)
	if err := coord.takeRoster(top, raw); err != nil {
		t.Fatal(err)
	}
	coord.noteHubFeatures(nil) // keep the fetch from replacing it
	if _, err := coord.PublishRoster(context.Background(), math.MaxInt64, nil); err == nil || !strings.Contains(err.Error(), "exhausted") {
		t.Errorf("got %v, want the sequence exhausted", err)
	}
}

// A first fetch that fails is tried again, so a board does not run
// unfiltered for want of a later event.
func TestStartRosterRetries(t *testing.T) {
	ts, h, hubKS := newTestHub(t, true)
	seedHubNAL(t, h, hubKS)
	coord := coordLeaf(t, ts, hubKS)
	alpha := subscribedLeaf(t, ts, "Alpha")
	r, err := coord.PublishRoster(context.Background(), 0, []protocol.CentralRosterMember{memberOf(alpha)})
	if err != nil {
		t.Fatal(err)
	}
	oldMin := rosterRetryMin
	rosterRetryMin = 20 * time.Millisecond
	t.Cleanup(func() { rosterRetryMin = oldMin })
	// No coordinator pinned yet: refused, but that is final for now. Pin
	// it, then make the hub fail the first fetch by pausing the leaf.
	pinCoord(t, alpha, hubKS)
	alpha.hubMu.Lock()
	alpha.hub.paused = true
	alpha.hubMu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { alpha.startRoster(ctx); close(done) }()
	time.Sleep(60 * time.Millisecond)
	alpha.hubMu.Lock()
	alpha.hub.paused = false
	alpha.hubMu.Unlock()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("startRoster never finished")
	}
	if alpha.RosterStatus().Seq != r.Seq {
		t.Errorf("holding seq %d, want %d", alpha.RosterStatus().Seq, r.Seq)
	}
}
