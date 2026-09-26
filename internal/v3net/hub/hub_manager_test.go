package hub

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/keystore"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/nal"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

func loadTestKeystore(t *testing.T, name string) *keystore.Keystore {
	t.Helper()
	ks, _, err := keystore.Load(filepath.Join(t.TempDir(), name))
	if err != nil {
		t.Fatalf("load %s keystore: %v", name, err)
	}
	return ks
}

func postStatus(t *testing.T, req *http.Request) int {
	t.Helper()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", req.Method, req.URL.Path, err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

func fetchNAL(t *testing.T, ts *httptest.Server) *protocol.NAL {
	t.Helper()
	resp, err := http.Get(ts.URL + "/v3net/v1/testnet/nal")
	if err != nil {
		t.Fatalf("GET nal: %v", err)
	}
	defer resp.Body.Close()
	var n protocol.NAL
	if err := json.NewDecoder(resp.Body).Decode(&n); err != nil {
		t.Fatalf("decode NAL: %v", err)
	}
	if err := nal.Verify(&n); err != nil {
		t.Fatalf("served NAL does not verify: %v", err)
	}
	return &n
}

func TestSetAreaManager(t *testing.T) {
	h, hubKS := setupTestHubManual(t)
	ts := httptest.NewServer(h.newMux())
	defer ts.Close()

	leafKS := loadTestKeystore(t, "leaf.key")
	pendingKS := loadTestKeystore(t, "pending.key")
	registerAndActivate(t, ts, h, hubKS, "Hub BBS", "hub.example.net")
	registerAndActivate(t, ts, h, leafKS, "Leaf BBS", "leaf.example.net")
	registerHubAsLeaf(t, ts, pendingKS) // registered but left pending
	seedNALWithAreas(t, h, hubKS, []protocol.Area{{Tag: "gen.chat", Name: "Chat", Access: protocol.AreaAccess{Mode: "open"}}})

	url := ts.URL + "/v3net/v1/testnet/areas/gen.chat/manager"
	body := func(id string) string { return fmt.Sprintf(`{"manager_node_id":%q}`, id) }

	if got := postStatus(t, signedRequest(t, leafKS, "POST", url, body(leafKS.NodeID()))); got != http.StatusForbidden {
		t.Errorf("non-coordinator: expected 403, got %d", got)
	}
	if got := postStatus(t, signedRequest(t, hubKS, "POST", url, body(pendingKS.NodeID()))); got != http.StatusUnprocessableEntity {
		t.Errorf("pending subscriber as manager: expected 422, got %d", got)
	}
	if got := postStatus(t, signedRequest(t, hubKS, "POST", url, body(""))); got != http.StatusBadRequest {
		t.Errorf("empty manager: expected 400, got %d", got)
	}
	missing := ts.URL + "/v3net/v1/testnet/areas/gen.none/manager"
	if got := postStatus(t, signedRequest(t, hubKS, "POST", missing, body(leafKS.NodeID()))); got != http.StatusNotFound {
		t.Errorf("unknown area: expected 404, got %d", got)
	}

	if got := postStatus(t, signedRequest(t, hubKS, "POST", url, body(leafKS.NodeID()))); got != http.StatusOK {
		t.Fatalf("coordinator reassign: expected 200, got %d", got)
	}
	area := fetchNAL(t, ts).FindArea("gen.chat")
	if area == nil {
		t.Fatal("gen.chat missing from NAL")
	}
	if area.ManagerNodeID != leafKS.NodeID() || area.ManagerPubKeyB64 != leafKS.PubKeyBase64() {
		t.Errorf("manager = %q/%q, want %q/%q", area.ManagerNodeID, area.ManagerPubKeyB64, leafKS.NodeID(), leafKS.PubKeyBase64())
	}
}

// proposeAndList submits the stock test proposal as proposer and returns its
// ID from the coordinator's queue.
func proposeAndList(t *testing.T, ts *httptest.Server, proposer, coord *keystore.Keystore) string {
	t.Helper()
	if got := postStatus(t, signedRequest(t, proposer, "POST", ts.URL+"/v3net/v1/testnet/areas/propose", proposalBody)); got != http.StatusOK {
		t.Fatalf("propose: expected 200, got %d", got)
	}
	resp, err := http.DefaultClient.Do(signedRequest(t, coord, "GET", ts.URL+"/v3net/v1/testnet/areas/proposals", ""))
	if err != nil {
		t.Fatalf("GET proposals: %v", err)
	}
	defer resp.Body.Close()
	var proposals []protocol.AreaProposal
	if err := json.NewDecoder(resp.Body).Decode(&proposals); err != nil || len(proposals) != 1 {
		t.Fatalf("expected 1 proposal, got %d (%v)", len(proposals), err)
	}
	return proposals[0].ID
}

func TestApproveProposal_Overrides(t *testing.T) {
	h, hubKS := setupTestHubManual(t)
	ts := httptest.NewServer(h.newMux())
	defer ts.Close()

	leafKS := loadTestKeystore(t, "leaf.key")
	otherKS := loadTestKeystore(t, "other.key")
	registerAndActivate(t, ts, h, hubKS, "Hub BBS", "hub.example.net")
	registerAndActivate(t, ts, h, leafKS, "Leaf BBS", "leaf.example.net")
	registerAndActivate(t, ts, h, otherKS, "Other BBS", "other.example.net")
	seedTestNALForProposals(t, h, hubKS)

	id := proposeAndList(t, ts, leafKS, hubKS)
	url := ts.URL + "/v3net/v1/testnet/areas/proposals/" + id + "/approve"

	if got := postStatus(t, signedRequest(t, hubKS, "POST", url, `{"access_mode":"secret"}`)); got != http.StatusUnprocessableEntity {
		t.Errorf("bad access mode: expected 422, got %d", got)
	}
	if got := postStatus(t, signedRequest(t, hubKS, "POST", url, `{"manager_node_id":"NOPE"}`)); got != http.StatusUnprocessableEntity {
		t.Errorf("unknown manager: expected 422, got %d", got)
	}

	body := fmt.Sprintf(`{"access_mode":"approval","manager_node_id":%q}`, otherKS.NodeID())
	if got := postStatus(t, signedRequest(t, hubKS, "POST", url, body)); got != http.StatusOK {
		t.Fatalf("approve with overrides: expected 200, got %d", got)
	}
	area := fetchNAL(t, ts).FindArea("gen.test")
	if area == nil {
		t.Fatal("gen.test missing from NAL")
	}
	if area.Access.Mode != protocol.AccessModeApproval {
		t.Errorf("access mode = %q, want approval", area.Access.Mode)
	}
	if area.ManagerNodeID != otherKS.NodeID() || area.ManagerPubKeyB64 != otherKS.PubKeyBase64() {
		t.Errorf("manager = %q, want %q", area.ManagerNodeID, otherKS.NodeID())
	}
}
