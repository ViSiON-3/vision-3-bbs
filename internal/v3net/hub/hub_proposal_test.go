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

func setupTestHubManual(t *testing.T) (*Hub, *keystore.Keystore) {
	t.Helper()
	dir := t.TempDir()
	ks, _, err := keystore.Load(filepath.Join(dir, "hub.key"))
	if err != nil {
		t.Fatalf("load hub keystore: %v", err)
	}
	cfg := Config{
		ListenAddr:  ":0",
		DataDir:     dir,
		Keystore:    ks,
		AutoApprove: false,
		Networks:    []NetworkConfig{{Name: "testnet", Description: "Test network"}},
	}
	h, err := New(cfg)
	if err != nil {
		t.Fatalf("create hub: %v", err)
	}
	t.Cleanup(func() { h.Close() })
	return h, ks
}

func seedTestNALForProposals(t *testing.T, h *Hub, ks *keystore.Keystore) {
	t.Helper()
	testNAL := &protocol.NAL{
		V3NetNAL:       "1.0",
		Network:        "testnet",
		CoordNodeID:    ks.NodeID(),
		CoordPubKeyB64: ks.PubKeyBase64(),
		Areas:          []protocol.Area{},
	}
	if err := nal.Sign(testNAL, ks); err != nil {
		t.Fatalf("sign test NAL: %v", err)
	}
	if err := h.nalStore.Put("testnet", testNAL); err != nil {
		t.Fatalf("put test NAL: %v", err)
	}
}

func activateSubscriber(t *testing.T, h *Hub, nodeID, network string) {
	t.Helper()
	h.subscribers.mu.Lock()
	defer h.subscribers.mu.Unlock()
	key := nodeID + ":" + network
	if s, ok := h.subscribers.cache[key]; ok {
		s.Status = "active"
	}
	if _, err := h.subscribers.db.Exec(
		`UPDATE subscribers SET status = 'active' WHERE node_id = ? AND network = ?`,
		nodeID, network,
	); err != nil {
		t.Fatalf("activate subscriber: %v", err)
	}
}

func registerAndActivate(t *testing.T, ts *httptest.Server, h *Hub, ks *keystore.Keystore, bbsName, bbsHost string) {
	t.Helper()
	body := fmt.Sprintf(`{"network":"testnet","node_id":%q,"pubkey_b64":%q,"bbs_name":%q,"bbs_host":%q}`,
		ks.NodeID(), ks.PubKeyBase64(), bbsName, bbsHost)
	resp, err := http.Post(ts.URL+"/v3net/v1/subscribe", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("subscribe %s: %v", bbsName, err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("subscribe %s status: %d", bbsName, resp.StatusCode)
	}
	activateSubscriber(t, h, ks.NodeID(), "testnet")
}

const proposalBody = `{"tag":"gen.test","name":"Test Area","description":"A test","language":"en","access_mode":"open","allow_ansi":true}`

func TestPropose_AutoApprove(t *testing.T) {
	h, hubKS := setupTestHub(t)
	ts := httptest.NewServer(h.newMux())
	defer ts.Close()

	leafKS, _, err := keystore.Load(filepath.Join(t.TempDir(), "leaf.key"))
	if err != nil {
		t.Fatalf("load leaf keystore: %v", err)
	}
	registerLeaf(t, ts, leafKS)
	seedTestNALForProposals(t, h, hubKS)

	req := signedRequest(t, leafKS, "POST", ts.URL+"/v3net/v1/testnet/areas/propose", proposalBody)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST propose: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if result["status"] != "approved" {
		t.Errorf("expected status approved, got %v", result["status"])
	}
	if result["proposal_id"] == nil || result["proposal_id"] == "" {
		t.Errorf("expected non-empty proposal_id")
	}

	// Verify the area was added to the NAL.
	nalResp, err := http.Get(ts.URL + "/v3net/v1/testnet/nal")
	if err != nil {
		t.Fatalf("GET nal: %v", err)
	}
	defer nalResp.Body.Close()

	if nalResp.StatusCode != http.StatusOK {
		t.Fatalf("GET nal status: %d", nalResp.StatusCode)
	}

	var nalDoc protocol.NAL
	if err := json.NewDecoder(nalResp.Body).Decode(&nalDoc); err != nil {
		t.Fatalf("decode NAL: %v", err)
	}
	if nalDoc.FindArea("gen.test") == nil {
		t.Errorf("expected area gen.test in NAL, got areas: %+v", nalDoc.Areas)
	}
}

func TestPropose_ManualPending(t *testing.T) {
	h, hubKS := setupTestHubManual(t)
	ts := httptest.NewServer(h.newMux())
	defer ts.Close()

	leafKS, _, err := keystore.Load(filepath.Join(t.TempDir(), "leaf.key"))
	if err != nil {
		t.Fatalf("load leaf keystore: %v", err)
	}
	registerAndActivate(t, ts, h, leafKS, "Test BBS", "test.example.net")
	seedTestNALForProposals(t, h, hubKS)

	ch, cancel := h.broadcaster.Subscribe("testnet")
	defer cancel()

	req := signedRequest(t, leafKS, "POST", ts.URL+"/v3net/v1/testnet/areas/propose", proposalBody)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST propose: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if result["status"] != "pending" {
		t.Errorf("expected status pending, got %v", result["status"])
	}

	select {
	case ev := <-ch:
		if ev.Type != "area_proposed" {
			t.Errorf("expected area_proposed event, got %q", ev.Type)
		}
	case <-time.After(2 * time.Second):
		t.Error("timed out waiting for area_proposed event")
	}
}

func TestListProposals_CoordinatorOnly(t *testing.T) {
	h, hubKS := setupTestHubManual(t)
	ts := httptest.NewServer(h.newMux())
	defer ts.Close()

	leafKS, _, err := keystore.Load(filepath.Join(t.TempDir(), "leaf.key"))
	if err != nil {
		t.Fatalf("load leaf keystore: %v", err)
	}
	registerAndActivate(t, ts, h, leafKS, "Test BBS", "test.example.net")
	seedTestNALForProposals(t, h, hubKS)

	// Propose an area as the leaf.
	propReq := signedRequest(t, leafKS, "POST", ts.URL+"/v3net/v1/testnet/areas/propose", proposalBody)
	propResp, err := http.DefaultClient.Do(propReq)
	if err != nil {
		t.Fatalf("POST propose: %v", err)
	}
	propResp.Body.Close()
	if propResp.StatusCode != http.StatusOK {
		t.Fatalf("POST propose expected 200, got %d", propResp.StatusCode)
	}

	// (a) GET proposals as non-coordinator leaf -> 403.
	listReq := signedRequest(t, leafKS, "GET", ts.URL+"/v3net/v1/testnet/areas/proposals", "")
	listResp, err := http.DefaultClient.Do(listReq)
	if err != nil {
		t.Fatalf("GET proposals as leaf: %v", err)
	}
	listResp.Body.Close()
	if listResp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403 for non-coordinator, got %d", listResp.StatusCode)
	}

	// (b) Register hub keystore as subscriber, GET proposals as coordinator -> 200.
	registerAndActivate(t, ts, h, hubKS, "Hub BBS", "hub.example.net")

	listReq2 := signedRequest(t, hubKS, "GET", ts.URL+"/v3net/v1/testnet/areas/proposals", "")
	listResp2, err := http.DefaultClient.Do(listReq2)
	if err != nil {
		t.Fatalf("GET proposals as coordinator: %v", err)
	}
	defer listResp2.Body.Close()

	if listResp2.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", listResp2.StatusCode)
	}

	var proposals []protocol.AreaProposal
	if err := json.NewDecoder(listResp2.Body).Decode(&proposals); err != nil {
		t.Fatalf("decode proposals: %v", err)
	}
	if len(proposals) != 1 {
		t.Fatalf("expected 1 proposal, got %d", len(proposals))
	}
	if proposals[0].Tag != "gen.test" {
		t.Errorf("expected tag gen.test, got %q", proposals[0].Tag)
	}
	if proposals[0].Status != "pending" {
		t.Errorf("expected status pending, got %q", proposals[0].Status)
	}
}

func TestApproveProposal_AddsToNAL(t *testing.T) {
	h, hubKS := setupTestHubManual(t)
	ts := httptest.NewServer(h.newMux())
	defer ts.Close()

	leafKS, _, err := keystore.Load(filepath.Join(t.TempDir(), "leaf.key"))
	if err != nil {
		t.Fatalf("load leaf keystore: %v", err)
	}
	registerAndActivate(t, ts, h, leafKS, "Test BBS", "test.example.net")
	registerAndActivate(t, ts, h, hubKS, "Hub BBS", "hub.example.net")
	seedTestNALForProposals(t, h, hubKS)

	// Propose an area as the leaf.
	propReq := signedRequest(t, leafKS, "POST", ts.URL+"/v3net/v1/testnet/areas/propose", proposalBody)
	propResp, err := http.DefaultClient.Do(propReq)
	if err != nil {
		t.Fatalf("POST propose: %v", err)
	}
	propResp.Body.Close()

	// Get the proposal ID from the list.
	listReq := signedRequest(t, hubKS, "GET", ts.URL+"/v3net/v1/testnet/areas/proposals", "")
	listResp, err := http.DefaultClient.Do(listReq)
	if err != nil {
		t.Fatalf("GET proposals: %v", err)
	}
	defer listResp.Body.Close()

	var proposals []protocol.AreaProposal
	if err := json.NewDecoder(listResp.Body).Decode(&proposals); err != nil {
		t.Fatalf("decode proposals: %v", err)
	}
	if len(proposals) != 1 {
		t.Fatalf("expected 1 proposal, got %d", len(proposals))
	}
	proposalID := proposals[0].ID

	// Subscribe to broadcaster before approve to catch the nal_updated event.
	ch, cancel := h.broadcaster.Subscribe("testnet")
	defer cancel()

	// Approve the proposal.
	approveReq := signedRequest(t, hubKS, "POST",
		ts.URL+"/v3net/v1/testnet/areas/proposals/"+proposalID+"/approve",
		`{"access_mode":"open"}`)
	approveResp, err := http.DefaultClient.Do(approveReq)
	if err != nil {
		t.Fatalf("POST approve: %v", err)
	}
	approveResp.Body.Close()
	if approveResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", approveResp.StatusCode)
	}

	// Verify the area was added to the NAL.
	nalResp, err := http.Get(ts.URL + "/v3net/v1/testnet/nal")
	if err != nil {
		t.Fatalf("GET nal: %v", err)
	}
	defer nalResp.Body.Close()

	var nalDoc protocol.NAL
	if err := json.NewDecoder(nalResp.Body).Decode(&nalDoc); err != nil {
		t.Fatalf("decode NAL: %v", err)
	}
	if nalDoc.FindArea("gen.test") == nil {
		t.Errorf("expected area gen.test in NAL after approve")
	}

	// Verify nal_updated event was broadcast.
	select {
	case ev := <-ch:
		if ev.Type != "nal_updated" {
			t.Errorf("expected nal_updated event, got %q", ev.Type)
		}
	case <-time.After(2 * time.Second):
		t.Error("timed out waiting for nal_updated event")
	}
}

func TestRejectProposal_Event(t *testing.T) {
	h, hubKS := setupTestHubManual(t)
	ts := httptest.NewServer(h.newMux())
	defer ts.Close()

	leafKS, _, err := keystore.Load(filepath.Join(t.TempDir(), "leaf.key"))
	if err != nil {
		t.Fatalf("load leaf keystore: %v", err)
	}
	registerAndActivate(t, ts, h, leafKS, "Test BBS", "test.example.net")
	registerAndActivate(t, ts, h, hubKS, "Hub BBS", "hub.example.net")
	seedTestNALForProposals(t, h, hubKS)

	// Propose an area as the leaf.
	propReq := signedRequest(t, leafKS, "POST", ts.URL+"/v3net/v1/testnet/areas/propose", proposalBody)
	propResp, err := http.DefaultClient.Do(propReq)
	if err != nil {
		t.Fatalf("POST propose: %v", err)
	}
	propResp.Body.Close()

	// Get the proposal ID from the list.
	listReq := signedRequest(t, hubKS, "GET", ts.URL+"/v3net/v1/testnet/areas/proposals", "")
	listResp, err := http.DefaultClient.Do(listReq)
	if err != nil {
		t.Fatalf("GET proposals: %v", err)
	}
	defer listResp.Body.Close()

	var proposals []protocol.AreaProposal
	if err := json.NewDecoder(listResp.Body).Decode(&proposals); err != nil {
		t.Fatalf("decode proposals: %v", err)
	}
	if len(proposals) != 1 {
		t.Fatalf("expected 1 proposal, got %d", len(proposals))
	}
	proposalID := proposals[0].ID

	// Subscribe to broadcaster before reject to catch the event.
	ch, cancel := h.broadcaster.Subscribe("testnet")
	defer cancel()

	// Reject the proposal.
	rejectReq := signedRequest(t, hubKS, "POST",
		ts.URL+"/v3net/v1/testnet/areas/proposals/"+proposalID+"/reject",
		`{"reason":"not needed"}`)
	rejectResp, err := http.DefaultClient.Do(rejectReq)
	if err != nil {
		t.Fatalf("POST reject: %v", err)
	}
	rejectResp.Body.Close()
	if rejectResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", rejectResp.StatusCode)
	}

	// Verify proposal_rejected event was broadcast.
	select {
	case ev := <-ch:
		if ev.Type != "proposal_rejected" {
			t.Errorf("expected proposal_rejected event, got %q", ev.Type)
		}
	case <-time.After(2 * time.Second):
		t.Error("timed out waiting for proposal_rejected event")
	}
}

func TestPropose_InvalidTag(t *testing.T) {
	h, hubKS := setupTestHub(t)
	ts := httptest.NewServer(h.newMux())
	defer ts.Close()

	leafKS, _, err := keystore.Load(filepath.Join(t.TempDir(), "leaf.key"))
	if err != nil {
		t.Fatalf("load leaf keystore: %v", err)
	}
	registerLeaf(t, ts, leafKS)
	seedTestNALForProposals(t, h, hubKS)

	invalidBody := `{"tag":"INVALID","name":"Bad Area","description":"Invalid tag","language":"en","access_mode":"open","allow_ansi":false}`
	req := signedRequest(t, leafKS, "POST", ts.URL+"/v3net/v1/testnet/areas/propose", invalidBody)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST propose: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("expected 422 for invalid tag, got %d", resp.StatusCode)
	}
}
