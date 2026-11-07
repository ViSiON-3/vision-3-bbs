package hub

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/keystore"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// activityFor builds and signs ks's snapshot of kind on testnet with one
// entry per handle.
func activityFor(t *testing.T, ks *keystore.Keystore, kind string, handles ...string) *protocol.CentralActivity {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Second)
	a := &protocol.CentralActivity{
		V3NetCentral: protocol.CentralActivityVersion, Kind: kind, Network: "testnet",
		Seq: time.Now().UnixNano(), BBS: "Board " + ks.NodeID()[:4], Entries: []protocol.CentralActivityEntry{},
		PublishedAt: now.Format(time.RFC3339), ExpiresAt: now.Add(3 * time.Minute).Format(time.RFC3339),
	}
	for i, h := range handles {
		e := protocol.CentralActivityEntry{Handle: h}
		if kind == protocol.CentralKindPresence {
			e.Session = "session" + string(rune('A'+i)) + "xxxxxx"
		} else {
			e.At = now.Add(-time.Hour).Format(time.RFC3339)
		}
		a.Entries = append(a.Entries, e)
	}
	if err := a.Sign(ks); err != nil {
		t.Fatal(err)
	}
	return a
}

func activityDo(t *testing.T, ts *httptest.Server, ks *keystore.Keystore, method, kind string, a *protocol.CentralActivity) (int, []byte) {
	t.Helper()
	body := ""
	if a != nil {
		b, _ := json.Marshal(a)
		body = string(b)
	}
	resp, err := http.DefaultClient.Do(signedRequest(t, ks, method, ts.URL+"/v3net/v1/testnet/central/"+kind, body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}

func getActivity(t *testing.T, ts *httptest.Server, ks *keystore.Keystore, kind string) protocol.CentralActivityFeed {
	t.Helper()
	code, body := activityDo(t, ts, ks, http.MethodGet, kind, nil)
	if code != http.StatusOK {
		t.Fatalf("GET %s: %d %s", kind, code, body)
	}
	var feed protocol.CentralActivityFeed
	if err := json.Unmarshal(body, &feed); err != nil {
		t.Fatal(err)
	}
	return feed
}

// handlesIn lists every handle in a feed.
func handlesIn(feed protocol.CentralActivityFeed) []string {
	var out []string
	for _, b := range feed.Boards {
		for _, e := range b.Activity.Entries {
			out = append(out, e.Handle)
		}
	}
	slices.Sort(out)
	return out
}

// openStream opens ks's event stream and waits for the hub to count it.
func openStream(t *testing.T, h *Hub, ts *httptest.Server, ks *keystore.Keystore) func() {
	t.Helper()
	resp, err := http.DefaultClient.Do(signedRequest(t, ks, http.MethodGet, ts.URL+"/v3net/v1/testnet/events", ""))
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("SSE: %v %v", err, resp)
	}
	waitFor(t, "stream counted", func() bool { return h.online.online(ks.NodeID(), "testnet") })
	return func() { resp.Body.Close() }
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("%s never happened", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestNetworkInfoAdvertisesActivityFeatures(t *testing.T) {
	for _, f := range []string{protocol.FeatureCentralPresence, protocol.FeatureCentralLastCallers} {
		if !slices.Contains(hubFeatures, f) {
			t.Errorf("hub does not advertise %s", f)
		}
	}
}

// Last callers: published, listed by every subscriber, replaced whole by
// a newer snapshot (a caller who withdrew is gone), withdrawn, and an
// older snapshot cannot be replayed over a newer one or a withdrawal.
func TestCentralLastCallersPublishReplaceWithdraw(t *testing.T) {
	h, _ := setupTestHub(t)
	ts := httptest.NewServer(h.newMux())
	defer ts.Close()
	a, b := newLeafKS(t), newLeafKS(t)
	registerLeaf(t, ts, a)
	registerLeaf(t, ts, b)
	events, cancel := h.broadcaster.Subscribe("testnet")
	defer cancel()

	first := activityFor(t, a, protocol.CentralKindLastCallers, "Alice", "Bob")
	if code, body := activityDo(t, ts, a, http.MethodPut, protocol.CentralKindLastCallers, first); code != http.StatusOK {
		t.Fatalf("publish: %d %s", code, body)
	}
	select {
	case ev := <-events:
		if ev.Type != protocol.EventCentralActivityUpdated {
			t.Errorf("event %q", ev.Type)
		}
	case <-time.After(time.Second):
		t.Error("no event for the publish")
	}
	feed := getActivity(t, ts, b, protocol.CentralKindLastCallers)
	if got := handlesIn(feed); !slices.Equal(got, []string{"Alice", "Bob"}) {
		t.Fatalf("feed: %v", got)
	}
	if err := feed.Boards[0].Activity.Verify(); err != nil {
		t.Errorf("served snapshot does not verify: %v", err)
	}
	// Last calls are served whether or not the board is connected.
	if feed.Boards[0].Online {
		t.Error("board without a stream reported online")
	}

	// Bob withdraws: the next snapshot leaves him out, and replaces the
	// first whole.
	second := activityFor(t, a, protocol.CentralKindLastCallers, "Alice")
	if code, body := activityDo(t, ts, a, http.MethodPut, protocol.CentralKindLastCallers, second); code != http.StatusOK {
		t.Fatalf("republish: %d %s", code, body)
	}
	if got := handlesIn(getActivity(t, ts, b, protocol.CentralKindLastCallers)); !slices.Equal(got, []string{"Alice"}) {
		t.Fatalf("after withdrawal of Bob: %v", got)
	}
	// Replaying the first snapshot, which still had Bob, is refused.
	if code, _ := activityDo(t, ts, a, http.MethodPut, protocol.CentralKindLastCallers, first); code != http.StatusConflict {
		t.Errorf("replay over a newer snapshot: %d, want 409", code)
	}

	// The board withdraws the feature: gone, and neither old snapshot comes back.
	if code, _ := activityDo(t, ts, a, http.MethodDelete, protocol.CentralKindLastCallers, nil); code != http.StatusNoContent {
		t.Fatalf("withdraw: %d", code)
	}
	if got := handlesIn(getActivity(t, ts, b, protocol.CentralKindLastCallers)); len(got) != 0 {
		t.Fatalf("after withdraw: %v", got)
	}
	for _, old := range []*protocol.CentralActivity{first, second} {
		if code, _ := activityDo(t, ts, a, http.MethodPut, protocol.CentralKindLastCallers, old); code != http.StatusConflict {
			t.Errorf("replay after withdrawal: %d, want 409", code)
		}
	}
	if got := handlesIn(getActivity(t, ts, b, protocol.CentralKindLastCallers)); len(got) != 0 {
		t.Fatalf("replay restored withdrawn calls: %v", got)
	}
	// A repeated withdrawal is harmless.
	if code, _ := activityDo(t, ts, a, http.MethodDelete, protocol.CentralKindLastCallers, nil); code != http.StatusNoContent {
		t.Errorf("repeat withdraw: %d", code)
	}
}

// Presence is served only while the board's event stream is open, and is
// dropped when its last stream closes: a board that vanishes takes its
// callers off Who's Online. Two streams across a reconnect do not drop it
// early.
func TestCentralPresenceFollowsStream(t *testing.T) {
	h, _ := setupTestHub(t)
	ts := httptest.NewServer(h.newMux())
	defer ts.Close()
	a, b := newLeafKS(t), newLeafKS(t)
	registerLeaf(t, ts, a)
	registerLeaf(t, ts, b)

	if code, body := activityDo(t, ts, a, http.MethodPut, protocol.CentralKindPresence, activityFor(t, a, protocol.CentralKindPresence, "Alice")); code != http.StatusOK {
		t.Fatalf("publish: %d %s", code, body)
	}
	if got := handlesIn(getActivity(t, ts, b, protocol.CentralKindPresence)); len(got) != 0 {
		t.Fatalf("presence served for a board with no stream: %v", got)
	}

	close1 := openStream(t, h, ts, a)
	if got := handlesIn(getActivity(t, ts, b, protocol.CentralKindPresence)); !slices.Equal(got, []string{"Alice"}) {
		t.Fatalf("with a stream: %v", got)
	}
	// A reconnect overlaps: the old stream closes after the new one opens.
	close2 := openStream(t, h, ts, a)
	close1()
	time.Sleep(200 * time.Millisecond)
	if got := handlesIn(getActivity(t, ts, b, protocol.CentralKindPresence)); !slices.Equal(got, []string{"Alice"}) {
		t.Fatalf("presence dropped while a second stream was open: %v", got)
	}
	// The last stream closes, as when the board loses its connection.
	close2()
	waitFor(t, "stream closed", func() bool { return !h.online.online(a.NodeID(), "testnet") })
	waitFor(t, "presence dropped", func() bool {
		rows, _ := h.activity.List("testnet", protocol.CentralKindPresence, time.Now())
		return len(rows) == 0
	})
	// Reconnecting alone does not bring it back: the board must publish
	// again, which a leaf does on every reconnect.
	close3 := openStream(t, h, ts, a)
	defer close3()
	if got := handlesIn(getActivity(t, ts, b, protocol.CentralKindPresence)); len(got) != 0 {
		t.Fatalf("presence came back without a publish: %v", got)
	}
}

// A snapshot must be the authenticated node's own, under its registered
// key, unexpired, not from the future, and within limits.
func TestCentralActivityRejectsForgery(t *testing.T) {
	h, _ := setupTestHub(t)
	ts := httptest.NewServer(h.newMux())
	defer ts.Close()
	a, b := newLeafKS(t), newLeafKS(t)
	registerLeaf(t, ts, a)
	registerLeaf(t, ts, b)
	kind := protocol.CentralKindPresence

	// B sends A's snapshot as its own.
	if code, _ := activityDo(t, ts, b, http.MethodPut, kind, activityFor(t, a, kind, "Alice")); code != http.StatusForbidden {
		t.Errorf("another node's snapshot: %d, want 403", code)
	}
	// Signed by an unregistered key that claims to be A.
	stranger := newLeafKS(t)
	forged := activityFor(t, stranger, kind, "Alice")
	forged.NodeID = a.NodeID()
	if code, _ := activityDo(t, ts, a, http.MethodPut, kind, forged); code != http.StatusForbidden {
		t.Errorf("forged node ID: %d, want 403", code)
	}
	// Tampered after signing.
	tampered := activityFor(t, a, kind, "Alice")
	tampered.Entries[0].Handle = "Mallory"
	if code, _ := activityDo(t, ts, a, http.MethodPut, kind, tampered); code != http.StatusForbidden {
		t.Errorf("tampered: %d, want 403", code)
	}
	// Published as the other kind.
	if code, _ := activityDo(t, ts, a, http.MethodPut, protocol.CentralKindLastCallers, activityFor(t, a, kind, "Alice")); code != http.StatusUnprocessableEntity {
		t.Errorf("wrong kind: %d, want 422", code)
	}
	// Unauthenticated read.
	resp, err := http.Get(ts.URL + "/v3net/v1/testnet/central/presence")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("unauthenticated GET: %d, want 401", resp.StatusCode)
	}
	if rows, _ := h.activity.List("testnet", kind, time.Now()); len(rows) != 0 {
		t.Errorf("a refused snapshot was stored: %+v", rows)
	}
}

func TestCentralActivityPruneAndExpiry(t *testing.T) {
	h, _ := setupTestHub(t)
	ks := newLeafKS(t)
	a := activityFor(t, ks, protocol.CentralKindLastCallers, "Alice")
	raw, _ := json.Marshal(a)
	if err := h.activity.Put(a, raw); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(10 * time.Minute)
	if rows, _ := h.activity.List("testnet", protocol.CentralKindLastCallers, later); len(rows) != 0 {
		t.Error("expired snapshot still listed")
	}
	if n, err := h.activity.PruneExpired(later); err != nil || n != 1 {
		t.Errorf("prune: %d %v", n, err)
	}
}
