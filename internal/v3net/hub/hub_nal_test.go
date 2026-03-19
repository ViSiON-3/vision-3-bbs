package hub

import (
	"encoding/json"
	"fmt"
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

func seedTestNAL(t *testing.T, h *Hub, ks *keystore.Keystore) {
	t.Helper()
	testNAL := &protocol.NAL{
		V3NetNAL:       "1.0",
		Network:        "testnet",
		CoordNodeID:    ks.NodeID(),
		CoordPubKeyB64: ks.PubKeyBase64(),
		Areas: []protocol.Area{
			{
				Tag:              "gen.general",
				Name:             "General",
				Description:      "General discussion",
				Language:         "en",
				ManagerNodeID:    ks.NodeID(),
				ManagerPubKeyB64: ks.PubKeyBase64(),
				Access:           protocol.AreaAccess{Mode: protocol.AccessModeOpen},
				Policy:           protocol.AreaPolicy{MaxBodyBytes: 65536, AllowANSI: true},
			},
		},
	}
	if err := nal.Sign(testNAL, ks); err != nil {
		t.Fatalf("sign test NAL: %v", err)
	}
	if err := h.nalStore.Put("testnet", testNAL); err != nil {
		t.Fatalf("put test NAL: %v", err)
	}
}

func TestGetNAL_Public(t *testing.T) {
	h, hubKS := setupTestHub(t)
	ts := httptest.NewServer(h.newMux())
	defer ts.Close()

	seedTestNAL(t, h, hubKS)

	resp, err := http.Get(ts.URL + "/v3net/v1/testnet/nal")
	if err != nil {
		t.Fatalf("GET /nal: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var got protocol.NAL
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode NAL: %v", err)
	}
	if got.Network != "testnet" {
		t.Errorf("expected network testnet, got %q", got.Network)
	}
	if got.CoordNodeID != hubKS.NodeID() {
		t.Errorf("expected coord node ID %q, got %q", hubKS.NodeID(), got.CoordNodeID)
	}
	if len(got.Areas) != 1 {
		t.Errorf("expected 1 area, got %d", len(got.Areas))
	}
}

func TestGetNAL_NotFound(t *testing.T) {
	h, _ := setupTestHub(t)
	ts := httptest.NewServer(h.newMux())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/v3net/v1/testnet/nal")
	if err != nil {
		t.Fatalf("GET /nal: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestPostNAL_Coordinator(t *testing.T) {
	h, hubKS := setupTestHub(t)
	ts := httptest.NewServer(h.newMux())
	defer ts.Close()

	// Register the hub keystore as a subscriber so auth passes.
	body := fmt.Sprintf(`{"network":"testnet","node_id":%q,"pubkey_b64":%q,"bbs_name":"Hub BBS","bbs_host":"hub.example.net"}`,
		hubKS.NodeID(), hubKS.PubKeyBase64())
	resp, err := http.Post(ts.URL+"/v3net/v1/subscribe", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("subscribe hub: %v", err)
	}
	resp.Body.Close()

	seedTestNAL(t, h, hubKS)

	// Build an updated NAL signed by the hub (coordinator).
	updatedNAL := &protocol.NAL{
		V3NetNAL:       "1.0",
		Network:        "testnet",
		CoordNodeID:    hubKS.NodeID(),
		CoordPubKeyB64: hubKS.PubKeyBase64(),
		Areas: []protocol.Area{
			{
				Tag:              "gen.general",
				Name:             "General",
				Description:      "General discussion updated",
				Language:         "en",
				ManagerNodeID:    hubKS.NodeID(),
				ManagerPubKeyB64: hubKS.PubKeyBase64(),
				Access:           protocol.AreaAccess{Mode: protocol.AccessModeOpen},
				Policy:           protocol.AreaPolicy{MaxBodyBytes: 65536, AllowANSI: true},
			},
		},
	}
	if err := nal.Sign(updatedNAL, hubKS); err != nil {
		t.Fatalf("sign updated NAL: %v", err)
	}

	nalJSON, _ := json.Marshal(updatedNAL)
	req := signedRequest(t, hubKS, "POST", ts.URL+"/v3net/v1/testnet/nal", string(nalJSON))
	postResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /nal: %v", err)
	}
	defer postResp.Body.Close()

	if postResp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", postResp.StatusCode)
	}
}

func TestPostNAL_NonCoordinator(t *testing.T) {
	h, hubKS := setupTestHub(t)
	ts := httptest.NewServer(h.newMux())
	defer ts.Close()

	leafKS, _, err := keystore.Load(filepath.Join(t.TempDir(), "leaf.key"))
	if err != nil {
		t.Fatalf("load leaf keystore: %v", err)
	}
	registerLeaf(t, ts, leafKS)

	// Seed NAL with hub as coordinator.
	seedTestNAL(t, h, hubKS)

	// Leaf tries to POST a NAL — should be forbidden.
	fakeNAL := &protocol.NAL{
		V3NetNAL:       "1.0",
		Network:        "testnet",
		CoordNodeID:    leafKS.NodeID(),
		CoordPubKeyB64: leafKS.PubKeyBase64(),
		Areas: []protocol.Area{
			{
				Tag:              "gen.general",
				Name:             "General",
				Description:      "Hijacked",
				Language:         "en",
				ManagerNodeID:    leafKS.NodeID(),
				ManagerPubKeyB64: leafKS.PubKeyBase64(),
				Access:           protocol.AreaAccess{Mode: protocol.AccessModeOpen},
				Policy:           protocol.AreaPolicy{MaxBodyBytes: 65536, AllowANSI: true},
			},
		},
	}
	if err := nal.Sign(fakeNAL, leafKS); err != nil {
		t.Fatalf("sign fake NAL: %v", err)
	}

	nalJSON, _ := json.Marshal(fakeNAL)
	req := signedRequest(t, leafKS, "POST", ts.URL+"/v3net/v1/testnet/nal", string(nalJSON))
	postResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /nal: %v", err)
	}
	defer postResp.Body.Close()

	if postResp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403, got %d", postResp.StatusCode)
	}
}

func TestPostNAL_InitialCreation_HubOnly(t *testing.T) {
	h, _ := setupTestHub(t)
	ts := httptest.NewServer(h.newMux())
	defer ts.Close()

	leafKS, _, err := keystore.Load(filepath.Join(t.TempDir(), "leaf.key"))
	if err != nil {
		t.Fatalf("load leaf keystore: %v", err)
	}
	registerLeaf(t, ts, leafKS)

	// Leaf tries to create initial NAL — should be forbidden.
	initialNAL := &protocol.NAL{
		V3NetNAL:       "1.0",
		Network:        "testnet",
		CoordNodeID:    leafKS.NodeID(),
		CoordPubKeyB64: leafKS.PubKeyBase64(),
		Areas: []protocol.Area{
			{
				Tag:              "gen.general",
				Name:             "General",
				Description:      "Attempted initial creation",
				Language:         "en",
				ManagerNodeID:    leafKS.NodeID(),
				ManagerPubKeyB64: leafKS.PubKeyBase64(),
				Access:           protocol.AreaAccess{Mode: protocol.AccessModeOpen},
				Policy:           protocol.AreaPolicy{MaxBodyBytes: 65536, AllowANSI: true},
			},
		},
	}
	if err := nal.Sign(initialNAL, leafKS); err != nil {
		t.Fatalf("sign initial NAL: %v", err)
	}

	nalJSON, _ := json.Marshal(initialNAL)
	req := signedRequest(t, leafKS, "POST", ts.URL+"/v3net/v1/testnet/nal", string(nalJSON))
	postResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /nal: %v", err)
	}
	defer postResp.Body.Close()

	if postResp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403, got %d", postResp.StatusCode)
	}
}

func TestAuthClockSkew_Rejected(t *testing.T) {
	h, _ := setupTestHub(t)
	ts := httptest.NewServer(h.newMux())
	defer ts.Close()

	leafKS, _, err := keystore.Load(filepath.Join(t.TempDir(), "leaf.key"))
	if err != nil {
		t.Fatalf("load leaf keystore: %v", err)
	}
	registerLeaf(t, ts, leafKS)

	req := signedRequest(t, leafKS, "GET", ts.URL+"/v3net/v1/testnet/messages", "")
	// Override date to 10 minutes ago — clock skew check happens before signature verification.
	req.Header.Set("Date", time.Now().Add(-10*time.Minute).UTC().Format(http.TimeFormat))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /messages: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 for clock skew, got %d", resp.StatusCode)
	}
}

func TestGetMessages_Pagination(t *testing.T) {
	h, hubKS := setupTestHub(t)
	ts := httptest.NewServer(h.newMux())
	defer ts.Close()

	seedTestNAL(t, h, hubKS)

	leafKS, _, err := keystore.Load(filepath.Join(t.TempDir(), "leaf.key"))
	if err != nil {
		t.Fatalf("load leaf keystore: %v", err)
	}
	registerLeafWithAreas(t, ts, leafKS, []string{"gen.general"})

	// Post 3 messages with unique UUIDs.
	uuids := []string{
		"a50e8400-e29b-41d4-a716-446655440001",
		"b50e8400-e29b-41d4-a716-446655440002",
		"c50e8400-e29b-41d4-a716-446655440003",
	}
	for i, uuid := range uuids {
		msgJSON := fmt.Sprintf(`{
			"v3net":"1.0",
			"network":"testnet",
			"area_tag":"gen.general",
			"msg_uuid":%q,
			"thread_uuid":%q,
			"origin_node":"test.example.net",
			"origin_board":"General",
			"from":"Tester",
			"to":"All",
			"subject":"Message %d",
			"date_utc":"2026-03-16T04:20:00Z",
			"body":"Body %d",
			"kludges":{}
		}`, uuid, uuid, i+1, i+1)

		req := signedRequest(t, leafKS, "POST", ts.URL+"/v3net/v1/testnet/messages", msgJSON)
		resp, postErr := http.DefaultClient.Do(req)
		if postErr != nil {
			t.Fatalf("POST message %d: %v", i+1, postErr)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("POST message %d status: %d", i+1, resp.StatusCode)
		}
	}

	// GET with limit=2 — should return 2 messages and X-V3Net-Has-More: true.
	getReq := signedRequest(t, leafKS, "GET", ts.URL+"/v3net/v1/testnet/messages?limit=2", "")
	resp, err := http.DefaultClient.Do(getReq)
	if err != nil {
		t.Fatalf("GET messages limit=2: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET messages status: %d", resp.StatusCode)
	}

	hasMore := resp.Header.Get("X-V3Net-Has-More")
	if hasMore != "true" {
		t.Errorf("expected X-V3Net-Has-More: true, got %q", hasMore)
	}

	var batch1 []protocol.Message
	if err := json.NewDecoder(resp.Body).Decode(&batch1); err != nil {
		t.Fatalf("decode batch 1: %v", err)
	}
	if len(batch1) != 2 {
		t.Fatalf("expected 2 messages in batch 1, got %d", len(batch1))
	}

	// GET with since=last UUID from first batch — should return remaining message(s).
	sinceUUID := batch1[len(batch1)-1].MsgUUID
	getReq2 := signedRequest(t, leafKS, "GET",
		fmt.Sprintf("%s/v3net/v1/testnet/messages?since=%s&limit=10", ts.URL, sinceUUID), "")
	resp2, err := http.DefaultClient.Do(getReq2)
	if err != nil {
		t.Fatalf("GET messages since: %v", err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("GET messages since status: %d", resp2.StatusCode)
	}

	var batch2 []protocol.Message
	if err := json.NewDecoder(resp2.Body).Decode(&batch2); err != nil {
		t.Fatalf("decode batch 2: %v", err)
	}
	if len(batch2) != 1 {
		t.Errorf("expected 1 message in batch 2, got %d", len(batch2))
	}

	hasMore2 := resp2.Header.Get("X-V3Net-Has-More")
	if hasMore2 == "true" {
		t.Errorf("expected no X-V3Net-Has-More on final batch, got %q", hasMore2)
	}
}
