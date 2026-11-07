package leaf

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

type staticActivity struct {
	presence, calls []protocol.CentralActivityEntry
}

func (s staticActivity) CentralPresence() []protocol.CentralActivityEntry { return s.presence }
func (s staticActivity) CentralLastCalls(time.Time) []protocol.CentralActivityEntry {
	return s.calls
}

// On a hub from before phase 2, a leaf with both features on calls none
// of their endpoints, offers neither, and returns: message networking is
// left as it was.
func TestCentralActivityUnsupportedOnOlderHub(t *testing.T) {
	var activity atomic.Int32
	old := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v3net/v1/testnet/info":
			// A phase 1 hub: the directory only.
			_ = json.NewEncoder(w).Encode(protocol.NetworkInfo{Name: "testnet", HubNodeID: "0000000000000000", Features: []string{protocol.FeatureCentralDirectory}})
		case strings.HasSuffix(r.URL.Path, "/central/presence"), strings.HasSuffix(r.URL.Path, "/central/lastcallers"):
			activity.Add(1)
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	defer old.Close()

	l, _ := setupLeaf(t, old.URL, &mockJAMWriter{})
	l.cfg.CentralEnabled, l.cfg.CentralPresence, l.cfg.CentralLastCallers = true, true, true
	l.cfg.Activity = staticActivity{presence: []protocol.CentralActivityEntry{{Handle: "Alice", Session: "aliceSession1"}}}
	if err := l.fetchHubFeatures(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	done := make(chan struct{})
	go func() {
		l.runCentralActivity(ctx)
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("runCentralActivity did not return on a hub without the features")
	}
	if n := activity.Load(); n != 0 {
		t.Errorf("leaf called an activity endpoint %d times on a hub that serves none", n)
	}
	for _, kind := range activityKinds {
		if _, err := l.FetchCentralActivity(context.Background(), kind, true); !errors.Is(err, ErrCentralUnsupported) {
			t.Errorf("%s: want ErrCentralUnsupported, got %v", kind, err)
		}
		if st := l.ActivityStatus(kind); st.Supported || !st.Enabled || !st.LastPublished.IsZero() {
			t.Errorf("%s status: %+v", kind, st)
		}
	}
}

// The leaf cleans and caps what the source gives it, and never publishes
// a call past the retention limit; a last-callers snapshot ends when its
// oldest call would pass the limit, and is refreshed just before.
func TestBuildActivityCleansCapsAndBoundsRetention(t *testing.T) {
	l, _ := setupLeaf(t, "http://unused.invalid", &mockJAMWriter{})
	l.cfg.Network = "testnet"
	l.cfg.BBSName = "My |12Board"
	now := time.Now().UTC().Truncate(time.Second)
	var sessions []protocol.CentralActivityEntry
	for i := 0; i < protocol.MaxCentralPresenceEntries+5; i++ {
		sessions = append(sessions, protocol.CentralActivityEntry{Handle: "Caller", Session: "session" + string(rune('A'+i%26)) + string(rune('a'+i/26)) + "xx"})
	}
	sessions = append([]protocol.CentralActivityEntry{
		{Handle: "Al|ice\x1b[2J", Session: "aliceSession1", Availability: "busy"},
		{Handle: "Dupe", Session: "aliceSession1"},
		{Handle: "Odd", Session: "oddSession01", Availability: "napping"},
		{Handle: "   ", Session: "blankSession"},
	}, sessions...)
	l.cfg.Activity = staticActivity{
		presence: sessions,
		calls: []protocol.CentralActivityEntry{
			{Handle: "Old", At: now.Add(-25 * time.Hour).Format(time.RFC3339)},
			{Handle: "Edge", At: now.Add(-20 * time.Hour).Format(time.RFC3339)},
			{Handle: "Recent", At: now.Add(-time.Hour).Format(time.RFC3339), Until: now.Add(-50 * time.Minute).Format(time.RFC3339)},
			{Handle: "Bad", At: "yesterday"},
		},
	}

	a, refresh, err := l.buildActivity(protocol.CentralKindPresence, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Entries) != protocol.MaxCentralPresenceEntries {
		t.Errorf("presence entries: %d, want the cap %d", len(a.Entries), protocol.MaxCentralPresenceEntries)
	}
	if e := a.Entries[0]; e.Handle != "Al/ice [2J" || e.Availability != protocol.CentralBusy {
		t.Errorf("first entry not cleaned: %+v", e)
	}
	if e := a.Entries[1]; e.Handle != "Odd" || e.Availability != "" {
		t.Errorf("duplicate session kept, or unknown availability published: %+v", e)
	}
	if a.BBS != "My /12Board" || refresh != centralPresenceHeartbeat {
		t.Errorf("bbs %q refresh %s", a.BBS, refresh)
	}
	if err := a.Verify(); err != nil {
		t.Errorf("verify: %v", err)
	}

	a, refresh, err = l.buildActivity(protocol.CentralKindLastCallers, now)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range a.Entries {
		names = append(names, e.Handle)
	}
	if strings.Join(names, ",") != "Recent,Edge" {
		t.Errorf("calls %v, want newest first without the expired and unparseable ones", names)
	}
	// Edge began 20h ago: the snapshot ends 4h from now, and is refreshed
	// shortly before that.
	if want := now.Add(4 * time.Hour).Format(time.RFC3339); a.ExpiresAt != want {
		t.Errorf("expires %s, want %s", a.ExpiresAt, want)
	}
	if refresh != centralLastCallsHeartbeat {
		t.Errorf("refresh %s, want the heartbeat (the oldest call is hours from the limit)", refresh)
	}
	edgeOnly := staticActivity{calls: []protocol.CentralActivityEntry{{Handle: "Edge", At: now.Add(-centralLastCallsRetention + 5*time.Minute).Format(time.RFC3339)}}}
	l.cfg.Activity = edgeOnly
	if _, refresh, _ = l.buildActivity(protocol.CentralKindLastCallers, now); refresh != 5*time.Minute {
		t.Errorf("refresh %s, want 5m: republish when the oldest call reaches the limit", refresh)
	}
}

// Seq rises on every snapshot, even two built in the same nanosecond.
func TestBuildActivitySeqRises(t *testing.T) {
	l, _ := setupLeaf(t, "http://unused.invalid", &mockJAMWriter{})
	l.cfg.Network, l.cfg.BBSName = "testnet", "Board"
	l.cfg.Activity = staticActivity{}
	a1, _, _ := l.buildActivity(protocol.CentralKindPresence, time.Now())
	l.central.activity[protocol.CentralKindPresence].lastSeq = a1.Seq + 1e12
	a2, _, _ := l.buildActivity(protocol.CentralKindPresence, time.Now())
	if a2.Seq <= a1.Seq+1e12 {
		t.Errorf("seq did not rise: %d then %d", a1.Seq, a2.Seq)
	}
}

// An activity event stales only its own kind's cache, once.
func TestActivityEventStalesItsKind(t *testing.T) {
	l, _ := setupLeaf(t, "http://unused.invalid", &mockJAMWriter{})
	l.cfg.Network = "testnet"
	staleOf := func(kind string) bool {
		l.central.mu.Lock()
		defer l.central.mu.Unlock()
		return l.central.activity[kind].stale
	}
	meta, _ := protocol.NewEventMeta(protocol.CentralEventTTL)
	ev, _ := protocol.NewEvent(protocol.EventCentralActivityUpdated, protocol.CentralActivityEventPayload{EventMeta: meta, Network: "testnet", NodeID: "x", Kind: protocol.CentralKindPresence})
	l.dispatchCentralEvent(ev)
	if !staleOf(protocol.CentralKindPresence) || staleOf(protocol.CentralKindLastCallers) {
		t.Fatal("event did not stale exactly its kind")
	}
	l.central.mu.Lock()
	l.central.activity[protocol.CentralKindPresence].stale = false
	l.central.mu.Unlock()
	l.dispatchCentralEvent(ev)
	if staleOf(protocol.CentralKindPresence) {
		t.Error("replayed activity event acted on")
	}
	m2, _ := protocol.NewEventMeta(protocol.CentralEventTTL)
	ev, _ = protocol.NewEvent(protocol.EventCentralActivityUpdated, protocol.CentralActivityEventPayload{EventMeta: m2, Network: "testnet", Kind: "wall"})
	l.dispatchCentralEvent(ev) // an unknown kind is ignored, not a panic
}

// WithdrawActivity is final: the hub holds nothing afterwards, and the
// leaf refuses to publish the kind again, so a heartbeat or a change
// notice racing a subscription's removal cannot put it back.
func TestWithdrawActivityHaltsPublishing(t *testing.T) {
	ts, h, _ := newTestHub(t, true)
	l := centralLeaf(t, ts, "alpha", "")
	l.cfg.CentralLastCallers = true
	l.cfg.Activity = staticActivity{calls: []protocol.CentralActivityEntry{{Handle: "Alice", At: time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)}}}
	kind := protocol.CentralKindLastCallers
	if _, err := l.publishActivity(context.Background(), kind); err != nil {
		t.Fatal(err)
	}
	if err := l.WithdrawActivity(context.Background(), kind); err != nil {
		t.Fatal(err)
	}
	if _, err := l.publishActivity(context.Background(), kind); !errors.Is(err, errActivityHalted) {
		t.Errorf("publish after withdrawal: %v, want errActivityHalted", err)
	}
	if rows, _ := h.Activity().List("testnet", kind, time.Now()); len(rows) != 0 {
		t.Errorf("hub still holds %d snapshots", len(rows))
	}
}

// A cached feed is filtered for expiry on the way out: a snapshot or call
// that passed its limit while cached is not served.
func TestLiveActivityFeedDropsExpired(t *testing.T) {
	now := time.Now().UTC()
	feed := []protocol.CentralActivityBoard{
		{Activity: protocol.CentralActivity{NodeID: "a", ExpiresAt: now.Add(-time.Second).Format(time.RFC3339)}},
		{Activity: protocol.CentralActivity{NodeID: "b", ExpiresAt: now.Add(time.Hour).Format(time.RFC3339), Entries: []protocol.CentralActivityEntry{
			{Handle: "Old", At: now.Add(-24*time.Hour - time.Minute).Format(time.RFC3339)},
			{Handle: "New", At: now.Add(-time.Hour).Format(time.RFC3339)},
		}}},
	}
	out := liveActivityFeed(feed, protocol.CentralKindLastCallers, now)
	if len(out) != 1 || out[0].Activity.NodeID != "b" || len(out[0].Activity.Entries) != 1 || out[0].Activity.Entries[0].Handle != "New" {
		t.Errorf("live feed %+v", out)
	}
	if len(feed[1].Activity.Entries) != 2 {
		t.Error("the cache itself was changed")
	}
}
