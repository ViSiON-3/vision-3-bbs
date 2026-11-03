package hub

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/keystore"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// centralListingFor builds and signs a listing for ks on testnet.
func centralListingFor(t *testing.T, ks *keystore.Keystore, name string) *protocol.CentralListing {
	t.Helper()
	now := time.Now().UTC()
	l := &protocol.CentralListing{
		V3NetCentral: protocol.CentralListingVersion,
		Network:      "testnet",
		Seq:          now.UnixNano(),
		Name:         name,
		Description:  "A test board",
		Addresses:    []string{"ssh://" + strings.ToLower(strings.ReplaceAll(name, " ", "")) + ".example:2222"},
		Capabilities: []string{protocol.FeatureCentralDirectory},
		PublishedAt:  now.Format(time.RFC3339),
		ExpiresAt:    now.Add(24 * time.Hour).Format(time.RFC3339),
	}
	if err := l.Sign(ks); err != nil {
		t.Fatalf("sign listing: %v", err)
	}
	return l
}

func putListing(t *testing.T, ts *httptest.Server, ks *keystore.Keystore, l *protocol.CentralListing) (int, string) {
	t.Helper()
	body, _ := json.Marshal(l)
	resp, err := http.DefaultClient.Do(signedRequest(t, ks, http.MethodPut, ts.URL+"/v3net/v1/testnet/central/listing", string(body)))
	if err != nil {
		t.Fatalf("PUT listing: %v", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func getDirectory(t *testing.T, ts *httptest.Server, ks *keystore.Keystore) protocol.CentralDirectory {
	t.Helper()
	resp, err := http.DefaultClient.Do(signedRequest(t, ks, http.MethodGet, ts.URL+"/v3net/v1/testnet/central/listings", ""))
	if err != nil {
		t.Fatalf("GET directory: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("GET directory: %d %s", resp.StatusCode, b)
	}
	var dir protocol.CentralDirectory
	if err := json.NewDecoder(resp.Body).Decode(&dir); err != nil {
		t.Fatalf("decode directory: %v", err)
	}
	return dir
}

func newLeafKS(t *testing.T) *keystore.Keystore {
	t.Helper()
	ks, _, err := keystore.Load(filepath.Join(t.TempDir(), "leaf.key"))
	if err != nil {
		t.Fatalf("load leaf keystore: %v", err)
	}
	return ks
}

func TestNetworkInfoAdvertisesCentralDirectory(t *testing.T) {
	h, _ := setupTestHub(t)
	ts := httptest.NewServer(h.newMux())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/v3net/v1/testnet/info")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var info protocol.NetworkInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, f := range info.Features {
		if f == protocol.FeatureCentralDirectory {
			found = true
		}
	}
	if !found {
		t.Errorf("info.features %v does not name %s", info.Features, protocol.FeatureCentralDirectory)
	}
}

func TestCentralPublishListAndWithdraw(t *testing.T) {
	h, _ := setupTestHub(t)
	ts := httptest.NewServer(h.newMux())
	defer ts.Close()

	a, b := newLeafKS(t), newLeafKS(t)
	registerLeaf(t, ts, a)
	registerLeaf(t, ts, b)

	// Directory listens on the broadcaster so events can be checked.
	events, cancel := h.broadcaster.Subscribe("testnet")
	defer cancel()

	if code, body := putListing(t, ts, a, centralListingFor(t, a, "Board A")); code != http.StatusOK {
		t.Fatalf("publish A: %d %s", code, body)
	}
	if code, body := putListing(t, ts, b, centralListingFor(t, b, "Board B")); code != http.StatusOK {
		t.Fatalf("publish B: %d %s", code, body)
	}

	dir := getDirectory(t, ts, a)
	if dir.Network != "testnet" || len(dir.Listings) != 2 {
		t.Fatalf("directory: %+v", dir)
	}
	names := map[string]bool{}
	for _, e := range dir.Listings {
		if err := e.Listing.Verify(); err != nil {
			t.Errorf("served listing %q does not verify: %v", e.Listing.Name, err)
		}
		if e.LastSeenAt == "" {
			t.Errorf("served listing %q has no last_seen_at", e.Listing.Name)
		}
		if e.Online {
			t.Errorf("served listing %q reports online with no stream", e.Listing.Name)
		}
		names[e.Listing.Name] = true
	}
	if !names["Board A"] || !names["Board B"] {
		t.Errorf("directory names %v", names)
	}

	// Two updated events, one per publish.
	for i := 0; i < 2; i++ {
		select {
		case ev := <-events:
			if ev.Type != protocol.EventCentralListingUpdated {
				t.Errorf("event %d: %s", i, ev.Type)
			}
			var p protocol.CentralListingEventPayload
			if err := json.Unmarshal(ev.Data, &p); err != nil || p.EventID == "" || p.ExpiresAt == "" || p.NodeID == "" {
				t.Errorf("event payload lacks id, expiry or node: %s", ev.Data)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("no updated event")
		}
	}

	// B withdraws; the directory shrinks and a withdrawn event goes out.
	resp, err := http.DefaultClient.Do(signedRequest(t, b, http.MethodDelete, ts.URL+"/v3net/v1/testnet/central/listing", ""))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("withdraw: %d", resp.StatusCode)
	}
	select {
	case ev := <-events:
		if ev.Type != protocol.EventCentralListingWithdrawn {
			t.Errorf("after withdraw: event %s", ev.Type)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no withdrawn event")
	}
	dir = getDirectory(t, ts, a)
	if len(dir.Listings) != 1 || dir.Listings[0].Listing.Name != "Board A" {
		t.Errorf("after withdraw: %+v", dir.Listings)
	}

	// Withdrawing again is fine and silent.
	resp, err = http.DefaultClient.Do(signedRequest(t, b, http.MethodDelete, ts.URL+"/v3net/v1/testnet/central/listing", ""))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("second withdraw: %d", resp.StatusCode)
	}
	select {
	case ev := <-events:
		t.Errorf("second withdraw published %s", ev.Type)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestCentralPublishRejectsForgeryAndReplay(t *testing.T) {
	h, _ := setupTestHub(t)
	ts := httptest.NewServer(h.newMux())
	defer ts.Close()
	// This test publishes many times in a row; the limit has its own test.
	h.centralLimiter.Stop()
	h.centralLimiter = newBurstRateLimiter(time.Millisecond, 1000)

	a, b := newLeafKS(t), newLeafKS(t)
	registerLeaf(t, ts, a)
	registerLeaf(t, ts, b)

	// B sends A's listing under B's transport signature: the listing's
	// node is not the authenticated node.
	if code, _ := putListing(t, ts, b, centralListingFor(t, a, "Board A")); code != http.StatusForbidden {
		t.Errorf("foreign listing: want 403, got %d", code)
	}

	// A listing whose content was changed after signing.
	l := centralListingFor(t, a, "Board A")
	l.Description = "altered"
	if code, body := putListing(t, ts, a, l); code != http.StatusForbidden || !strings.Contains(body, "signature") {
		t.Errorf("altered listing: want 403 signature error, got %d %s", code, body)
	}

	// A listing signed by a key other than the one A registered with, but
	// claiming A's node ID: Verify's node derivation refuses it.
	other := newLeafKS(t)
	l = centralListingFor(t, other, "Board A")
	l.NodeID = a.NodeID()
	if code, _ := putListing(t, ts, a, l); code != http.StatusForbidden {
		t.Errorf("foreign key: want 403, got %d", code)
	}

	// Replay: a listing with a seq at or below the stored one.
	first := centralListingFor(t, a, "Board A")
	if code, body := putListing(t, ts, a, first); code != http.StatusOK {
		t.Fatalf("publish: %d %s", code, body)
	}
	if code, _ := putListing(t, ts, a, first); code != http.StatusConflict {
		t.Errorf("replayed listing: want 409, got %d", code)
	}
	older := centralListingFor(t, a, "Board A")
	older.Seq = first.Seq - 1
	if err := older.Sign(a); err != nil {
		t.Fatal(err)
	}
	if code, _ := putListing(t, ts, a, older); code != http.StatusConflict {
		t.Errorf("older listing: want 409, got %d", code)
	}
	newer := centralListingFor(t, a, "Board A renamed")
	if code, _ := putListing(t, ts, a, newer); code != http.StatusOK {
		t.Errorf("newer listing: want 200, got %d", code)
	}
	dir := getDirectory(t, ts, b)
	if len(dir.Listings) != 1 || dir.Listings[0].Listing.Name != "Board A renamed" {
		t.Errorf("directory after update: %+v", dir.Listings)
	}

	// Content limits are the hub's to enforce too.
	bad := centralListingFor(t, a, "|04Red|07")
	if code, _ := putListing(t, ts, a, bad); code != http.StatusUnprocessableEntity {
		t.Errorf("pipe-coded name: want 422, got %d", code)
	}
	future := centralListingFor(t, a, "Board A")
	future.PublishedAt = time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	future.ExpiresAt = time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339)
	if err := future.Sign(a); err != nil {
		t.Fatal(err)
	}
	if code, _ := putListing(t, ts, a, future); code != http.StatusUnprocessableEntity {
		t.Errorf("future listing: want 422, got %d", code)
	}
	wrongNet := centralListingFor(t, a, "Board A")
	wrongNet.Network = "othernet"
	if err := wrongNet.Sign(a); err != nil {
		t.Fatal(err)
	}
	if code, _ := putListing(t, ts, a, wrongNet); code != http.StatusUnprocessableEntity {
		t.Errorf("wrong network: want 422, got %d", code)
	}
}

func TestCentralDirectoryRequiresAuthAndOmitsExpired(t *testing.T) {
	h, _ := setupTestHub(t)
	ts := httptest.NewServer(h.newMux())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/v3net/v1/testnet/central/listings")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("unauthenticated directory: want 401, got %d", resp.StatusCode)
	}

	a := newLeafKS(t)
	registerLeaf(t, ts, a)
	l := centralListingFor(t, a, "Board A")
	if code, _ := putListing(t, ts, a, l); code != http.StatusOK {
		t.Fatal("publish")
	}
	// Age the stored row past its expiry, as a board gone for a month would be.
	if _, err := h.db.Exec("UPDATE central_listings SET expires_at = ?", time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	if dir := getDirectory(t, ts, a); len(dir.Listings) != 0 {
		t.Errorf("expired listing served: %+v", dir.Listings)
	}
	n, err := h.central.PruneExpired(time.Now())
	if err != nil || n != 1 {
		t.Errorf("prune: n=%d err=%v", n, err)
	}
	// The node may publish again after expiry: no row, so no seq to beat.
	if code, _ := putListing(t, ts, a, centralListingFor(t, a, "Board A")); code != http.StatusOK {
		t.Errorf("republish after prune: %d", code)
	}
}

func TestCentralDirectoryReportsOnlineAndLastSeen(t *testing.T) {
	h, _ := setupTestHub(t)
	ts := httptest.NewServer(h.newMux())
	defer ts.Close()

	a, b := newLeafKS(t), newLeafKS(t)
	registerLeaf(t, ts, a)
	registerLeaf(t, ts, b)
	if code, _ := putListing(t, ts, a, centralListingFor(t, a, "Board A")); code != http.StatusOK {
		t.Fatal("publish")
	}
	seen0 := getDirectory(t, ts, b).Listings[0].LastSeenAt

	// A opens its event stream: online, and seen again.
	time.Sleep(1100 * time.Millisecond) // last_seen_at has one-second resolution
	sseResp, err := http.DefaultClient.Do(signedRequest(t, a, http.MethodGet, ts.URL+"/v3net/v1/testnet/events", ""))
	if err != nil {
		t.Fatal(err)
	}
	if sseResp.StatusCode != http.StatusOK {
		t.Fatalf("SSE: %d", sseResp.StatusCode)
	}
	deadline := time.Now().Add(3 * time.Second)
	var entry protocol.CentralDirectoryEntry
	for {
		entry = getDirectory(t, ts, b).Listings[0]
		if entry.Online || time.Now().After(deadline) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !entry.Online {
		t.Error("board with an open stream not reported online")
	}
	if entry.LastSeenAt == seen0 {
		t.Error("opening the stream did not move last_seen_at")
	}
	sseResp.Body.Close()
	deadline = time.Now().Add(3 * time.Second)
	for {
		entry = getDirectory(t, ts, b).Listings[0]
		if !entry.Online || time.Now().After(deadline) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if entry.Online {
		t.Error("board still online after its stream closed")
	}
}

func TestCentralPublishRateLimit(t *testing.T) {
	h, _ := setupTestHub(t)
	ts := httptest.NewServer(h.newMux())
	defer ts.Close()
	a := newLeafKS(t)
	registerLeaf(t, ts, a)

	var codes []int
	for i := 0; i < centralPublishBurst+2; i++ {
		l := centralListingFor(t, a, "Board A")
		l.Seq += int64(i) // strictly rising
		if err := l.Sign(a); err != nil {
			t.Fatal(err)
		}
		code, _ := putListing(t, ts, a, l)
		codes = append(codes, code)
	}
	for i, code := range codes {
		want := http.StatusOK
		if i >= centralPublishBurst {
			want = http.StatusTooManyRequests
		}
		if code != want {
			t.Errorf("publish %d: want %d, got %d", i, want, code)
		}
	}
}

func TestOnlineNodesCountsStreams(t *testing.T) {
	o := newOnlineNodes()
	o.add("n", "net")
	o.add("n", "net")
	o.remove("n", "net")
	if !o.online("n", "net") {
		t.Error("one stream left: should be online")
	}
	o.remove("n", "net")
	if o.online("n", "net") {
		t.Error("no streams: should be offline")
	}
	o.remove("n", "net") // extra remove is harmless
	if o.online("n", "net") {
		t.Error("still offline")
	}
}
