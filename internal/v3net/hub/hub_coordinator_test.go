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

func seedCoordNAL(t *testing.T, h *Hub, ks *keystore.Keystore) {
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

func registerAsLeaf(t *testing.T, ts *httptest.Server, ks *keystore.Keystore) {
	t.Helper()
	body := fmt.Sprintf(`{"network":"testnet","node_id":%q,"pubkey_b64":%q,"bbs_name":"Node","bbs_host":"node.example.net"}`, ks.NodeID(), ks.PubKeyBase64())
	resp, err := http.Post(ts.URL+"/v3net/v1/subscribe", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("register status: %d", resp.StatusCode)
	}
}

func TestCoordTransfer_FullFlow(t *testing.T) {
	h, hubKS := setupTestHub(t)
	ts := httptest.NewServer(h.newMux())
	defer ts.Close()

	leafKS, _, err := keystore.Load(filepath.Join(t.TempDir(), "leaf.key"))
	if err != nil {
		t.Fatalf("load leaf keystore: %v", err)
	}

	registerAsLeaf(t, ts, hubKS)
	registerAsLeaf(t, ts, leafKS)
	seedCoordNAL(t, h, hubKS)

	// Initiate transfer as hub (coordinator) to leaf.
	transferBody := fmt.Sprintf(`{"new_node_id":%q,"new_pubkey_b64":%q}`, leafKS.NodeID(), leafKS.PubKeyBase64())
	req := signedRequest(t, hubKS, "POST", ts.URL+"/v3net/v1/testnet/coordinator/transfer", transferBody)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST transfer: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("transfer expected 200, got %d", resp.StatusCode)
	}

	var transferResp map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&transferResp); err != nil {
		t.Fatalf("decode transfer response: %v", err)
	}
	token, ok := transferResp["token"].(string)
	if !ok || token == "" {
		t.Fatalf("expected non-empty token, got %v", transferResp["token"])
	}

	// Accept transfer as leaf (new coordinator).
	acceptBody := fmt.Sprintf(`{"token":%q}`, token)
	acceptReq := signedRequest(t, leafKS, "POST", ts.URL+"/v3net/v1/testnet/coordinator/accept", acceptBody)
	acceptResp, err := http.DefaultClient.Do(acceptReq)
	if err != nil {
		t.Fatalf("POST accept: %v", err)
	}
	defer acceptResp.Body.Close()

	if acceptResp.StatusCode != http.StatusOK {
		t.Fatalf("accept expected 200, got %d", acceptResp.StatusCode)
	}

	// Verify NAL coordinator changed to leaf.
	nalResp, err := http.Get(ts.URL + "/v3net/v1/testnet/nal")
	if err != nil {
		t.Fatalf("GET nal: %v", err)
	}
	defer nalResp.Body.Close()

	if nalResp.StatusCode != http.StatusOK {
		t.Fatalf("GET nal expected 200, got %d", nalResp.StatusCode)
	}

	var updatedNAL protocol.NAL
	if err := json.NewDecoder(nalResp.Body).Decode(&updatedNAL); err != nil {
		t.Fatalf("decode NAL: %v", err)
	}
	if updatedNAL.CoordNodeID != leafKS.NodeID() {
		t.Errorf("expected coordinator_node_id %q, got %q", leafKS.NodeID(), updatedNAL.CoordNodeID)
	}
	if updatedNAL.CoordPubKeyB64 != leafKS.PubKeyBase64() {
		t.Errorf("expected coordinator_pubkey_b64 %q, got %q", leafKS.PubKeyBase64(), updatedNAL.CoordPubKeyB64)
	}
}

func TestCoordTransfer_NonCoordinator(t *testing.T) {
	h, hubKS := setupTestHub(t)
	ts := httptest.NewServer(h.newMux())
	defer ts.Close()

	leafKS, _, err := keystore.Load(filepath.Join(t.TempDir(), "leaf.key"))
	if err != nil {
		t.Fatalf("load leaf keystore: %v", err)
	}

	registerAsLeaf(t, ts, hubKS)
	registerAsLeaf(t, ts, leafKS)
	seedCoordNAL(t, h, hubKS)

	// Attempt transfer as leaf (non-coordinator).
	transferBody := fmt.Sprintf(`{"new_node_id":%q,"new_pubkey_b64":%q}`, hubKS.NodeID(), hubKS.PubKeyBase64())
	req := signedRequest(t, leafKS, "POST", ts.URL+"/v3net/v1/testnet/coordinator/transfer", transferBody)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST transfer: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403 for non-coordinator transfer, got %d", resp.StatusCode)
	}
}

func TestCoordAccept_WrongNode(t *testing.T) {
	h, hubKS := setupTestHub(t)
	ts := httptest.NewServer(h.newMux())
	defer ts.Close()

	leafKS, _, err := keystore.Load(filepath.Join(t.TempDir(), "leaf.key"))
	if err != nil {
		t.Fatalf("load leaf keystore: %v", err)
	}
	thirdKS, _, err := keystore.Load(filepath.Join(t.TempDir(), "third.key"))
	if err != nil {
		t.Fatalf("load third keystore: %v", err)
	}

	registerAsLeaf(t, ts, hubKS)
	registerAsLeaf(t, ts, leafKS)
	registerAsLeaf(t, ts, thirdKS)
	seedCoordNAL(t, h, hubKS)

	// Initiate transfer to leaf.
	transferBody := fmt.Sprintf(`{"new_node_id":%q,"new_pubkey_b64":%q}`, leafKS.NodeID(), leafKS.PubKeyBase64())
	req := signedRequest(t, hubKS, "POST", ts.URL+"/v3net/v1/testnet/coordinator/transfer", transferBody)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST transfer: %v", err)
	}
	defer resp.Body.Close()

	var transferResp map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&transferResp); err != nil {
		t.Fatalf("decode transfer response: %v", err)
	}
	token := transferResp["token"].(string)

	// Attempt accept as third keystore (wrong node).
	acceptBody := fmt.Sprintf(`{"token":%q}`, token)
	acceptReq := signedRequest(t, thirdKS, "POST", ts.URL+"/v3net/v1/testnet/coordinator/accept", acceptBody)
	acceptResp, err := http.DefaultClient.Do(acceptReq)
	if err != nil {
		t.Fatalf("POST accept: %v", err)
	}
	acceptResp.Body.Close()

	if acceptResp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403 for wrong node accept, got %d", acceptResp.StatusCode)
	}
}

func TestCoordAccept_InvalidToken(t *testing.T) {
	h, hubKS := setupTestHub(t)
	ts := httptest.NewServer(h.newMux())
	defer ts.Close()

	leafKS, _, err := keystore.Load(filepath.Join(t.TempDir(), "leaf.key"))
	if err != nil {
		t.Fatalf("load leaf keystore: %v", err)
	}

	registerAsLeaf(t, ts, hubKS)
	registerAsLeaf(t, ts, leafKS)
	seedCoordNAL(t, h, hubKS)

	// Initiate transfer to leaf.
	transferBody := fmt.Sprintf(`{"new_node_id":%q,"new_pubkey_b64":%q}`, leafKS.NodeID(), leafKS.PubKeyBase64())
	req := signedRequest(t, hubKS, "POST", ts.URL+"/v3net/v1/testnet/coordinator/transfer", transferBody)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST transfer: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("transfer expected 200, got %d", resp.StatusCode)
	}

	// Attempt accept with bogus token.
	acceptBody := `{"token":"dGhpcyBpcyBub3QgYSB2YWxpZCB0b2tlbg=="}`
	acceptReq := signedRequest(t, leafKS, "POST", ts.URL+"/v3net/v1/testnet/coordinator/accept", acceptBody)
	acceptResp, err := http.DefaultClient.Do(acceptReq)
	if err != nil {
		t.Fatalf("POST accept: %v", err)
	}
	acceptResp.Body.Close()

	if acceptResp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403 for invalid token, got %d", acceptResp.StatusCode)
	}
}

func TestCoordTransfer_SSEEvent(t *testing.T) {
	h, hubKS := setupTestHub(t)
	ts := httptest.NewServer(h.newMux())
	defer ts.Close()

	leafKS, _, err := keystore.Load(filepath.Join(t.TempDir(), "leaf.key"))
	if err != nil {
		t.Fatalf("load leaf keystore: %v", err)
	}

	registerAsLeaf(t, ts, hubKS)
	registerAsLeaf(t, ts, leafKS)
	seedCoordNAL(t, h, hubKS)

	ch, cancel := h.broadcaster.Subscribe("testnet")
	defer cancel()

	// Initiate transfer.
	transferBody := fmt.Sprintf(`{"new_node_id":%q,"new_pubkey_b64":%q}`, leafKS.NodeID(), leafKS.PubKeyBase64())
	req := signedRequest(t, hubKS, "POST", ts.URL+"/v3net/v1/testnet/coordinator/transfer", transferBody)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST transfer: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("transfer expected 200, got %d", resp.StatusCode)
	}

	select {
	case ev := <-ch:
		if ev.Type != "coordinator_transfer_pending" {
			t.Errorf("expected coordinator_transfer_pending event, got %q", ev.Type)
		}
	case <-time.After(2 * time.Second):
		t.Error("timed out waiting for coordinator_transfer_pending SSE event")
	}
}
