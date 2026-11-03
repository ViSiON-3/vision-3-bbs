package leaf

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/keystore"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// centralLeaf is subscribedLeaf with Central on and, with name set, a
// listing to publish, its hub features read.
func centralLeaf(t *testing.T, ts *httptest.Server, bbsName, listingName string) *Leaf {
	t.Helper()
	l := subscribedLeaf(t, ts, bbsName)
	l.cfg.CentralEnabled = true
	if listingName != "" {
		l.cfg.CentralListing = &CentralListingSpec{
			Name:        listingName,
			Description: "Test board " + listingName,
			Addresses:   []string{"ssh://" + bbsName + ".example:2222"},
		}
	}
	if err := l.fetchHubFeatures(context.Background()); err != nil {
		t.Fatalf("features: %v", err)
	}
	return l
}

func TestCentralPublishAndFetchDirectory(t *testing.T) {
	ts, _, _ := newTestHub(t, true)
	a := centralLeaf(t, ts, "alpha", "Alpha BBS")
	b := centralLeaf(t, ts, "beta", "")

	if supported, known := a.HubSupports(protocol.FeatureCentralDirectory); !supported || !known {
		t.Fatalf("hub support: supported=%v known=%v", supported, known)
	}
	if err := a.publishCentralListing(context.Background()); err != nil {
		t.Fatalf("publish: %v", err)
	}
	st := a.CentralStatus()
	if st.LastPublished.IsZero() || st.LastError != "" || !st.Publishing || !st.Supported {
		t.Errorf("status after publish: %+v", st)
	}

	dir, err := b.FetchCentralDirectory(context.Background(), false)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(dir) != 1 || dir[0].Listing.Name != "Alpha BBS" || dir[0].Listing.NodeID != a.cfg.Keystore.NodeID() {
		t.Fatalf("directory: %+v", dir)
	}
	if !dir[0].Listing.HasCapability(protocol.FeatureCentralDirectory) {
		t.Errorf("published listing lacks the directory capability: %v", dir[0].Listing.Capabilities)
	}
	if st := b.CentralStatus(); st.DirectoryCount != 1 || st.DirectoryFetched.IsZero() {
		t.Errorf("status after fetch: %+v", st)
	}

	// Withdraw, then the cache still serves the old copy until forced or
	// a listing event stales it.
	if err := a.withdrawCentralListing(context.Background()); err != nil {
		t.Fatalf("withdraw: %v", err)
	}
	if !a.CentralStatus().Withdrawn {
		t.Error("status does not show withdrawn")
	}
	if dir, _ := b.FetchCentralDirectory(context.Background(), false); len(dir) != 1 {
		t.Errorf("cached directory should still hold the listing, got %d", len(dir))
	}
	if dir, _ := b.FetchCentralDirectory(context.Background(), true); len(dir) != 0 {
		t.Errorf("forced fetch after withdrawal: %+v", dir)
	}
}

func TestCentralFetchRefusedWhenDisabled(t *testing.T) {
	ts, _, _ := newTestHub(t, true)
	l := subscribedLeaf(t, ts, "gamma")
	if err := l.fetchHubFeatures(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := l.FetchCentralDirectory(context.Background(), false); !errors.Is(err, ErrCentralDisabled) {
		t.Errorf("want ErrCentralDisabled, got %v", err)
	}
}

// A hub from before Central names no features: the leaf treats the
// directory as unsupported, publishes nothing and withdraws nothing.
func TestCentralUnsupportedOnOlderHub(t *testing.T) {
	var central, info int
	old := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v3net/v1/testnet/info":
			info++
			_ = json.NewEncoder(w).Encode(protocol.NetworkInfo{Name: "testnet", HubNodeID: "0000000000000000"})
		case "/v3net/v1/testnet/central/listing", "/v3net/v1/testnet/central/listings":
			central++
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	defer old.Close()

	l, _ := setupLeaf(t, old.URL, &mockJAMWriter{})
	l.cfg.CentralEnabled = true
	l.cfg.CentralListing = &CentralListingSpec{Name: "Delta"}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := make(chan struct{})
	go func() {
		l.runCentral(ctx)
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("runCentral did not return on an unsupported hub")
	}
	if supported, known := l.HubSupports(protocol.FeatureCentralDirectory); supported || !known {
		t.Errorf("support: supported=%v known=%v", supported, known)
	}
	if central != 0 {
		t.Errorf("leaf called a Central endpoint %d times on a hub that serves none", central)
	}
	if _, err := l.FetchCentralDirectory(context.Background(), false); !errors.Is(err, ErrCentralUnsupported) {
		t.Errorf("want ErrCentralUnsupported, got %v", err)
	}
	st := l.CentralStatus()
	if st.Supported || !st.SupportKnown || !st.Enabled || !st.Publishing || !st.LastPublished.IsZero() {
		t.Errorf("status on older hub: %+v", st)
	}
}

// runCentral with publication off withdraws once and returns; with it on
// it publishes and keeps the listing fresh.
func TestRunCentralPublishesOrWithdraws(t *testing.T) {
	ts, h, _ := newTestHub(t, true)
	a := centralLeaf(t, ts, "alpha", "Alpha BBS")
	b := centralLeaf(t, ts, "beta", "")

	// Seed a listing for b as if an earlier run published it.
	b.cfg.CentralListing = &CentralListingSpec{Name: "Beta BBS"}
	if err := b.publishCentralListing(context.Background()); err != nil {
		t.Fatal(err)
	}
	b.cfg.CentralListing = nil

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go a.runCentral(ctx)
	bDone := make(chan struct{})
	go func() { b.runCentral(ctx); close(bDone) }()

	select {
	case <-bDone:
	case <-time.After(3 * time.Second):
		t.Fatal("withdrawing run did not return")
	}
	deadline := time.Now().Add(3 * time.Second)
	for a.CentralStatus().LastPublished.IsZero() && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	rows, err := h.Central().List("testnet", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].NodeID != a.cfg.Keystore.NodeID() {
		t.Errorf("hub holds %d listings after a publishes and b withdraws: %+v", len(rows), rows)
	}
	if !b.CentralStatus().Withdrawn {
		t.Error("b should report its listing withdrawn")
	}
}

// A hub that alters a listing, or lists a board under the wrong node, is
// caught by the leaf's own checks: only listings that verify are shown.
func TestCentralDirectoryDropsTamperedListings(t *testing.T) {
	good, _, err := keystore.Load(filepath.Join(t.TempDir(), "good.key"))
	if err != nil {
		t.Fatal(err)
	}
	mk := func(name string) protocol.CentralListing {
		now := time.Now().UTC()
		l := protocol.CentralListing{
			V3NetCentral: protocol.CentralListingVersion, Network: "testnet", Seq: now.UnixNano(), Name: name,
			PublishedAt: now.Format(time.RFC3339), ExpiresAt: now.Add(time.Hour).Format(time.RFC3339),
		}
		if err := l.Sign(good); err != nil {
			t.Fatal(err)
		}
		return l
	}
	honest := mk("Honest")
	altered := mk("Altered")
	altered.Description = "the hub wrote this"
	expired := mk("Expired")
	expired.ExpiresAt = time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
	_ = expired.Sign(good)
	wrongNet := mk("Wrong net")
	wrongNet.Network = "othernet"
	_ = wrongNet.Sign(good)
	dup := mk("Duplicate of honest")

	dir := protocol.CentralDirectory{Network: "testnet", GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Listings: []protocol.CentralDirectoryEntry{{Listing: honest, Online: true}, {Listing: altered}, {Listing: expired}, {Listing: wrongNet}, {Listing: dup}}}

	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v3net/v1/testnet/central/listings" {
			_ = json.NewEncoder(w).Encode(dir)
			return
		}
		http.NotFound(w, r)
	}))
	defer fake.Close()

	l, _ := setupLeaf(t, fake.URL, &mockJAMWriter{})
	l.cfg.CentralEnabled = true
	l.noteHubFeatures([]string{protocol.FeatureCentralDirectory})
	got, err := l.FetchCentralDirectory(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Listing.Name != "Honest" || !got[0].Online {
		t.Errorf("want only the honest listing, got %+v", got)
	}
}

// A listing event marks the cache stale; a replay of it, or an expired
// one, is ignored.
func TestCentralEventDedupAndExpiry(t *testing.T) {
	ts, _, _ := newTestHub(t, true)
	l := centralLeaf(t, ts, "alpha", "")
	if _, err := l.FetchCentralDirectory(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	stale := func() bool {
		l.central.mu.Lock()
		defer l.central.mu.Unlock()
		return l.central.dirStale
	}
	clear := func() {
		l.central.mu.Lock()
		l.central.dirStale = false
		l.central.mu.Unlock()
	}

	meta, _ := protocol.NewEventMeta(protocol.CentralEventTTL)
	ev, _ := protocol.NewEvent(protocol.EventCentralListingUpdated, protocol.CentralListingEventPayload{EventMeta: meta, Network: "testnet", NodeID: "x", Seq: 1})
	l.dispatchCentralEvent(ev)
	if !stale() {
		t.Fatal("fresh event did not stale the cache")
	}
	clear()
	l.dispatchCentralEvent(ev)
	if stale() {
		t.Error("replayed event staled the cache again")
	}

	old := meta
	old.EventID = "another"
	old.ExpiresAt = time.Now().Add(-time.Second).UTC().Format(time.RFC3339)
	ev, _ = protocol.NewEvent(protocol.EventCentralListingWithdrawn, protocol.CentralListingEventPayload{EventMeta: old, Network: "testnet", NodeID: "x"})
	l.dispatchCentralEvent(ev)
	if stale() {
		t.Error("expired event staled the cache")
	}

	other := meta
	other.EventID = "elsewhere"
	ev, _ = protocol.NewEvent(protocol.EventCentralListingWithdrawn, protocol.CentralListingEventPayload{EventMeta: other, Network: "othernet", NodeID: "x"})
	l.dispatchCentralEvent(ev)
	if stale() {
		t.Error("event for another network staled the cache")
	}

	// An event without metadata (an older hub) is always acted on.
	ev, _ = protocol.NewEvent(protocol.EventCentralListingUpdated, protocol.CentralListingEventPayload{Network: "testnet", NodeID: "x"})
	l.dispatchCentralEvent(ev)
	if !stale() {
		t.Error("event without metadata should stale the cache")
	}

	// The seen ring is bounded.
	for i := 0; i < centralSeenEvents*2; i++ {
		m, _ := protocol.NewEventMeta(protocol.CentralEventTTL)
		ev, _ := protocol.NewEvent(protocol.EventCentralListingUpdated, protocol.CentralListingEventPayload{EventMeta: m, Network: "testnet"})
		l.dispatchCentralEvent(ev)
	}
	l.central.mu.Lock()
	n := len(l.central.seen)
	l.central.mu.Unlock()
	if n != centralSeenEvents {
		t.Errorf("seen ring holds %d ids, want %d", n, centralSeenEvents)
	}
}

// Over the real stream, a publish on one leaf stales the directory on
// another, so its next look fetches the new listing.
func TestCentralEventOverSSEStalesDirectory(t *testing.T) {
	ts, _, _ := newTestHub(t, true)
	a := centralLeaf(t, ts, "alpha", "Alpha BBS")
	b := centralLeaf(t, ts, "beta", "")
	events := watchEvents(t, b)
	if !b.SSEConnected() {
		t.Error("SSEConnected false while the stream is up")
	}
	if dir, _ := b.FetchCentralDirectory(context.Background(), true); len(dir) != 0 {
		t.Fatalf("directory before publish: %+v", dir)
	}
	if err := a.publishCentralListing(context.Background()); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(5 * time.Second)
	for {
		select {
		case ev := <-events:
			if ev.Type == protocol.EventCentralListingUpdated {
				goto got
			}
		case <-deadline:
			t.Fatal("no central_listing_updated over SSE")
		}
	}
got:
	// The cache was fresh a moment ago; the event must have staled it.
	dir, err := b.FetchCentralDirectory(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(dir) != 1 || dir[0].Listing.Name != "Alpha BBS" {
		t.Errorf("directory after event: %+v", dir)
	}
}

func TestBuildCentralListingCleansText(t *testing.T) {
	ts, _, _ := newTestHub(t, true)
	l := subscribedLeaf(t, ts, "alpha")
	l.cfg.CentralListing = &CentralListingSpec{
		Name:        "|04Red|07 Board\x01",
		Description: "fine",
		Addresses:   []string{"ssh://alpha.example:2222"},
		Software:    "ViSiON/3",
	}
	got, err := l.buildCentralListing(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "/04Red/07 Board " {
		t.Errorf("name not cleaned: %q", got.Name)
	}
	if err := got.Validate("testnet"); err != nil {
		t.Errorf("built listing invalid: %v", err)
	}
	l.cfg.CentralListing.Addresses = []string{"gopher://nope"}
	if _, err := l.buildCentralListing(time.Now()); err == nil {
		t.Error("bad address should refuse to build")
	}
}
