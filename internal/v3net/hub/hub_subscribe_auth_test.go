package hub

import (
	"database/sql"
	"encoding/json"
	"fmt"
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

// Reject unsigned requests before they can create or mutate any registration.
func TestSubscribe_RejectsUnsigned(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(fmt.Sprintf("existing=%v", existing), func(t *testing.T) {
			h, hubKS := setupTestHub(t)
			ts := httptest.NewServer(h.newMux())
			defer ts.Close()
			seedTestNAL(t, h, hubKS)
			ks := loadTestKeystore(t, "leaf.key")
			if existing {
				registerLeaf(t, ts, ks)
			}
			for _, tags := range [][]string{nil, {"gen.general"}} {
				body := subscribeBody(t, ks, "Impostor", "evil.example.net", tags...)
				req, err := http.NewRequest("POST", ts.URL+"/v3net/v1/subscribe", strings.NewReader(body))
				if err != nil {
					t.Fatal(err)
				}
				if code := postSubscribe(t, req); code != http.StatusUnauthorized {
					t.Fatalf("unsigned subscribe: got %d, want 401", code)
				}
			}
			if existing {
				assertProfile(t, h, ks.NodeID(), "Test BBS", "test.example.net")
			} else if h.subscribers.Get(ks.NodeID(), "testnet") != nil {
				t.Fatal("unsigned request created a subscriber")
			}
			if subs, err := h.areaSubscriptions.ListForNode(ks.NodeID(), "testnet"); err != nil || len(subs) != 0 {
				t.Fatalf("area subscriptions = %+v, error %v", subs, err)
			}
			if requests, err := h.accessRequests.ListPending("testnet", "gen.general"); err != nil || len(requests) != 0 {
				t.Fatalf("access requests = %+v, error %v", requests, err)
			}
		})
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
		{"node ID without signature", func() *http.Request {
			r := signedRequest(t, leafKS, "POST", url, body)
			r.Header.Del(headerSignature)
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
	if _, err := h.areaSubscriptions.Upsert(leafKS.NodeID(), "testnet", "gen.general", "active"); err != nil {
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

// Existing records may predate signed subscriptions. Reopen the persisted
// stores and prove that the same key resumes without resetting approvals.
func TestSubscribe_ExistingNodeUpgrade(t *testing.T) {
	for _, status := range []string{"active", "pending", "banned"} {
		t.Run(status, func(t *testing.T) {
			h, hubKS := setupTestHubManual(t)
			ks := loadTestKeystore(t, "leaf.key")
			seedNALWithAreas(t, h, hubKS, []protocol.Area{
				{Tag: "gen.general", Name: "General", Language: "en", Access: protocol.AreaAccess{Mode: protocol.AccessModeApproval}},
			})
			// Seed precisely the persisted identity of a pre-upgrade registration.
			if _, err := h.subscribers.Add(Subscriber{NodeID: ks.NodeID(), Network: "testnet", PubKeyB64: ks.PubKeyBase64(), BBSName: "Old BBS", BBSHost: "old.example.net", Status: status}); err != nil {
				t.Fatal(err)
			}
			if _, err := h.areaSubscriptions.Upsert(ks.NodeID(), "testnet", "gen.general", "active"); err != nil {
				t.Fatal(err)
			}
			cfg := h.cfg
			if err := h.Close(); err != nil {
				t.Fatal(err)
			}
			reopened, err := New(cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			ts := httptest.NewServer(reopened.newMux())
			defer ts.Close()
			code, out := subscribeWithAreas(t, ts, ks, "gen.general")
			if code != http.StatusOK || out.Status != status {
				t.Fatalf("upgraded subscribe: %d %+v, want %s", code, out, status)
			}
			if active, err := reopened.areaSubscriptions.IsActive(ks.NodeID(), "testnet", "gen.general"); err != nil || !active {
				t.Fatalf("approval lost: active=%v, error=%v", active, err)
			}
			if status == "pending" {
				activateSubscriber(t, reopened, ks.NodeID(), "testnet")
				code, out = subscribeWithAreas(t, ts, ks, "gen.general")
				if code != http.StatusOK || out.Status != "active" || len(out.Areas) != 1 || out.Areas[0].Status != "active" {
					t.Fatalf("after manual approval: %d %+v", code, out)
				}
			}
			if requests, err := reopened.accessRequests.ListPending("testnet", "gen.general"); err != nil || len(requests) != 0 {
				t.Fatalf("duplicate approval requests: %+v, %v", requests, err)
			}
		})
	}
}

func TestSubscriberStore_RequestedAreasSurviveReload(t *testing.T) {
	ss := newTestStore(t)
	if _, err := ss.Add(Subscriber{
		NodeID: "n1", Network: "testnet", PubKeyB64: "k", Status: "pending",
		RequestedAreas: []string{"gen.general", "gen.private"},
	}); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := ss.loadCache(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	got := ss.Get("n1", "testnet")
	if got == nil || strings.Join(got.RequestedAreas, ",") != "gen.general,gen.private" {
		t.Errorf("requested areas after reload = %+v", got)
	}
}

func TestSubscriberStore_SetProfileUnlessBanned(t *testing.T) {
	ss := newTestStore(t)
	addTestSub(t, ss, "ok", "active")
	addTestSub(t, ss, "bad", "banned")

	if updated, err := ss.SetProfileUnlessBanned("ok", "testnet", "New", "new.example.net"); err != nil || !updated {
		t.Errorf("active node: updated=%v err=%v, want true", updated, err)
	}
	if s := ss.Get("ok", "testnet"); s.BBSName != "New" || s.BBSHost != "new.example.net" {
		t.Errorf("active node profile = %q/%q", s.BBSName, s.BBSHost)
	}

	before := *ss.Get("bad", "testnet")
	if updated, err := ss.SetProfileUnlessBanned("bad", "testnet", "New", "new.example.net"); err != nil || updated {
		t.Errorf("banned node: updated=%v err=%v, want false", updated, err)
	}
	if err := ss.loadCache(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if s := ss.Get("bad", "testnet"); s.BBSName != before.BBSName || s.BBSHost != before.BBSHost {
		t.Errorf("banned node profile changed to %q/%q", s.BBSName, s.BBSHost)
	}
}

// Upsert decides "never downgrade active" in the write itself, so an
// approval that lands after a caller read the old status is kept.
func TestAreaSubscriptionUpsert_KeepsActive(t *testing.T) {
	h, _ := setupTestHub(t)
	as := h.areaSubscriptions

	if got, err := as.Upsert("n1", "testnet", "gen.general", "pending"); err != nil || got != "pending" {
		t.Fatalf("new pending subscription: got %q, %v", got, err)
	}
	if err := as.SetStatus("n1", "testnet", "gen.general", "active"); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if got, err := as.Upsert("n1", "testnet", "gen.general", "pending"); err != nil || got != "active" {
		t.Errorf("stale pending upsert over active: got %q, %v; want active", got, err)
	}
	if active, _ := as.IsActive("n1", "testnet", "gen.general"); !active {
		t.Error("subscription was downgraded")
	}
	// Non-active statuses are still replaced.
	if err := as.SetStatus("n1", "testnet", "gen.general", "denied"); err != nil {
		t.Fatalf("deny: %v", err)
	}
	if got, err := as.Upsert("n1", "testnet", "gen.general", "pending"); err != nil || got != "pending" {
		t.Errorf("upsert over denied: got %q, %v; want pending", got, err)
	}
}

// A hub database from before requested_area_tags existed is migrated, and
// its rows load with no requested areas.
func TestSubscriberStore_MigratesRequestedAreasColumn(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "old.sqlite"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(subscribersSchema); err != nil {
		t.Fatalf("create old schema: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO subscribers (node_id, network, pubkey_b64, bbs_name, bbs_host, status)
		VALUES ('n1', 'testnet', 'k', 'Old BBS', 'old.example.net', 'active')`); err != nil {
		t.Fatalf("seed old row: %v", err)
	}

	ss, err := NewSubscriberStore(db)
	if err != nil {
		t.Fatalf("open store on old database: %v", err)
	}
	got := ss.Get("n1", "testnet")
	if got == nil || got.Status != "active" || got.BBSName != "Old BBS" || len(got.RequestedAreas) != 0 {
		t.Errorf("migrated row = %+v", got)
	}
	// Opening again finds the column already there.
	if _, err := NewSubscriberStore(db); err != nil {
		t.Errorf("reopen migrated database: %v", err)
	}
}

func TestSubscribe_UnsignedResubscribeChangesNothing(t *testing.T) {
	h, hubKS := setupTestHub(t)
	h.cfg.RequireSignedSubscribe = false
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

func TestSubscribe_LegacyLeafGetsAreasAfterManualApproval(t *testing.T) {
	h, hubKS := setupTestHubManual(t)
	h.cfg.RequireSignedSubscribe = false
	ts := httptest.NewServer(h.newMux())
	defer ts.Close()
	leafKS := loadTestKeystore(t, "leaf.key")
	seedNALWithAreas(t, h, hubKS, []protocol.Area{
		{Tag: "gen.general", Name: "General", Language: "en", Access: protocol.AreaAccess{Mode: protocol.AccessModeOpen}},
		{Tag: "gen.private", Name: "Private", Language: "en", Access: protocol.AreaAccess{Mode: protocol.AccessModeApproval}},
		{Tag: "gen.extra", Name: "Extra", Language: "en", Access: protocol.AreaAccess{Mode: protocol.AccessModeOpen}},
	})
	url := ts.URL + "/v3net/v1/subscribe"
	post := func(body string) protocol.SubscribeWithAreasResponse {
		t.Helper()
		resp, err := http.Post(url, "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatalf("POST subscribe: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("subscribe: expected 200, got %d", resp.StatusCode)
		}
		var out protocol.SubscribeWithAreasResponse
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return out
	}

	first := subscribeBody(t, leafKS, "Test BBS", "test.example.net", "gen.general", "gen.private")
	if out := post(first); out.Status != "pending" || len(out.Areas) != 0 {
		t.Fatalf("first subscribe = %+v, want pending with no areas", out)
	}
	activateSubscriber(t, h, leafKS.NodeID(), "testnet")

	// The retry also names an area the first request did not, and a
	// different BBS name; neither is taken from an unsigned request.
	retry := subscribeBody(t, leafKS, "Impostor", "evil.example.net", "gen.general", "gen.private", "gen.extra")
	out := post(retry)
	if out.Status != "active" || len(out.Areas) != 2 {
		t.Fatalf("retry after approval = %+v, want active with 2 areas", out)
	}
	want := map[string]string{"gen.general": "active", "gen.private": "pending"}
	subs, err := h.areaSubscriptions.ListForNode(leafKS.NodeID(), "testnet")
	if err != nil {
		t.Fatalf("list area subscriptions: %v", err)
	}
	if len(subs) != len(want) {
		t.Errorf("area subscriptions = %+v, want %v", subs, want)
	}
	for _, sub := range subs {
		if want[sub.Tag] != sub.Status {
			t.Errorf("area %s = %q, want %q", sub.Tag, sub.Status, want[sub.Tag])
		}
	}
	pending, err := h.accessRequests.ListPending("testnet", "gen.private")
	if err != nil {
		t.Fatalf("list access requests: %v", err)
	}
	if len(pending) != 1 || pending[0].BBSName != "Test BBS" {
		t.Errorf("access requests = %+v, want one under the registered name", pending)
	}
	assertProfile(t, h, leafKS.NodeID(), "Test BBS", "test.example.net")
}
