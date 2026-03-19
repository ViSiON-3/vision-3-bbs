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

func seedNALWithAreas(t *testing.T, h *Hub, ks *keystore.Keystore, areas []protocol.Area) {
	t.Helper()
	for i := range areas {
		if areas[i].ManagerNodeID == "" {
			areas[i].ManagerNodeID = ks.NodeID()
			areas[i].ManagerPubKeyB64 = ks.PubKeyBase64()
		}
	}
	testNAL := &protocol.NAL{
		V3NetNAL:       "1.0",
		Network:        "testnet",
		CoordNodeID:    ks.NodeID(),
		CoordPubKeyB64: ks.PubKeyBase64(),
		Areas:          areas,
	}
	if err := nal.Sign(testNAL, ks); err != nil {
		t.Fatalf("sign test NAL: %v", err)
	}
	if err := h.nalStore.Put("testnet", testNAL); err != nil {
		t.Fatalf("put test NAL: %v", err)
	}
}

func registerHubAsLeaf(t *testing.T, ts *httptest.Server, hubKS *keystore.Keystore) {
	t.Helper()
	body := fmt.Sprintf(`{"network":"testnet","node_id":%q,"pubkey_b64":%q,"bbs_name":"Hub BBS","bbs_host":"hub.example.net"}`,
		hubKS.NodeID(), hubKS.PubKeyBase64())
	resp, err := http.Post(ts.URL+"/v3net/v1/subscribe", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("register hub as leaf: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("register hub status: %d", resp.StatusCode)
	}
}

func TestGetAccess_ManagerOnly(t *testing.T) {
	h, hubKS := setupTestHub(t)
	ts := httptest.NewServer(h.newMux())
	defer ts.Close()

	leafKS, _, err := keystore.Load(filepath.Join(t.TempDir(), "leaf.key"))
	if err != nil {
		t.Fatalf("load leaf keystore: %v", err)
	}

	registerHubAsLeaf(t, ts, hubKS)
	registerLeaf(t, ts, leafKS)

	seedNALWithAreas(t, h, hubKS, []protocol.Area{
		{
			Tag:         "gen.general",
			Name:        "General",
			Description: "General discussion",
			Language:    "en",
			Access:      protocol.AreaAccess{Mode: protocol.AccessModeOpen},
		},
	})

	// (a) Leaf should get 403.
	leafReq := signedRequest(t, leafKS, "GET", ts.URL+"/v3net/v1/testnet/areas/gen.general/access", "")
	resp, err := http.DefaultClient.Do(leafReq)
	if err != nil {
		t.Fatalf("GET access as leaf: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403 for non-manager, got %d", resp.StatusCode)
	}

	// (b) Hub (manager) should get 200.
	hubReq := signedRequest(t, hubKS, "GET", ts.URL+"/v3net/v1/testnet/areas/gen.general/access", "")
	resp2, err := http.DefaultClient.Do(hubReq)
	if err != nil {
		t.Fatalf("GET access as hub: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for manager, got %d", resp2.StatusCode)
	}

	var cfg protocol.AreaAccessConfig
	if err := json.NewDecoder(resp2.Body).Decode(&cfg); err != nil {
		t.Fatalf("decode access config: %v", err)
	}
	if cfg.Mode != protocol.AccessModeOpen {
		t.Errorf("expected mode %q, got %q", protocol.AccessModeOpen, cfg.Mode)
	}
	if len(cfg.AllowList) != 0 {
		t.Errorf("expected empty allow_list, got %v", cfg.AllowList)
	}
	if len(cfg.DenyList) != 0 {
		t.Errorf("expected empty deny_list, got %v", cfg.DenyList)
	}
}

func TestSetAccessMode(t *testing.T) {
	h, hubKS := setupTestHub(t)
	ts := httptest.NewServer(h.newMux())
	defer ts.Close()

	registerHubAsLeaf(t, ts, hubKS)

	seedNALWithAreas(t, h, hubKS, []protocol.Area{
		{
			Tag:         "gen.general",
			Name:        "General",
			Description: "General discussion",
			Language:    "en",
			Access:      protocol.AreaAccess{Mode: protocol.AccessModeOpen},
		},
	})

	// POST to change mode to approval.
	modeBody := `{"mode":"approval"}`
	req := signedRequest(t, hubKS, "POST", ts.URL+"/v3net/v1/testnet/areas/gen.general/access/mode", modeBody)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST access mode: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for set mode, got %d", resp.StatusCode)
	}

	// GET and verify mode changed.
	getReq := signedRequest(t, hubKS, "GET", ts.URL+"/v3net/v1/testnet/areas/gen.general/access", "")
	resp2, err := http.DefaultClient.Do(getReq)
	if err != nil {
		t.Fatalf("GET access: %v", err)
	}
	defer resp2.Body.Close()

	var cfg protocol.AreaAccessConfig
	if err := json.NewDecoder(resp2.Body).Decode(&cfg); err != nil {
		t.Fatalf("decode access config: %v", err)
	}
	if cfg.Mode != protocol.AccessModeApproval {
		t.Errorf("expected mode %q, got %q", protocol.AccessModeApproval, cfg.Mode)
	}
}

func TestApproveAccess_AddsToAllowList(t *testing.T) {
	h, hubKS := setupTestHub(t)
	ts := httptest.NewServer(h.newMux())
	defer ts.Close()

	leafKS, _, err := keystore.Load(filepath.Join(t.TempDir(), "leaf.key"))
	if err != nil {
		t.Fatalf("load leaf keystore: %v", err)
	}

	registerHubAsLeaf(t, ts, hubKS)
	registerLeaf(t, ts, leafKS)

	seedNALWithAreas(t, h, hubKS, []protocol.Area{
		{
			Tag:         "gen.general",
			Name:        "General",
			Description: "General discussion",
			Language:    "en",
			Access:      protocol.AreaAccess{Mode: protocol.AccessModeApproval},
		},
	})

	// Approve the leaf node.
	approveBody := fmt.Sprintf(`{"node_ids":[%q]}`, leafKS.NodeID())
	req := signedRequest(t, hubKS, "POST", ts.URL+"/v3net/v1/testnet/areas/gen.general/access/approve", approveBody)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST approve: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for approve, got %d", resp.StatusCode)
	}

	// GET access config and verify allow_list.
	getReq := signedRequest(t, hubKS, "GET", ts.URL+"/v3net/v1/testnet/areas/gen.general/access", "")
	resp2, err := http.DefaultClient.Do(getReq)
	if err != nil {
		t.Fatalf("GET access: %v", err)
	}
	defer resp2.Body.Close()

	var cfg protocol.AreaAccessConfig
	if err := json.NewDecoder(resp2.Body).Decode(&cfg); err != nil {
		t.Fatalf("decode access config: %v", err)
	}

	found := false
	for _, id := range cfg.AllowList {
		if id == leafKS.NodeID() {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected leaf node_id %q in allow_list, got %v", leafKS.NodeID(), cfg.AllowList)
	}
}

func TestDenyAccess_AddsToDenyList(t *testing.T) {
	h, hubKS := setupTestHub(t)
	ts := httptest.NewServer(h.newMux())
	defer ts.Close()

	leafKS, _, err := keystore.Load(filepath.Join(t.TempDir(), "leaf.key"))
	if err != nil {
		t.Fatalf("load leaf keystore: %v", err)
	}

	registerHubAsLeaf(t, ts, hubKS)
	registerLeaf(t, ts, leafKS)

	seedNALWithAreas(t, h, hubKS, []protocol.Area{
		{
			Tag:         "gen.general",
			Name:        "General",
			Description: "General discussion",
			Language:    "en",
			Access:      protocol.AreaAccess{Mode: protocol.AccessModeOpen},
		},
	})

	// Deny the leaf node.
	denyBody := fmt.Sprintf(`{"node_ids":[%q]}`, leafKS.NodeID())
	req := signedRequest(t, hubKS, "POST", ts.URL+"/v3net/v1/testnet/areas/gen.general/access/deny", denyBody)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST deny: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for deny, got %d", resp.StatusCode)
	}

	// GET access config and verify deny_list.
	getReq := signedRequest(t, hubKS, "GET", ts.URL+"/v3net/v1/testnet/areas/gen.general/access", "")
	resp2, err := http.DefaultClient.Do(getReq)
	if err != nil {
		t.Fatalf("GET access: %v", err)
	}
	defer resp2.Body.Close()

	var cfg protocol.AreaAccessConfig
	if err := json.NewDecoder(resp2.Body).Decode(&cfg); err != nil {
		t.Fatalf("decode access config: %v", err)
	}

	found := false
	for _, id := range cfg.DenyList {
		if id == leafKS.NodeID() {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected leaf node_id %q in deny_list, got %v", leafKS.NodeID(), cfg.DenyList)
	}
}

func TestRemoveFromDenyList(t *testing.T) {
	h, hubKS := setupTestHub(t)
	ts := httptest.NewServer(h.newMux())
	defer ts.Close()

	leafKS, _, err := keystore.Load(filepath.Join(t.TempDir(), "leaf.key"))
	if err != nil {
		t.Fatalf("load leaf keystore: %v", err)
	}

	registerHubAsLeaf(t, ts, hubKS)
	registerLeaf(t, ts, leafKS)

	// Seed NAL with area that has the leaf in deny_list.
	seedNALWithAreas(t, h, hubKS, []protocol.Area{
		{
			Tag:         "gen.general",
			Name:        "General",
			Description: "General discussion",
			Language:    "en",
			Access: protocol.AreaAccess{
				Mode:     protocol.AccessModeOpen,
				DenyList: []string{leafKS.NodeID()},
			},
		},
	})

	// Remove the leaf from deny_list.
	removeBody := fmt.Sprintf(`{"node_ids":[%q]}`, leafKS.NodeID())
	req := signedRequest(t, hubKS, "POST", ts.URL+"/v3net/v1/testnet/areas/gen.general/access/remove", removeBody)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST remove: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for remove, got %d", resp.StatusCode)
	}

	// GET access config and verify deny_list is empty.
	getReq := signedRequest(t, hubKS, "GET", ts.URL+"/v3net/v1/testnet/areas/gen.general/access", "")
	resp2, err := http.DefaultClient.Do(getReq)
	if err != nil {
		t.Fatalf("GET access: %v", err)
	}
	defer resp2.Body.Close()

	var cfg protocol.AreaAccessConfig
	if err := json.NewDecoder(resp2.Body).Decode(&cfg); err != nil {
		t.Fatalf("decode access config: %v", err)
	}
	if len(cfg.DenyList) != 0 {
		t.Errorf("expected empty deny_list after remove, got %v", cfg.DenyList)
	}
}

func TestSubscribeWithAreaTags_OpenArea(t *testing.T) {
	h, hubKS := setupTestHub(t)
	ts := httptest.NewServer(h.newMux())
	defer ts.Close()

	leafKS, _, err := keystore.Load(filepath.Join(t.TempDir(), "leaf.key"))
	if err != nil {
		t.Fatalf("load leaf keystore: %v", err)
	}

	seedNALWithAreas(t, h, hubKS, []protocol.Area{
		{
			Tag:         "gen.general",
			Name:        "General",
			Description: "General discussion",
			Language:    "en",
			Access:      protocol.AreaAccess{Mode: protocol.AccessModeOpen},
		},
	})

	body := fmt.Sprintf(`{"network":"testnet","node_id":%q,"pubkey_b64":%q,"bbs_name":"Test BBS","bbs_host":"test.example.net","area_tags":["gen.general"]}`,
		leafKS.NodeID(), leafKS.PubKeyBase64())
	resp, err := http.Post(ts.URL+"/v3net/v1/subscribe", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST subscribe: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result protocol.SubscribeWithAreasResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !result.OK {
		t.Errorf("expected ok=true, got false")
	}
	if len(result.Areas) != 1 {
		t.Fatalf("expected 1 area status, got %d", len(result.Areas))
	}
	if result.Areas[0].Tag != "gen.general" {
		t.Errorf("expected tag gen.general, got %q", result.Areas[0].Tag)
	}
	if result.Areas[0].Status != "active" {
		t.Errorf("expected status active, got %q", result.Areas[0].Status)
	}
}

func TestSubscribeWithAreaTags_ApprovalArea(t *testing.T) {
	h, hubKS := setupTestHub(t)
	ts := httptest.NewServer(h.newMux())
	defer ts.Close()

	leafKS, _, err := keystore.Load(filepath.Join(t.TempDir(), "leaf.key"))
	if err != nil {
		t.Fatalf("load leaf keystore: %v", err)
	}

	seedNALWithAreas(t, h, hubKS, []protocol.Area{
		{
			Tag:         "gen.general",
			Name:        "General",
			Description: "General discussion",
			Language:    "en",
			Access:      protocol.AreaAccess{Mode: protocol.AccessModeApproval},
		},
	})

	body := fmt.Sprintf(`{"network":"testnet","node_id":%q,"pubkey_b64":%q,"bbs_name":"Test BBS","bbs_host":"test.example.net","area_tags":["gen.general"]}`,
		leafKS.NodeID(), leafKS.PubKeyBase64())
	resp, err := http.Post(ts.URL+"/v3net/v1/subscribe", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST subscribe: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result protocol.SubscribeWithAreasResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(result.Areas) != 1 {
		t.Fatalf("expected 1 area status, got %d", len(result.Areas))
	}
	if result.Areas[0].Status != "pending" {
		t.Errorf("expected status pending, got %q", result.Areas[0].Status)
	}
}

func TestSubscribeWithAreaTags_ClosedDenied(t *testing.T) {
	h, hubKS := setupTestHub(t)
	ts := httptest.NewServer(h.newMux())
	defer ts.Close()

	leafKS, _, err := keystore.Load(filepath.Join(t.TempDir(), "leaf.key"))
	if err != nil {
		t.Fatalf("load leaf keystore: %v", err)
	}

	seedNALWithAreas(t, h, hubKS, []protocol.Area{
		{
			Tag:         "gen.general",
			Name:        "General",
			Description: "General discussion",
			Language:    "en",
			Access:      protocol.AreaAccess{Mode: protocol.AccessModeClosed},
		},
	})

	body := fmt.Sprintf(`{"network":"testnet","node_id":%q,"pubkey_b64":%q,"bbs_name":"Test BBS","bbs_host":"test.example.net","area_tags":["gen.general"]}`,
		leafKS.NodeID(), leafKS.PubKeyBase64())
	resp, err := http.Post(ts.URL+"/v3net/v1/subscribe", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST subscribe: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403 for closed area, got %d", resp.StatusCode)
	}
}

func TestSubscribeWithAreaTags_DenyList(t *testing.T) {
	h, hubKS := setupTestHub(t)
	ts := httptest.NewServer(h.newMux())
	defer ts.Close()

	leafKS, _, err := keystore.Load(filepath.Join(t.TempDir(), "leaf.key"))
	if err != nil {
		t.Fatalf("load leaf keystore: %v", err)
	}

	seedNALWithAreas(t, h, hubKS, []protocol.Area{
		{
			Tag:         "gen.general",
			Name:        "General",
			Description: "General discussion",
			Language:    "en",
			Access: protocol.AreaAccess{
				Mode:     protocol.AccessModeOpen,
				DenyList: []string{leafKS.NodeID()},
			},
		},
	})

	body := fmt.Sprintf(`{"network":"testnet","node_id":%q,"pubkey_b64":%q,"bbs_name":"Test BBS","bbs_host":"test.example.net","area_tags":["gen.general"]}`,
		leafKS.NodeID(), leafKS.PubKeyBase64())
	resp, err := http.Post(ts.URL+"/v3net/v1/subscribe", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST subscribe: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403 for deny-listed node, got %d", resp.StatusCode)
	}
}
