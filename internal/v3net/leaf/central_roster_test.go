package leaf

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
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
	if _, err := coord.PublishRoster(context.Background(), []protocol.CentralRosterMember{memberOf(alpha), memberOf(beta)}); err != nil {
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

	first, err := coord.PublishRoster(context.Background(), []protocol.CentralRosterMember{memberOf(alpha)})
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
