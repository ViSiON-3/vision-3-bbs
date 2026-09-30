package hub

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/keystore"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/nal"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

func TestNew_UnusableDataDir(t *testing.T) {
	ks := loadTestKeystore(t, "hub.key")
	_, err := New(Config{
		DataDir:  filepath.Join(t.TempDir(), "does", "not", "exist"),
		Keystore: ks,
	})
	if err == nil {
		t.Fatal("expected error when the data directory does not exist")
	}
}

func TestHubStart_ServesUntilCancelled(t *testing.T) {
	dir := t.TempDir()
	ks, _, err := keystore.Load(filepath.Join(dir, "hub.key"))
	if err != nil {
		t.Fatalf("load keystore: %v", err)
	}

	// Reserve a free port, then hand it to the hub.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()

	h, err := New(Config{
		ListenAddr: addr,
		DataDir:    dir,
		Keystore:   ks,
		Networks:   []NetworkConfig{{Name: "testnet", Description: "Test network"}},
	})
	if err != nil {
		t.Fatalf("create hub: %v", err)
	}
	defer h.Close()

	if h.Subscribers() != h.subscribers || h.NALStore() != h.nalStore {
		t.Error("accessors should expose the hub's own stores")
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- h.Start(ctx) }()

	// Poll until the listener is up, then check it serves the hub API.
	var summaries []protocol.NetworkSummary
	deadline := time.Now().Add(5 * time.Second)
	for {
		resp, err := http.Get("http://" + addr + "/v3net/v1/networks")
		if err == nil {
			decErr := json.NewDecoder(resp.Body).Decode(&summaries)
			resp.Body.Close()
			if decErr != nil {
				t.Fatalf("decode networks: %v", decErr)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("hub never started listening on %s: %v", addr, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(summaries) != 1 || summaries[0].Name != "testnet" || summaries[0].HubNodeID != ks.NodeID() {
		t.Errorf("unexpected networks from started hub: %+v", summaries)
	}

	// Cancelling the context is a clean shutdown, not an error.
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Start after cancel = %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Start did not return after context cancel")
	}
}

func TestHubStart_BadListenAddr(t *testing.T) {
	dir := t.TempDir()
	ks, _, err := keystore.Load(filepath.Join(dir, "hub.key"))
	if err != nil {
		t.Fatalf("load keystore: %v", err)
	}
	h, err := New(Config{ListenAddr: "127.0.0.1:not-a-port", DataDir: dir, Keystore: ks})
	if err != nil {
		t.Fatalf("create hub: %v", err)
	}
	defer h.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := h.Start(ctx); err == nil {
		t.Error("expected Start to fail on an unusable listen address")
	}
}

func TestMux_RoutesLikeInternalMux(t *testing.T) {
	h, _ := setupTestHub(t)
	ts := httptest.NewServer(h.Mux())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/v3net/v1/testnet/info")
	if err != nil {
		t.Fatalf("GET info: %v", err)
	}
	defer resp.Body.Close()
	var info protocol.NetworkInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		t.Fatalf("decode info: %v", err)
	}
	if info.Name != "testnet" || info.Policy.MaxBodyBytes != protocol.MaxBodyBytes {
		t.Errorf("unexpected network info: %+v", info)
	}

	// Paths outside /v3net/v1/{network} and unknown networks are 404s.
	for _, path := range []string{"/", "/v3net/v2/testnet/messages", "/v3net/v1/nonet/info", "/v3net/v1/nonet/nal"} {
		resp, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("GET %s: expected 404, got %d", path, resp.StatusCode)
		}
	}
}

func TestMux_AuthenticatedUnknownRouteIs404(t *testing.T) {
	_, ts, leafKS := setupChatTest(t)
	if code := sendSigned(t, leafKS, "GET", ts.URL+"/v3net/v1/testnet/no/such/route", "", nil); code != http.StatusNotFound {
		t.Errorf("expected 404 for unknown authenticated route, got %d", code)
	}
}

func TestAuth_RejectsBadCredentials(t *testing.T) {
	_, ts, leafKS := setupChatTest(t)
	url := ts.URL + "/v3net/v1/testnet/messages"

	// A Date header that does not parse.
	req := signedRequest(t, leafKS, "GET", url, "")
	req.Header.Set("Date", "yesterday-ish")
	if code := postStatus(t, req); code != http.StatusUnauthorized {
		t.Errorf("unparseable Date: expected 401, got %d", code)
	}

	// A node the hub has never seen.
	stranger := loadTestKeystore(t, "stranger.key")
	if code := sendSigned(t, stranger, "GET", url, "", nil); code != http.StatusUnauthorized {
		t.Errorf("unknown node: expected 401, got %d", code)
	}

	// A signature made with another node's key.
	req = signedRequest(t, stranger, "GET", url, "")
	req.Header.Set(headerNodeID, leafKS.NodeID())
	if code := postStatus(t, req); code != http.StatusUnauthorized {
		t.Errorf("forged signature: expected 401, got %d", code)
	}

	// A body altered after signing no longer matches the signed hash.
	req = signedRequest(t, leafKS, "POST", ts.URL+"/v3net/v1/testnet/presence", `{"type":"logon","handle":"a"}`)
	req.Body = http.NoBody
	req.ContentLength = 0
	if code := postStatus(t, req); code != http.StatusUnauthorized {
		t.Errorf("tampered body: expected 401, got %d", code)
	}

	// Bodies over the 64KB limit are refused before signature checking.
	big := `{"pad":"` + strings.Repeat("x", 70*1024) + `"}`
	if code := sendSigned(t, leafKS, "POST", ts.URL+"/v3net/v1/testnet/presence", big, nil); code != http.StatusBadRequest {
		t.Errorf("oversized body: expected 400, got %d", code)
	}
}

// TestHub_CloseTwice checks that a second Close, as a signal handler plus a
// deferred cleanup would make, neither panics nor reports an error (#518).
func TestHub_CloseTwice(t *testing.T) {
	h, _ := setupTestHub(t) // its cleanup closes the hub a third time
	if err := h.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := h.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
}

func TestRateLimiter_StopTwice(t *testing.T) {
	rl := newRateLimiter(time.Minute)
	rl.Stop()
	rl.Stop()
}

func TestRateLimiter_EvictDropsOnlyStaleKeys(t *testing.T) {
	rl := newRateLimiter(time.Minute) // ttl is 10 minutes
	defer rl.Stop()

	if !rl.Allow("fresh") {
		t.Fatal("first request for a key should be allowed")
	}
	rl.mu.Lock()
	rl.last["stale"] = time.Now().Add(-11 * time.Minute)
	rl.mu.Unlock()

	rl.evict()

	rl.mu.Lock()
	_, freshKept := rl.last["fresh"]
	_, staleKept := rl.last["stale"]
	rl.mu.Unlock()
	if !freshKept {
		t.Error("evict removed a key that is still within the TTL")
	}
	if staleKept {
		t.Error("evict kept a key older than the TTL")
	}
	// The surviving key is still rate limited.
	if rl.Allow("fresh") {
		t.Error("fresh key should still be limited after evict")
	}
}

func TestBroadcaster_SlowConsumerAndCancel(t *testing.T) {
	b := NewBroadcaster()
	slow, cancelSlow := b.Subscribe("net")
	other, cancelOther := b.Subscribe("othernet")
	defer cancelOther()

	// Publishing past the buffer must drop rather than block.
	for i := 0; i < eventBufSize+10; i++ {
		b.Publish("net", protocol.Event{Type: fmt.Sprintf("ev%d", i)})
	}
	if len(slow) != eventBufSize {
		t.Errorf("slow consumer buffered %d events, want %d", len(slow), eventBufSize)
	}
	if first := <-slow; first.Type != "ev0" {
		t.Errorf("first buffered event = %q, want ev0 (newest are dropped)", first.Type)
	}
	if len(other) != 0 {
		t.Errorf("events leaked to another network: %d", len(other))
	}

	// Cancel closes the channel and is safe to call twice.
	cancelSlow()
	cancelSlow()
	for range slow {
	}
	b.Publish("net", protocol.Event{Type: "after-cancel"}) // no subscribers: must not panic
}

func TestBroadcaster_StartPingStopsOnCancel(t *testing.T) {
	b := NewBroadcaster()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		b.StartPing(ctx)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("StartPing did not return after context cancel")
	}
}

func TestSubscribe_Validation(t *testing.T) {
	h, _ := setupTestHub(t)
	ts := httptest.NewServer(h.newMux())
	defer ts.Close()
	ks := loadTestKeystore(t, "leaf.key")
	other := loadTestKeystore(t, "other.key")

	sub := func(network, nodeID, pubKey string) string {
		return fmt.Sprintf(`{"network":%q,"node_id":%q,"pubkey_b64":%q,"bbs_name":"B","bbs_host":"h"}`,
			network, nodeID, pubKey)
	}
	cases := []struct {
		name, body string
		want       int
	}{
		{"invalid JSON", `{"network":`, http.StatusBadRequest},
		{"unknown network", sub("nonet", ks.NodeID(), ks.PubKeyBase64()), http.StatusNotFound},
		{"pubkey not base64", sub("testnet", ks.NodeID(), "!!!"), http.StatusUnprocessableEntity},
		{"pubkey wrong length", sub("testnet", ks.NodeID(), "AAAA"), http.StatusUnprocessableEntity},
		{"node ID of another key", sub("testnet", other.NodeID(), ks.PubKeyBase64()), http.StatusUnprocessableEntity},
		{"oversized body", `{"network":"` + strings.Repeat("x", 9*1024) + `"}`, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := http.Post(ts.URL+"/v3net/v1/subscribe", "application/json", strings.NewReader(tc.body))
			if err != nil {
				t.Fatalf("POST subscribe: %v", err)
			}
			resp.Body.Close()
			if resp.StatusCode != tc.want {
				t.Errorf("expected %d, got %d", tc.want, resp.StatusCode)
			}
		})
	}
	if h.subscribers.Get(ks.NodeID(), "testnet") != nil || h.subscribers.Get(other.NodeID(), "testnet") != nil {
		t.Error("rejected subscribe requests must not register a node")
	}
}

// subscribeWithAreas posts a subscribe request carrying area tags and returns
// the status code and decoded response.
func subscribeWithAreas(t *testing.T, ts *httptest.Server, ks *keystore.Keystore, tags ...string) (int, protocol.SubscribeWithAreasResponse) {
	t.Helper()
	body, _ := json.Marshal(protocol.SubscribeRequest{
		Network: "testnet", NodeID: ks.NodeID(), PubKeyB64: ks.PubKeyBase64(),
		BBSName: "Test BBS", BBSHost: "test.example.net", AreaTags: tags,
	})
	resp, err := http.Post(ts.URL+"/v3net/v1/subscribe", "application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("POST subscribe: %v", err)
	}
	defer resp.Body.Close()
	var out protocol.SubscribeWithAreasResponse
	if resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatalf("decode subscribe response: %v", err)
		}
	}
	return resp.StatusCode, out
}

func TestSubscribeWithAreaTags_EdgeCases(t *testing.T) {
	t.Run("pending subscriber gets no area subscriptions", func(t *testing.T) {
		h, hubKS := setupTestHubManual(t)
		ts := httptest.NewServer(h.newMux())
		defer ts.Close()
		seedTestNAL(t, h, hubKS)
		leafKS := loadTestKeystore(t, "leaf.key")

		code, resp := subscribeWithAreas(t, ts, leafKS, "gen.general")
		if code != http.StatusOK || resp.Status != "pending" || len(resp.Areas) != 0 {
			t.Errorf("status %d, response %+v; want 200, pending, no areas", code, resp)
		}
		if subs, _ := h.areaSubscriptions.ListForNode(leafKS.NodeID(), "testnet"); len(subs) != 0 {
			t.Errorf("pending node should have no area subscriptions, got %+v", subs)
		}
	})

	t.Run("no NAL published", func(t *testing.T) {
		h, _ := setupTestHub(t)
		ts := httptest.NewServer(h.newMux())
		defer ts.Close()
		leafKS := loadTestKeystore(t, "leaf.key")

		code, resp := subscribeWithAreas(t, ts, leafKS, "gen.general")
		if code != http.StatusOK || resp.Status != "active" || len(resp.Areas) != 0 {
			t.Errorf("status %d, response %+v; want 200, active, no areas", code, resp)
		}
	})

	t.Run("unknown tag rejects the whole request", func(t *testing.T) {
		h, hubKS := setupTestHub(t)
		ts := httptest.NewServer(h.newMux())
		defer ts.Close()
		seedTestNAL(t, h, hubKS)
		leafKS := loadTestKeystore(t, "leaf.key")

		if code, _ := subscribeWithAreas(t, ts, leafKS, "gen.general", "gen.missing"); code != http.StatusUnprocessableEntity {
			t.Errorf("expected 422 for unknown tag, got %d", code)
		}
		// The valid tag listed first must not have been applied.
		if subs, _ := h.areaSubscriptions.ListForNode(leafKS.NodeID(), "testnet"); len(subs) != 0 {
			t.Errorf("partial subscription applied despite rejection: %+v", subs)
		}
	})

	t.Run("closed area admits allow-listed node", func(t *testing.T) {
		h, hubKS := setupTestHub(t)
		ts := httptest.NewServer(h.newMux())
		defer ts.Close()
		leafKS := loadTestKeystore(t, "leaf.key")
		seedNALWithAreas(t, h, hubKS, []protocol.Area{{
			Tag: "gen.private", Name: "Private", Language: "en",
			Access: protocol.AreaAccess{Mode: protocol.AccessModeClosed, AllowList: []string{leafKS.NodeID()}},
		}})

		code, resp := subscribeWithAreas(t, ts, leafKS, "gen.private")
		if code != http.StatusOK || len(resp.Areas) != 1 || resp.Areas[0].Status != "active" {
			t.Errorf("status %d, response %+v; want 200 with gen.private active", code, resp)
		}
		if active, _ := h.areaSubscriptions.IsActive(leafKS.NodeID(), "testnet", "gen.private"); !active {
			t.Error("allow-listed node should hold an active subscription")
		}
	})
}

func TestPostMessage_Validation(t *testing.T) {
	h, hubKS := setupTestHub(t)
	ts := httptest.NewServer(h.newMux())
	defer ts.Close()
	seedTestNAL(t, h, hubKS)
	leafKS := loadTestKeystore(t, "leaf.key")
	registerLeafWithAreas(t, ts, leafKS, []string{"gen.general"})
	url := ts.URL + "/v3net/v1/testnet/messages"
	const uuid = "770e8400-e29b-41d4-a716-446655440000"

	wrongNet := strings.Replace(testMsgJSON("gen.general", uuid), `"network": "testnet"`, `"network": "othernet"`, 1)
	cases := []struct {
		name, body string
		want       int
	}{
		{"invalid JSON", `{"v3net":`, http.StatusBadRequest},
		{"fails validation", `{"v3net":"1.0","network":"testnet"}`, http.StatusUnprocessableEntity},
		{"network mismatch", wrongNet, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if code := sendSigned(t, leafKS, "POST", url, tc.body, nil); code != tc.want {
				t.Errorf("expected %d, got %d", tc.want, code)
			}
		})
	}
	if n, err := h.messages.Count("testnet"); err != nil || n != 0 {
		t.Errorf("rejected messages must not be stored: count %d, err %v", n, err)
	}
}

func TestPostNAL_Validation(t *testing.T) {
	h, hubKS := setupTestHub(t)
	ts := httptest.NewServer(h.newMux())
	defer ts.Close()
	registerHubAsLeaf(t, ts, hubKS)
	leafKS := loadTestKeystore(t, "leaf.key")
	registerLeaf(t, ts, leafKS)
	url := ts.URL + "/v3net/v1/testnet/nal"

	signedNAL := func(network string, coord *keystore.Keystore) string {
		n := &protocol.NAL{
			V3NetNAL: "1.0", Network: network,
			CoordNodeID: coord.NodeID(), CoordPubKeyB64: coord.PubKeyBase64(),
			Areas: []protocol.Area{},
		}
		if err := nal.Sign(n, coord); err != nil {
			t.Fatalf("sign NAL: %v", err)
		}
		data, _ := json.Marshal(n)
		return string(data)
	}

	if code := sendSigned(t, hubKS, "POST", url, `{"network":`, nil); code != http.StatusBadRequest {
		t.Errorf("invalid JSON: expected 400, got %d", code)
	}
	if code := sendSigned(t, hubKS, "POST", url, signedNAL("othernet", hubKS), nil); code != http.StatusBadRequest {
		t.Errorf("network mismatch: expected 400, got %d", code)
	}
	// The operator cannot bootstrap a NAL naming someone else as coordinator.
	if code := sendSigned(t, hubKS, "POST", url, signedNAL("testnet", leafKS), nil); code != http.StatusBadRequest {
		t.Errorf("initial NAL with foreign coordinator: expected 400, got %d", code)
	}
	// A NAL altered after signing fails verification.
	tampered := strings.Replace(signedNAL("testnet", hubKS), `"areas":[]`,
		`"areas":[{"tag":"gen.sneaky","name":"Sneaky","access":{"mode":"open"}}]`, 1)
	if code := sendSigned(t, hubKS, "POST", url, tampered, nil); code != http.StatusUnprocessableEntity {
		t.Errorf("tampered NAL: expected 422, got %d", code)
	}

	if stored, err := h.nalStore.Get("testnet"); err != nil || stored != nil {
		t.Errorf("rejected NALs must not be stored: %+v, %v", stored, err)
	}
}
