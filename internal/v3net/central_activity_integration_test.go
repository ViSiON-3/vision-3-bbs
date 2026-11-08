package v3net_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/dedup"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/hub"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/keystore"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/leaf"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// fakeActivity is a board's leaf.CentralActivitySource: the consenting
// sessions and calls it reports, changed by the test as callers come,
// go and change their minds.
type fakeActivity struct {
	mu       sync.Mutex
	presence []protocol.CentralActivityEntry
	calls    []protocol.CentralActivityEntry
}

// CentralPresence returns the scripted sessions.
func (f *fakeActivity) CentralPresence(bool) []protocol.CentralActivityEntry {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.presence)
}

// CentralLastCalls returns the scripted calls that began after since.
func (f *fakeActivity) CentralLastCalls(since time.Time, _ bool) []protocol.CentralActivityEntry {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []protocol.CentralActivityEntry
	for _, c := range f.calls {
		if c.Began().After(since) {
			out = append(out, c)
		}
	}
	return out
}

// set replaces what the board reports, as callers come, go and change
// their minds.
func (f *fakeActivity) set(presence, calls []protocol.CentralActivityEntry) {
	f.mu.Lock()
	f.presence, f.calls = presence, calls
	f.mu.Unlock()
}

// online returns a presence entry for handle's session.
func online(handle, session string) protocol.CentralActivityEntry {
	return protocol.CentralActivityEntry{Handle: handle, Session: session}
}

// called returns a ten-minute call by handle that began ago.
func called(handle string, ago time.Duration) protocol.CentralActivityEntry {
	began := time.Now().UTC().Add(-ago)
	return protocol.CentralActivityEntry{Handle: handle, At: began.Format(time.RFC3339), Until: began.Add(10 * time.Minute).Format(time.RFC3339)}
}

// activityBoard is one board's keystore and source; start runs a leaf for
// it until the returned stop is called, so a board can disconnect and
// come back as itself.
type activityBoard struct {
	name     string
	ks       *keystore.Keystore
	src      *fakeActivity
	presence bool
	calls    bool
}

// newActivityBoard returns a board with a fresh key and source, offering
// presence and calls as given.
func newActivityBoard(t *testing.T, name string, presence, calls bool) *activityBoard {
	t.Helper()
	ks, _, err := keystore.Load(filepath.Join(t.TempDir(), "leaf.key"))
	if err != nil {
		t.Fatal(err)
	}
	return &activityBoard{name: name, ks: ks, src: &fakeActivity{}, presence: presence, calls: calls}
}

// start runs a leaf for the board against ts, returning it and a stop
// func that is safe to call more than once.
func (b *activityBoard) start(t *testing.T, ts *httptest.Server) (*leaf.Leaf, func()) {
	t.Helper()
	ix, err := dedup.Open(filepath.Join(t.TempDir(), "dedup.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	l := leaf.New(leaf.Config{
		HubURL: ts.URL, Network: "testnet", PollInterval: time.Hour,
		Keystore: b.ks, DedupIndex: ix, JAMWriter: &testJAMWriter{},
		BBSName: b.name, BBSHost: b.name + ".example",
		CentralEnabled: true, CentralPresence: b.presence, CentralLastCallers: b.calls,
		Activity: b.src,
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		l.Start(ctx)
		close(done)
	}()
	var once sync.Once
	stop := func() {
		once.Do(func() {
			cancel()
			<-done
			l.Close()
			ix.Close()
		})
	}
	t.Cleanup(stop)
	return l, stop
}

// feedHandles returns "Board/Handle" for every entry l sees in kind.
func feedHandles(l *leaf.Leaf, kind string) ([]string, error) {
	boards, err := l.FetchCentralActivity(context.Background(), kind, true)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, b := range boards {
		for _, e := range b.Activity.Entries {
			out = append(out, b.Activity.BBS+"/"+e.Handle)
		}
	}
	slices.Sort(out)
	return out, nil
}

// waitFeed polls l's feed of kind until it is exactly want.
func waitFeed(t *testing.T, l *leaf.Leaf, kind string, want ...string) {
	t.Helper()
	slices.Sort(want)
	if want == nil {
		want = []string{}
	}
	deadline := time.Now().Add(12 * time.Second)
	for {
		got, err := feedHandles(l, kind)
		if got == nil {
			got = []string{}
		}
		if err == nil && slices.Equal(got, want) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s feed never became %v: last %v (err %v)", kind, want, got, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// newActivityHub starts a hub serving testnet and its test server.
func newActivityHub(t *testing.T) (*hub.Hub, *httptest.Server) {
	t.Helper()
	dir := t.TempDir()
	hubKS, _, err := keystore.Load(filepath.Join(dir, "hub.key"))
	if err != nil {
		t.Fatal(err)
	}
	h, err := hub.New(hub.Config{
		ListenAddr: ":0", DataDir: dir, Keystore: hubKS, AutoApprove: true, RequireSignedSubscribe: true,
		Networks: []hub.NetworkConfig{{Name: "testnet"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { h.Close() })
	ts := httptest.NewServer(h.Mux())
	t.Cleanup(ts.Close)
	return h, ts
}

// Three boards on one hub. Alpha and Beta offer both features; Gamma's
// sysop offers neither, so though callers there have opted in (its source
// reports them), nothing of Gamma's is ever published. Callers who opt in
// show on every board; one who withdraws is gone from both feeds,
// including calls published before; a board that drops off the network
// takes its callers off Who's Online and puts them back when it returns;
// and a captured snapshot cannot be replayed to restore what was withdrawn.
func TestCentralActivityAcrossBoards(t *testing.T) {
	h, ts := newActivityHub(t)

	alpha := newActivityBoard(t, "Alpha", true, true)
	beta := newActivityBoard(t, "Beta", true, true)
	gamma := newActivityBoard(t, "Gamma", false, false)
	alpha.src.set(
		[]protocol.CentralActivityEntry{online("Alice", "aliceSession1"), online("Alice", "aliceSession2")},
		[]protocol.CentralActivityEntry{called("Alice", 2*time.Hour), called("Ann", time.Hour)})
	beta.src.set([]protocol.CentralActivityEntry{online("Bob", "bobSession01")}, nil)
	gamma.src.set(
		[]protocol.CentralActivityEntry{online("Gail", "gailSession1")},
		[]protocol.CentralActivityEntry{called("Gail", time.Hour)})

	la, _ := alpha.start(t, ts)
	lb, stopB := beta.start(t, ts)
	lg, _ := gamma.start(t, ts)

	// Alice has two sessions: two entries, one per session. Gamma's sysop
	// did not offer the features: Gail never appears, and Gamma cannot
	// read the feeds either.
	waitFeed(t, la, protocol.CentralKindPresence, "Alpha/Alice", "Alpha/Alice", "Beta/Bob")
	waitFeed(t, lb, protocol.CentralKindPresence, "Alpha/Alice", "Alpha/Alice", "Beta/Bob")
	waitFeed(t, lb, protocol.CentralKindLastCallers, "Alpha/Alice", "Alpha/Ann")
	if _, err := lg.FetchCentralActivity(context.Background(), protocol.CentralKindPresence, true); err == nil {
		t.Error("a board whose sysop did not offer Who's Online could read it")
	}
	deadline := time.Now().Add(8 * time.Second)
	for !lg.ActivityStatus(protocol.CentralKindPresence).Withdrawn || !lg.ActivityStatus(protocol.CentralKindLastCallers).Withdrawn {
		if time.Now().After(deadline) {
			t.Fatalf("gamma never withdrew: %+v %+v", lg.ActivityStatus(protocol.CentralKindPresence), lg.ActivityStatus(protocol.CentralKindLastCallers))
		}
		time.Sleep(50 * time.Millisecond)
	}
	for _, kind := range []string{protocol.CentralKindPresence, protocol.CentralKindLastCallers} {
		if st := lg.ActivityStatus(kind); !st.LastPublished.IsZero() {
			t.Errorf("gamma published %s: %+v", kind, st)
		}
	}

	// Capture Alpha's last-callers snapshot as stored, for the replay.
	rows, err := h.Activity().List("testnet", protocol.CentralKindLastCallers, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var captured json.RawMessage
	for _, r := range rows {
		if r.NodeID == alpha.ks.NodeID() {
			captured = r.Raw
		}
	}
	if captured == nil {
		t.Fatalf("alpha's last callers not stored: %d rows", len(rows))
	}

	// Alice ends one session: one entry left, she is still online.
	alpha.src.set(
		[]protocol.CentralActivityEntry{online("Alice", "aliceSession2")},
		[]protocol.CentralActivityEntry{called("Alice", 2*time.Hour), called("Ann", time.Hour)})
	la.NotifyCentralActivity()
	waitFeed(t, lb, protocol.CentralKindPresence, "Alpha/Alice", "Beta/Bob")

	// Alice withdraws from both. Her source entries go, and so does the
	// call she made before she withdrew.
	alpha.src.set(nil, []protocol.CentralActivityEntry{called("Ann", time.Hour)})
	la.NotifyCentralActivity()
	waitFeed(t, lb, protocol.CentralKindPresence, "Beta/Bob")
	waitFeed(t, lb, protocol.CentralKindLastCallers, "Alpha/Ann")

	// Replaying the captured snapshot, which still had Alice's call, is
	// refused, by the hub's sequence watermark.
	var replay protocol.CentralActivity
	if err := json.Unmarshal(captured, &replay); err != nil {
		t.Fatal(err)
	}
	if err := h.Activity().Put(&replay, captured); err == nil {
		t.Error("the hub stored a replayed older snapshot")
	}
	waitFeed(t, lb, protocol.CentralKindLastCallers, "Alpha/Ann")

	// Beta drops off the network: Bob is off Who's Online at once, as
	// seen from Alpha, without waiting for his snapshot to expire. His
	// board's last calls (none) are unaffected.
	stopB()
	waitFeed(t, la, protocol.CentralKindPresence)
	// Beta comes back as itself: Bob is shown again.
	lb2, _ := beta.start(t, ts)
	waitFeed(t, la, protocol.CentralKindPresence, "Beta/Bob")
	waitFeed(t, lb2, protocol.CentralKindPresence, "Beta/Bob")
}

// A call older than the retention limit is never published, and a board
// offering only Last Callers publishes no presence.
func TestCentralLastCallsRetention(t *testing.T) {
	_, ts := newActivityHub(t)
	alpha := newActivityBoard(t, "Alpha", false, true)
	alpha.src.set([]protocol.CentralActivityEntry{online("Alice", "aliceSession1")},
		[]protocol.CentralActivityEntry{called("Old", 25*time.Hour), called("New", time.Hour)})
	viewer := newActivityBoard(t, "Viewer", true, true)
	la, _ := alpha.start(t, ts)
	lv, _ := viewer.start(t, ts)
	waitFeed(t, lv, protocol.CentralKindLastCallers, "Alpha/New")
	waitFeed(t, lv, protocol.CentralKindPresence)
	if st := la.ActivityStatus(protocol.CentralKindPresence); !st.LastPublished.IsZero() || st.Enabled {
		t.Errorf("presence published by a board that does not offer it: %+v", st)
	}
}

// Turning a feature off on reload withdraws it from the hub before the
// old leaf stops.
func TestReloadWithdrawsActivityWhenTurnedOff(t *testing.T) {
	h, ts := newActivityHub(t)
	svc, mm := newReloadFixture(t, "ALPHA")
	svc.BBSName = "Reloader"
	src := &fakeActivity{}
	src.set(nil, []protocol.CentralActivityEntry{called("Alice", time.Hour)})
	svc.SetCentralActivitySource(src)
	on := config.V3NetLeafConfig{HubURL: ts.URL, Network: "testnet", Boards: []string{"ALPHA"},
		Central: &config.V3NetCentralConfig{Enabled: true, LastCallers: true}}
	svc.ConfigureLeaves([]config.V3NetLeafConfig{on}, mm)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go svc.Start(ctx)

	deadline := time.Now().Add(10 * time.Second)
	for {
		rows, _ := h.Activity().List("testnet", protocol.CentralKindLastCallers, time.Now())
		if len(rows) == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("last callers never published")
		}
		time.Sleep(50 * time.Millisecond)
	}
	off := on
	off.Central = &config.V3NetCentralConfig{Enabled: true}
	if err := svc.ReloadLeaves([]config.V3NetLeafConfig{off}, mm, nil); err != nil {
		t.Fatal(err)
	}
	if rows, _ := h.Activity().List("testnet", protocol.CentralKindLastCallers, time.Now()); len(rows) != 0 {
		t.Errorf("last callers still held after reload turned them off: %d rows", len(rows))
	}
}
