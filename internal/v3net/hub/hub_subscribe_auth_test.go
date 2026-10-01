package hub

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/keystore"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// subscribeBody builds a subscribe request body for ks.
func subscribeBody(t *testing.T, ks *keystore.Keystore, name, host string, tags ...string) string {
	t.Helper()
	body, err := json.Marshal(protocol.SubscribeRequest{
		Network: "testnet", NodeID: ks.NodeID(), PubKeyB64: ks.PubKeyBase64(),
		BBSName: name, BBSHost: host, AreaTags: tags,
	})
	if err != nil {
		t.Fatalf("marshal subscribe: %v", err)
	}
	return string(body)
}

// postSubscribe sends req and returns the status code.
func postSubscribe(t *testing.T, req *http.Request) int {
	t.Helper()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST subscribe: %v", err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

func assertProfile(t *testing.T, h *Hub, nodeID, name, host string) {
	t.Helper()
	sub := h.subscribers.Get(nodeID, "testnet")
	if sub == nil {
		t.Fatal("subscriber missing")
	}
	if sub.BBSName != name || sub.BBSHost != host {
		t.Errorf("profile = %q/%q, want %q/%q", sub.BBSName, sub.BBSHost, name, host)
	}
	listed, err := h.subscribers.List("testnet")
	if err != nil {
		t.Fatalf("list subscribers: %v", err)
	}
	for _, s := range listed {
		if s.NodeID == nodeID && (s.BBSName != name || s.BBSHost != host) {
			t.Errorf("stored profile = %q/%q, want %q/%q", s.BBSName, s.BBSHost, name, host)
		}
	}
}

func TestSubscribe_SignedResubscribeUpdatesProfile(t *testing.T) {
	h, _ := setupTestHub(t)
	ts := httptest.NewServer(h.newMux())
	defer ts.Close()
	leafKS := loadTestKeystore(t, "leaf.key")
	registerLeaf(t, ts, leafKS)

	url := ts.URL + "/v3net/v1/subscribe"
	if code := postSubscribe(t, signedRequest(t, leafKS, "POST", url, subscribeBody(t, leafKS, "New Name", "new.example.net"))); code != http.StatusOK {
		t.Fatalf("signed re-subscribe: expected 200, got %d", code)
	}
	assertProfile(t, h, leafKS.NodeID(), "New Name", "new.example.net")
}

func TestSubscribe_SignedFirstSubscribeRegisters(t *testing.T) {
	h, _ := setupTestHub(t)
	ts := httptest.NewServer(h.newMux())
	defer ts.Close()
	leafKS := loadTestKeystore(t, "leaf.key")

	url := ts.URL + "/v3net/v1/subscribe"
	if code := postSubscribe(t, signedRequest(t, leafKS, "POST", url, subscribeBody(t, leafKS, "Leaf", "leaf.example.net"))); code != http.StatusOK {
		t.Fatalf("signed subscribe: expected 200, got %d", code)
	}
	assertProfile(t, h, leafKS.NodeID(), "Leaf", "leaf.example.net")
}

func TestSubscribe_BannedNodeKeepsProfile(t *testing.T) {
	h, _ := setupTestHub(t)
	ts := httptest.NewServer(h.newMux())
	defer ts.Close()
	leafKS := loadTestKeystore(t, "leaf.key")
	registerLeaf(t, ts, leafKS)
	if err := h.subscribers.SetStatus(leafKS.NodeID(), "testnet", "banned"); err != nil {
		t.Fatalf("ban: %v", err)
	}

	url := ts.URL + "/v3net/v1/subscribe"
	if code := postSubscribe(t, signedRequest(t, leafKS, "POST", url, subscribeBody(t, leafKS, "Renamed", "renamed.example.net"))); code != http.StatusOK {
		t.Fatalf("signed re-subscribe: expected 200, got %d", code)
	}
	assertProfile(t, h, leafKS.NodeID(), "Test BBS", "test.example.net")
}

// Node keys are public, so an unsigned request naming a known node must not
// change anything about it.
func TestSubscribe_UnsignedResubscribeChangesNothing(t *testing.T) {
	h, hubKS := setupTestHub(t)
	ts := httptest.NewServer(h.newMux())
	defer ts.Close()
	seedTestNAL(t, h, hubKS)
	leafKS := loadTestKeystore(t, "leaf.key")
	registerLeaf(t, ts, leafKS)

	body := subscribeBody(t, leafKS, "Impostor", "evil.example.net", "gen.general")
	resp, err := http.Post(ts.URL+"/v3net/v1/subscribe", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST subscribe: %v", err)
	}
	defer resp.Body.Close()
	var out protocol.SubscribeWithAreasResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.StatusCode != http.StatusOK || out.Status != "active" || len(out.Areas) != 0 {
		t.Errorf("status %d, response %+v; want 200, active, no areas", resp.StatusCode, out)
	}

	assertProfile(t, h, leafKS.NodeID(), "Test BBS", "test.example.net")
	if subs, _ := h.areaSubscriptions.ListForNode(leafKS.NodeID(), "testnet"); len(subs) != 0 {
		t.Errorf("unsigned re-subscribe changed area subscriptions: %+v", subs)
	}
}

func TestSubscribe_RejectsBadSignatures(t *testing.T) {
	h, _ := setupTestHub(t)
	ts := httptest.NewServer(h.newMux())
	defer ts.Close()
	leafKS := loadTestKeystore(t, "leaf.key")
	other := loadTestKeystore(t, "other.key")
	registerLeaf(t, ts, leafKS)

	url := ts.URL + "/v3net/v1/subscribe"
	body := subscribeBody(t, leafKS, "Impostor", "evil.example.net")

	cases := []struct {
		name string
		req  func() *http.Request
	}{
		{"signed by another key", func() *http.Request {
			r := signedRequest(t, other, "POST", url, body)
			r.Header.Set(headerNodeID, leafKS.NodeID())
			return r
		}},
		{"node ID header of another node", func() *http.Request {
			return signedRequest(t, other, "POST", url, body)
		}},
		{"body changed after signing", func() *http.Request {
			r := signedRequest(t, leafKS, "POST", url, subscribeBody(t, leafKS, "Test BBS", "test.example.net"))
			r.Body = io.NopCloser(strings.NewReader(body))
			r.ContentLength = int64(len(body))
			return r
		}},
		{"stale Date", func() *http.Request {
			r := signedRequest(t, leafKS, "POST", url, body)
			r.Header.Set("Date", time.Now().Add(-time.Hour).UTC().Format(http.TimeFormat))
			return r
		}},
		{"signature without Date", func() *http.Request {
			r := signedRequest(t, leafKS, "POST", url, body)
			r.Header.Del("Date")
			return r
		}},
		{"signature without node ID header", func() *http.Request {
			r := signedRequest(t, leafKS, "POST", url, body)
			r.Header.Del(headerNodeID)
			return r
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if code := postSubscribe(t, tc.req()); code != http.StatusUnauthorized {
				t.Errorf("expected 401, got %d", code)
			}
		})
	}
	assertProfile(t, h, leafKS.NodeID(), "Test BBS", "test.example.net")
}

func TestSubscribe_RejectsNodeRegisteredWithDifferentKey(t *testing.T) {
	h, _ := setupTestHub(t)
	ts := httptest.NewServer(h.newMux())
	defer ts.Close()
	leafKS := loadTestKeystore(t, "leaf.key")
	other := loadTestKeystore(t, "other.key")

	// Simulate a node ID collision: leaf's ID stored under another key.
	if _, err := h.subscribers.Add(Subscriber{
		NodeID: leafKS.NodeID(), Network: "testnet", PubKeyB64: other.PubKeyBase64(),
		BBSName: "Original", BBSHost: "orig.example.net", Status: "active",
	}); err != nil {
		t.Fatalf("seed subscriber: %v", err)
	}

	url := ts.URL + "/v3net/v1/subscribe"
	if code := postSubscribe(t, signedRequest(t, leafKS, "POST", url, subscribeBody(t, leafKS, "Leaf", "leaf.example.net"))); code != http.StatusConflict {
		t.Errorf("expected 409, got %d", code)
	}
	assertProfile(t, h, leafKS.NodeID(), "Original", "orig.example.net")
}

// A leaf re-subscribes with its full area list on every start, so an area a
// manager approved must stay active rather than drop back to pending.
func TestSubscribe_ApprovedAreaSurvivesResubscribe(t *testing.T) {
	h, ts, hubKS, leafKS := setupAccessTest(t)
	base := ts.URL + "/v3net/v1/testnet/areas/gen.general/access"

	registerLeafWithAreas(t, ts, leafKS, []string{"gen.general"})
	if got := areaSubscriptionStatus(t, h, leafKS.NodeID()); got != "pending" {
		t.Fatalf("area subscription = %q before approval, want pending", got)
	}
	nodeIDs := fmt.Sprintf(`{"node_ids":[%q]}`, leafKS.NodeID())
	if code := sendSigned(t, hubKS, "POST", base+"/approve", nodeIDs, nil); code != http.StatusOK {
		t.Fatalf("approve: expected 200, got %d", code)
	}

	code, resp := subscribeWithAreas(t, ts, leafKS, "gen.general")
	if code != http.StatusOK || len(resp.Areas) != 1 || resp.Areas[0].Status != "active" {
		t.Errorf("status %d, response %+v; want 200 with gen.general active", code, resp)
	}
	if got := areaSubscriptionStatus(t, h, leafKS.NodeID()); got != "active" {
		t.Errorf("area subscription = %q after re-subscribe, want active", got)
	}
	if got := accessRequestStatus(t, h, leafKS.NodeID()); got != "approved" {
		t.Errorf("access request = %q after re-subscribe, want approved", got)
	}
}

// An active subscription is kept even when the node is not on the allow
// list, e.g. it joined while the area was open.
func TestSubscribe_ActiveAreaNotDowngradedByModeChange(t *testing.T) {
	h, ts, _, leafKS := setupAccessTest(t)
	if err := h.areaSubscriptions.Upsert(leafKS.NodeID(), "testnet", "gen.general", "active"); err != nil {
		t.Fatalf("seed area subscription: %v", err)
	}

	code, resp := subscribeWithAreas(t, ts, leafKS, "gen.general")
	if code != http.StatusOK || len(resp.Areas) != 1 || resp.Areas[0].Status != "active" {
		t.Errorf("status %d, response %+v; want 200 with gen.general active", code, resp)
	}
	pending, err := h.accessRequests.ListPending("testnet", "gen.general")
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(pending) != 0 {
		t.Errorf("re-subscribe filed access requests for an active area: %+v", pending)
	}
}
