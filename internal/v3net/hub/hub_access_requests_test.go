package hub

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/keystore"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// setupAccessTest starts a hub whose operator manages one approval-mode area,
// gen.general, and registers both the operator and a leaf as subscribers.
func setupAccessTest(t *testing.T) (*Hub, *httptest.Server, *keystore.Keystore, *keystore.Keystore) {
	t.Helper()
	h, hubKS := setupTestHub(t)
	ts := httptest.NewServer(h.newMux())
	t.Cleanup(ts.Close)
	leafKS := loadTestKeystore(t, "leaf.key")
	registerHubAsLeaf(t, ts, hubKS)
	registerLeaf(t, ts, leafKS)
	seedNALWithAreas(t, h, hubKS, []protocol.Area{
		{
			Tag:      "gen.general",
			Name:     "General",
			Language: "en",
			Access:   protocol.AreaAccess{Mode: protocol.AccessModeApproval},
		},
	})
	return h, ts, hubKS, leafKS
}

func accessRequestStatus(t *testing.T, h *Hub, nodeID string) string {
	t.Helper()
	var status string
	if err := h.db.QueryRow(
		`SELECT status FROM area_access_requests WHERE network = 'testnet' AND area_tag = 'gen.general' AND node_id = ?`,
		nodeID,
	).Scan(&status); err != nil {
		t.Fatalf("read access request status: %v", err)
	}
	return status
}

func areaSubscriptionStatus(t *testing.T, h *Hub, nodeID string) string {
	t.Helper()
	subs, err := h.areaSubscriptions.ListForNode(nodeID, "testnet")
	if err != nil {
		t.Fatalf("list area subscriptions: %v", err)
	}
	for _, s := range subs {
		if s.Tag == "gen.general" {
			return s.Status
		}
	}
	return ""
}

func TestAccessRequests_ListApproveFlow(t *testing.T) {
	h, ts, hubKS, leafKS := setupAccessTest(t)
	base := ts.URL + "/v3net/v1/testnet/areas/gen.general/access"

	// No requests yet: the manager sees an empty JSON array, not null.
	resp, err := http.DefaultClient.Do(signedRequest(t, hubKS, "GET", base+"/requests", ""))
	if err != nil {
		t.Fatalf("GET requests: %v", err)
	}
	var raw json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || strings.TrimSpace(string(raw)) != "[]" {
		t.Errorf("empty list: status %d body %s, want 200 []", resp.StatusCode, raw)
	}

	ch, cancel := h.broadcaster.Subscribe("testnet")
	defer cancel()

	// The leaf asks for the approval-mode area; asking twice must not
	// produce a second pending request.
	registerLeafWithAreas(t, ts, leafKS, []string{"gen.general"})
	registerLeafWithAreas(t, ts, leafKS, []string{"gen.general"})

	var requested protocol.AreaAccessRequestedPayload
	if err := json.Unmarshal(waitEvent(t, ch, protocol.EventAreaAccessRequested).Data, &requested); err != nil {
		t.Fatalf("decode access-requested event: %v", err)
	}
	if requested.Tag != "gen.general" || requested.NodeID != leafKS.NodeID() || requested.BBSName != "Test BBS" {
		t.Errorf("unexpected access-requested payload: %+v", requested)
	}

	// Non-managers may not see the queue.
	if code := sendSigned(t, leafKS, "GET", base+"/requests", "", nil); code != http.StatusForbidden {
		t.Errorf("list as non-manager: expected 403, got %d", code)
	}

	var reqs []protocol.AccessRequest
	if code := sendSigned(t, hubKS, "GET", base+"/requests", "", &reqs); code != http.StatusOK {
		t.Fatalf("list as manager: expected 200, got %d", code)
	}
	if len(reqs) != 1 {
		t.Fatalf("expected 1 pending request, got %d: %+v", len(reqs), reqs)
	}
	if reqs[0].NodeID != leafKS.NodeID() || reqs[0].BBSName != "Test BBS" || reqs[0].RequestedAt == "" {
		t.Errorf("unexpected pending request: %+v", reqs[0])
	}
	if got := areaSubscriptionStatus(t, h, leafKS.NodeID()); got != "pending" {
		t.Errorf("area subscription before approval = %q, want pending", got)
	}

	// Approve with a padded, duplicated ID: it is normalised to one entry.
	body := fmt.Sprintf(`{"node_ids":[" %s ",%q,""]}`, leafKS.NodeID(), leafKS.NodeID())
	if code := sendSigned(t, hubKS, "POST", base+"/approve", body, nil); code != http.StatusOK {
		t.Fatalf("approve: expected 200, got %d", code)
	}
	// Approving again must not grow the allow list.
	if code := sendSigned(t, hubKS, "POST", base+"/approve", body, nil); code != http.StatusOK {
		t.Fatalf("re-approve: expected 200, got %d", code)
	}

	area := fetchNAL(t, ts).FindArea("gen.general")
	if len(area.Access.AllowList) != 1 || area.Access.AllowList[0] != leafKS.NodeID() {
		t.Errorf("allow list = %v, want exactly [%s]", area.Access.AllowList, leafKS.NodeID())
	}
	if got := accessRequestStatus(t, h, leafKS.NodeID()); got != "approved" {
		t.Errorf("access request status = %q, want approved", got)
	}
	if got := areaSubscriptionStatus(t, h, leafKS.NodeID()); got != "active" {
		t.Errorf("area subscription after approval = %q, want active", got)
	}
	reqs = nil
	sendSigned(t, hubKS, "GET", base+"/requests", "", &reqs)
	if len(reqs) != 0 {
		t.Errorf("expected no pending requests after approval, got %+v", reqs)
	}
}

func TestDenyAccess_ResolvesRequestAndNotifies(t *testing.T) {
	h, ts, hubKS, leafKS := setupAccessTest(t)
	base := ts.URL + "/v3net/v1/testnet/areas/gen.general/access"
	nodeIDs := fmt.Sprintf(`{"node_ids":[%q]}`, leafKS.NodeID())

	registerLeafWithAreas(t, ts, leafKS, []string{"gen.general"})
	if code := sendSigned(t, hubKS, "POST", base+"/approve", nodeIDs, nil); code != http.StatusOK {
		t.Fatalf("approve: expected 200, got %d", code)
	}

	ch, cancel := h.broadcaster.Subscribe("testnet")
	defer cancel()

	if code := sendSigned(t, hubKS, "POST", base+"/deny", nodeIDs, nil); code != http.StatusOK {
		t.Fatalf("deny: expected 200, got %d", code)
	}

	// The NAL change is announced first, then the denial itself.
	var updated protocol.NALUpdatedPayload
	if err := json.Unmarshal(waitEvent(t, ch, protocol.EventNALUpdated).Data, &updated); err != nil {
		t.Fatalf("decode nal_updated: %v", err)
	}
	if updated.Network != "testnet" || updated.AreaCount != 1 {
		t.Errorf("unexpected nal_updated payload: %+v", updated)
	}
	var denied protocol.SubscriptionDeniedPayload
	if err := json.Unmarshal(waitEvent(t, ch, protocol.EventSubscriptionDenied).Data, &denied); err != nil {
		t.Fatalf("decode subscription_denied: %v", err)
	}
	want := protocol.SubscriptionDeniedPayload{Network: "testnet", Tag: "gen.general", NodeID: leafKS.NodeID()}
	if denied != want {
		t.Errorf("subscription_denied = %+v, want %+v", denied, want)
	}

	// Denial moves the node from the allow list to the deny list.
	area := fetchNAL(t, ts).FindArea("gen.general")
	if len(area.Access.AllowList) != 0 {
		t.Errorf("allow list should be empty after deny, got %v", area.Access.AllowList)
	}
	if len(area.Access.DenyList) != 1 || area.Access.DenyList[0] != leafKS.NodeID() {
		t.Errorf("deny list = %v, want [%s]", area.Access.DenyList, leafKS.NodeID())
	}
	if got := accessRequestStatus(t, h, leafKS.NodeID()); got != "denied" {
		t.Errorf("access request status = %q, want denied", got)
	}
	if got := areaSubscriptionStatus(t, h, leafKS.NodeID()); got != "denied" {
		t.Errorf("area subscription = %q, want denied", got)
	}

	// Denying again must not duplicate the deny-list entry.
	sendSigned(t, hubKS, "POST", base+"/deny", nodeIDs, nil)
	if dl := fetchNAL(t, ts).FindArea("gen.general").Access.DenyList; len(dl) != 1 {
		t.Errorf("deny list after second deny = %v, want 1 entry", dl)
	}
}

func TestAccessEndpoints_Validation(t *testing.T) {
	h, ts, hubKS, leafKS := setupAccessTest(t)
	base := ts.URL + "/v3net/v1/testnet/areas/gen.general/access"
	validIDs := fmt.Sprintf(`{"node_ids":[%q]}`, leafKS.NodeID())

	cases := []struct {
		name   string
		signer *keystore.Keystore
		path   string
		body   string
		want   int
	}{
		{"mode non-manager", leafKS, "/mode", `{"mode":"open"}`, http.StatusForbidden},
		{"mode invalid JSON", hubKS, "/mode", `{"mode":`, http.StatusBadRequest},
		{"mode unknown value", hubKS, "/mode", `{"mode":"secret"}`, http.StatusUnprocessableEntity},
		{"approve non-manager", leafKS, "/approve", validIDs, http.StatusForbidden},
		{"approve invalid JSON", hubKS, "/approve", `[`, http.StatusBadRequest},
		{"approve blank IDs", hubKS, "/approve", `{"node_ids":["", "  "]}`, http.StatusBadRequest},
		{"deny non-manager", leafKS, "/deny", validIDs, http.StatusForbidden},
		{"deny invalid JSON", hubKS, "/deny", `[`, http.StatusBadRequest},
		{"deny no IDs", hubKS, "/deny", `{"node_ids":[]}`, http.StatusBadRequest},
		{"remove non-manager", leafKS, "/remove", validIDs, http.StatusForbidden},
		{"remove invalid JSON", hubKS, "/remove", `[`, http.StatusBadRequest},
		{"remove no IDs", hubKS, "/remove", `{}`, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if code := sendSigned(t, tc.signer, "POST", base+tc.path, tc.body, nil); code != tc.want {
				t.Errorf("expected %d, got %d", tc.want, code)
			}
		})
	}

	// None of the rejected requests may have altered the area.
	stored, err := h.nalStore.Get("testnet")
	if err != nil {
		t.Fatalf("get NAL: %v", err)
	}
	area := stored.FindArea("gen.general")
	if area.Access.Mode != protocol.AccessModeApproval || len(area.Access.AllowList) != 0 || len(area.Access.DenyList) != 0 {
		t.Errorf("rejected requests changed area access: %+v", area.Access)
	}
}

func TestAccessEndpoints_UnknownAreaIsForbidden(t *testing.T) {
	_, ts, hubKS, _ := setupAccessTest(t)
	// Nobody manages an area that is not in the NAL, so every caller is refused.
	url := ts.URL + "/v3net/v1/testnet/areas/gen.missing/access"
	if code := sendSigned(t, hubKS, "GET", url, "", nil); code != http.StatusForbidden {
		t.Errorf("GET access for unknown area: expected 403, got %d", code)
	}
	if code := sendSigned(t, hubKS, "POST", url+"/mode", `{"mode":"open"}`, nil); code != http.StatusForbidden {
		t.Errorf("set mode for unknown area: expected 403, got %d", code)
	}
}

func TestListAccessRequests_StorageError(t *testing.T) {
	h, ts, hubKS, _ := setupAccessTest(t)
	if _, err := h.db.Exec(`DROP TABLE area_access_requests`); err != nil {
		t.Fatalf("drop table: %v", err)
	}
	url := ts.URL + "/v3net/v1/testnet/areas/gen.general/access/requests"
	if code := sendSigned(t, hubKS, "GET", url, "", nil); code != http.StatusInternalServerError {
		t.Errorf("expected 500 when the requests table is gone, got %d", code)
	}
}

func TestUpdateNALArea_Errors(t *testing.T) {
	h, hubKS := setupTestHub(t)
	mutated := false
	mutate := func(*protocol.Area) { mutated = true }

	if err := h.updateNALArea("testnet", "gen.general", mutate); err == nil {
		t.Error("expected error when no NAL exists")
	}
	seedTestNAL(t, h, hubKS)
	if err := h.updateNALArea("testnet", "gen.missing", mutate); err == nil {
		t.Error("expected error for an area not in the NAL")
	}
	if mutated {
		t.Error("mutate must not run when the area cannot be found")
	}
}

func TestAccessRequestStore(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	store, err := NewAccessRequestStore(db)
	if err != nil {
		t.Fatalf("NewAccessRequestStore: %v", err)
	}

	id1, err := store.Add("net", "gen.a", "node1", "BBS One")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	// A repeat request while still pending returns the same ID.
	if again, err := store.Add("net", "gen.a", "node1", "BBS One"); err != nil || again != id1 {
		t.Errorf("repeat Add = %q, %v; want %q", again, err, id1)
	}
	if _, err := store.Add("net", "gen.a", "node2", ""); err != nil {
		t.Fatalf("Add node2: %v", err)
	}
	if _, err := store.Add("net", "gen.b", "node1", "BBS One"); err != nil {
		t.Fatalf("Add gen.b: %v", err)
	}

	pending, err := store.ListPending("net", "gen.a")
	if err != nil {
		t.Fatalf("ListPending: %v", err)
	}
	if len(pending) != 2 {
		t.Fatalf("expected 2 pending for gen.a, got %+v", pending)
	}

	if err := store.Resolve("net", "gen.a", "node1", "denied"); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	pending, _ = store.ListPending("net", "gen.a")
	if len(pending) != 1 || pending[0].NodeID != "node2" {
		t.Errorf("after resolve, pending = %+v, want only node2", pending)
	}

	// A resolved node may ask again; the old row is replaced with a fresh
	// pending request under a new ID.
	id2, err := store.Add("net", "gen.a", "node1", "BBS One")
	if err != nil {
		t.Fatalf("re-Add: %v", err)
	}
	if id2 == id1 {
		t.Error("re-request after resolution should get a new ID")
	}
	if pending, _ = store.ListPending("net", "gen.a"); len(pending) != 2 {
		t.Errorf("expected 2 pending after re-request, got %+v", pending)
	}

	db.Close()
	if _, err := store.Add("net", "gen.a", "node3", ""); err == nil {
		t.Error("Add on closed DB should fail")
	}
	if _, err := store.ListPending("net", "gen.a"); err == nil {
		t.Error("ListPending on closed DB should fail")
	}
	if err := store.Resolve("net", "gen.a", "node1", "approved"); err == nil {
		t.Error("Resolve on closed DB should fail")
	}
	if _, err := NewAccessRequestStore(db); err == nil {
		t.Error("NewAccessRequestStore on closed DB should fail")
	}
}
