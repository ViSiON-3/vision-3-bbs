package hub

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/keystore"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/nal"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// seedAreaMsgNAL seeds a NAL with two areas: gen.general (open) and gen.restricted (approval).
func seedAreaMsgNAL(t *testing.T, h *Hub, ks *keystore.Keystore) {
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
				Language:         "en",
				ManagerNodeID:    ks.NodeID(),
				ManagerPubKeyB64: ks.PubKeyBase64(),
				Access:           protocol.AreaAccess{Mode: protocol.AccessModeOpen},
				Policy:           protocol.AreaPolicy{MaxBodyBytes: 65536, AllowANSI: true},
			},
			{
				Tag:              "gen.restricted",
				Name:             "Restricted",
				Language:         "en",
				ManagerNodeID:    ks.NodeID(),
				ManagerPubKeyB64: ks.PubKeyBase64(),
				Access:           protocol.AreaAccess{Mode: protocol.AccessModeApproval},
				Policy:           protocol.AreaPolicy{MaxBodyBytes: 65536, AllowANSI: true},
			},
		},
	}
	if err := nal.Sign(testNAL, ks); err != nil {
		t.Fatalf("sign area msg NAL: %v", err)
	}
	if err := h.nalStore.Put("testnet", testNAL); err != nil {
		t.Fatalf("put area msg NAL: %v", err)
	}
}

// registerLeafWithAreas subscribes a leaf node and requests area subscriptions.
func registerLeafWithAreas(t *testing.T, ts *httptest.Server, leafKS *keystore.Keystore, areaTags []string) {
	t.Helper()
	req := protocol.SubscribeRequest{
		Network:   "testnet",
		NodeID:    leafKS.NodeID(),
		PubKeyB64: leafKS.PubKeyBase64(),
		BBSName:   "Test BBS",
		BBSHost:   "test.example.net",
		AreaTags:  areaTags,
	}
	body, _ := json.Marshal(req)
	resp, err := http.Post(ts.URL+"/v3net/v1/subscribe", "application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("subscribe with areas: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("subscribe with areas status: %d", resp.StatusCode)
	}
}

// testMsgJSON returns a valid message JSON for the given area tag and UUID.
func testMsgJSON(areaTag, uuid string) string {
	return fmt.Sprintf(`{
		"v3net": "1.0",
		"network": "testnet",
		"area_tag": %q,
		"msg_uuid": %q,
		"thread_uuid": %q,
		"origin_node": "test.example.net",
		"origin_board": "General",
		"from": "Tester",
		"to": "All",
		"subject": "Test message",
		"date_utc": "2026-03-16T04:20:00Z",
		"body": "Hello V3Net!",
		"kludges": {}
	}`, areaTag, uuid, uuid)
}

func TestMessageStore_StoreWithAreaTag(t *testing.T) {
	h, _ := setupTestHub(t)

	ok, err := h.messages.Store("uuid-001", "testnet", "gen.general", `{"test":"data"}`)
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	if !ok {
		t.Fatal("expected new message to be stored")
	}
}

func TestPostMessage_NoNAL_Returns422(t *testing.T) {
	h, _ := setupTestHub(t)
	ts := httptest.NewServer(h.newMux())
	defer ts.Close()

	leafKS, _, err := keystore.Load(filepath.Join(t.TempDir(), "leaf.key"))
	if err != nil {
		t.Fatalf("load leaf keystore: %v", err)
	}
	// Register leaf without area subscriptions (no NAL seeded).
	registerLeaf(t, ts, leafKS)

	msgJSON := testMsgJSON("gen.general", "aa0e8400-e29b-41d4-a716-446655440001")
	req := signedRequest(t, leafKS, "POST", ts.URL+"/v3net/v1/testnet/messages", msgJSON)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("expected 422, got %d", resp.StatusCode)
	}
}

func TestPostMessage_UnknownArea_Returns422(t *testing.T) {
	h, hubKS := setupTestHub(t)
	ts := httptest.NewServer(h.newMux())
	defer ts.Close()

	leafKS, _, err := keystore.Load(filepath.Join(t.TempDir(), "leaf.key"))
	if err != nil {
		t.Fatalf("load leaf keystore: %v", err)
	}
	seedAreaMsgNAL(t, h, hubKS)
	registerLeafWithAreas(t, ts, leafKS, []string{"gen.general"})

	// Post to an area that doesn't exist in the NAL.
	msgJSON := testMsgJSON("gen.nonexistent", "ab0e8400-e29b-41d4-a716-446655440002")
	req := signedRequest(t, leafKS, "POST", ts.URL+"/v3net/v1/testnet/messages", msgJSON)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("expected 422, got %d", resp.StatusCode)
	}
}

func TestPostMessage_NotSubscribed_Returns403(t *testing.T) {
	h, hubKS := setupTestHub(t)
	ts := httptest.NewServer(h.newMux())
	defer ts.Close()

	leafKS, _, err := keystore.Load(filepath.Join(t.TempDir(), "leaf.key"))
	if err != nil {
		t.Fatalf("load leaf keystore: %v", err)
	}
	seedAreaMsgNAL(t, h, hubKS)
	// Register without subscribing to gen.general.
	registerLeaf(t, ts, leafKS)

	msgJSON := testMsgJSON("gen.general", "ac0e8400-e29b-41d4-a716-446655440003")
	req := signedRequest(t, leafKS, "POST", ts.URL+"/v3net/v1/testnet/messages", msgJSON)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403, got %d", resp.StatusCode)
	}
}

func TestPostMessage_ActiveSubscription_Returns200(t *testing.T) {
	h, hubKS := setupTestHub(t)
	ts := httptest.NewServer(h.newMux())
	defer ts.Close()

	leafKS, _, err := keystore.Load(filepath.Join(t.TempDir(), "leaf.key"))
	if err != nil {
		t.Fatalf("load leaf keystore: %v", err)
	}
	seedAreaMsgNAL(t, h, hubKS)
	registerLeafWithAreas(t, ts, leafKS, []string{"gen.general"})

	msgJSON := testMsgJSON("gen.general", "ad0e8400-e29b-41d4-a716-446655440004")
	req := signedRequest(t, leafKS, "POST", ts.URL+"/v3net/v1/testnet/messages", msgJSON)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestGetMessages_FiltersToSubscribedAreas(t *testing.T) {
	h, hubKS := setupTestHub(t)
	ts := httptest.NewServer(h.newMux())
	defer ts.Close()

	leafKS, _, err := keystore.Load(filepath.Join(t.TempDir(), "leaf.key"))
	if err != nil {
		t.Fatalf("load leaf keystore: %v", err)
	}
	seedAreaMsgNAL(t, h, hubKS)

	// Directly store messages bypassing POST enforcement.
	h.messages.Store("ae0e8400-e29b-41d4-a716-000000000001", "testnet", "gen.general", testMsgJSON("gen.general", "ae0e8400-e29b-41d4-a716-000000000001"))
	h.messages.Store("ae0e8400-e29b-41d4-a716-000000000002", "testnet", "gen.general", testMsgJSON("gen.general", "ae0e8400-e29b-41d4-a716-000000000002"))
	h.messages.Store("ae0e8400-e29b-41d4-a716-000000000003", "testnet", "gen.restricted", testMsgJSON("gen.restricted", "ae0e8400-e29b-41d4-a716-000000000003"))

	// Register leaf only with gen.general subscription.
	registerLeafWithAreas(t, ts, leafKS, []string{"gen.general"})

	getReq := signedRequest(t, leafKS, "GET", ts.URL+"/v3net/v1/testnet/messages", "")
	resp, err := http.DefaultClient.Do(getReq)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET status: %d", resp.StatusCode)
	}

	var messages []protocol.Message
	if err := json.NewDecoder(resp.Body).Decode(&messages); err != nil {
		t.Fatalf("decode messages: %v", err)
	}
	if len(messages) != 2 {
		t.Errorf("expected 2 messages (gen.general only), got %d", len(messages))
	}
	for _, m := range messages {
		if m.AreaTag != "gen.general" {
			t.Errorf("expected only gen.general messages, got area_tag %q", m.AreaTag)
		}
	}
}

func TestGetMessages_NoSubscriptions_ReturnsEmpty(t *testing.T) {
	h, hubKS := setupTestHub(t)
	ts := httptest.NewServer(h.newMux())
	defer ts.Close()

	leafKS, _, err := keystore.Load(filepath.Join(t.TempDir(), "leaf.key"))
	if err != nil {
		t.Fatalf("load leaf keystore: %v", err)
	}
	seedAreaMsgNAL(t, h, hubKS)

	// Store a message directly.
	h.messages.Store("af0e8400-e29b-41d4-a716-000000000001", "testnet", "gen.general", testMsgJSON("gen.general", "af0e8400-e29b-41d4-a716-000000000001"))

	// Register leaf without any area subscriptions.
	registerLeaf(t, ts, leafKS)

	getReq := signedRequest(t, leafKS, "GET", ts.URL+"/v3net/v1/testnet/messages", "")
	resp, err := http.DefaultClient.Do(getReq)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET status: %d", resp.StatusCode)
	}

	var messages []json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&messages); err != nil {
		t.Fatalf("decode messages: %v", err)
	}
	if len(messages) != 0 {
		t.Errorf("expected 0 messages for node with no subscriptions, got %d", len(messages))
	}
}

func TestMessageStore_FetchFiltersByAreaTags(t *testing.T) {
	h, _ := setupTestHub(t)

	// Store messages in two areas.
	h.messages.Store("b00e8400-0001", "testnet", "gen.general", `{"msg_uuid":"b00e8400-0001","area_tag":"gen.general"}`)
	h.messages.Store("b00e8400-0002", "testnet", "gen.general", `{"msg_uuid":"b00e8400-0002","area_tag":"gen.general"}`)
	h.messages.Store("b00e8400-0003", "testnet", "gen.restricted", `{"msg_uuid":"b00e8400-0003","area_tag":"gen.restricted"}`)

	// Fetch only gen.general.
	results, hasMore, err := h.messages.Fetch("testnet", "", 100, []string{"gen.general"})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if hasMore {
		t.Error("expected hasMore=false")
	}
	if len(results) != 2 {
		t.Errorf("expected 2 results, got %d", len(results))
	}

	// Fetch both areas.
	all, _, err := h.messages.Fetch("testnet", "", 100, []string{"gen.general", "gen.restricted"})
	if err != nil {
		t.Fatalf("Fetch all: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("expected 3 results, got %d", len(all))
	}
}

func TestMessageStore_FetchEmptyAreaTagsReturnsNil(t *testing.T) {
	h, _ := setupTestHub(t)

	h.messages.Store("b10e8400-0001", "testnet", "gen.general", `{"msg_uuid":"b10e8400-0001"}`)

	results, hasMore, err := h.messages.Fetch("testnet", "", 100, []string{})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if hasMore {
		t.Error("expected hasMore=false")
	}
	if results != nil {
		t.Errorf("expected nil results for empty areaTags, got %v", results)
	}
}
