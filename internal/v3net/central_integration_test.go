package v3net_test

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/dedup"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/hub"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/keystore"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/leaf"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// centralBoard starts a full leaf (subscribe, poll, SSE, Central) named
// name against ts, publishing a listing when listed is set.
func centralBoard(t *testing.T, ctx context.Context, ts *httptest.Server, name string, listed bool) *leaf.Leaf {
	t.Helper()
	dir := t.TempDir()
	ks, _, err := keystore.Load(filepath.Join(dir, "leaf.key"))
	if err != nil {
		t.Fatal(err)
	}
	ix, err := dedup.Open(filepath.Join(dir, "dedup.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ix.Close() })
	cfg := leaf.Config{
		HubURL:         ts.URL,
		Network:        "testnet",
		PollInterval:   time.Hour,
		Keystore:       ks,
		DedupIndex:     ix,
		JAMWriter:      &testJAMWriter{},
		BBSName:        name,
		BBSHost:        name + ".example",
		CentralEnabled: true,
	}
	if listed {
		cfg.CentralListing = &leaf.CentralListingSpec{
			Name:        name,
			Description: "Board " + name,
			Addresses:   []string{"ssh://" + name + ".example:2222"},
		}
	}
	l := leaf.New(cfg)
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		l.Start(ctx)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
		l.Close()
	})
	return l
}

// waitStatus polls l's Central status until ok accepts it or the deadline
// passes.
func waitStatus(t *testing.T, l *leaf.Leaf, what string, ok func(leaf.CentralStatus) bool) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for !ok(l.CentralStatus()) {
		if time.Now().After(deadline) {
			t.Fatalf("%s never happened: %+v", what, l.CentralStatus())
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// waitDirectory polls l's directory until want names are listed or the
// deadline passes, returning the last directory seen.
func waitDirectory(t *testing.T, l *leaf.Leaf, want ...string) []protocol.CentralDirectoryEntry {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for {
		dir, err := l.FetchCentralDirectory(context.Background(), true)
		if err == nil {
			names := map[string]bool{}
			for _, e := range dir {
				names[e.Listing.Name] = true
			}
			ok := len(dir) == len(want)
			for _, w := range want {
				ok = ok && names[w]
			}
			if ok {
				return dir
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("directory never reached %v: last %v (err %v)", want, dir, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// Two opted-in boards publish, discover and update each other's listings;
// a third that only browses is never listed; withdrawal removes a listing;
// a board that reconnects is listed as online again.
func TestCentralTwoBoardsDiscoverEachOther(t *testing.T) {
	dir := t.TempDir()
	hubKS, _, err := keystore.Load(filepath.Join(dir, "hub.key"))
	if err != nil {
		t.Fatal(err)
	}
	h, err := hub.New(hub.Config{
		ListenAddr: ":0", DataDir: dir, Keystore: hubKS, AutoApprove: true, RequireSignedSubscribe: true,
		Networks: []hub.NetworkConfig{{Name: "testnet", Description: "Central test"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { h.Close() })
	ts := httptest.NewServer(h.Mux())
	t.Cleanup(ts.Close)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	alpha := centralBoard(t, ctx, ts, "alpha", true)
	beta := centralBoard(t, ctx, ts, "beta", true)
	browserCtx, browserCancel := context.WithCancel(ctx)
	browser := centralBoard(t, browserCtx, ts, "browser", false)

	// Each listed board sees the other and itself; the browser sees both
	// and is not listed.
	got := waitDirectory(t, alpha, "alpha", "beta")
	for _, e := range got {
		if !e.Online {
			t.Errorf("%s has a stream open but is not online", e.Listing.Name)
		}
		if e.Listing.Description != "Board "+e.Listing.Name {
			t.Errorf("%s description %q", e.Listing.Name, e.Listing.Description)
		}
	}
	waitDirectory(t, beta, "alpha", "beta")
	waitDirectory(t, browser, "alpha", "beta")
	waitStatus(t, browser, "browser withdrawal", func(st leaf.CentralStatus) bool { return st.Withdrawn })
	if st := browser.CentralStatus(); !st.LastPublished.IsZero() {
		t.Errorf("browsing board published: %+v", st)
	}

	// Beta withdraws: alpha's next fetch no longer lists it.
	if err := beta.WithdrawCentralListing(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitDirectory(t, alpha, "alpha")

	// The browser disconnects and is never listed; alpha keeps its own row.
	browserCancel()
	waitDirectory(t, alpha, "alpha")
	if st := alpha.CentralStatus(); !st.Connected || !st.Supported || st.LastError != "" {
		t.Errorf("alpha status: %+v", st)
	}
}
