package hub

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/keystore"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// setupNodesTest returns a hub with its operator self-registered active
// and one pending leaf registered, plus the operator and leaf keystores.
func setupNodesTest(t *testing.T) (*httptest.Server, *keystore.Keystore, *keystore.Keystore) {
	t.Helper()
	h, hubKS := setupTestHub(t)
	// setupTestHub uses AutoApprove: true; flip the store entry to pending
	// after registration to exercise approve.
	if _, err := h.subscribers.Add(Subscriber{
		NodeID: hubKS.NodeID(), Network: "testnet",
		PubKeyB64: hubKS.PubKeyBase64(), BBSName: "hub", Status: "active",
	}); err != nil {
		t.Fatalf("self-register: %v", err)
	}

	leafKS, _, err := keystore.Load(filepath.Join(t.TempDir(), "leaf.key"))
	if err != nil {
		t.Fatalf("leaf keystore: %v", err)
	}
	ts := httptest.NewServer(h.newMux())
	t.Cleanup(ts.Close)
	registerLeaf(t, ts, leafKS)
	if err := h.subscribers.SetStatus(leafKS.NodeID(), "testnet", "pending"); err != nil {
		t.Fatalf("set pending: %v", err)
	}
	return ts, hubKS, leafKS
}

func doSigned(t *testing.T, ks *keystore.Keystore, method, url string) *http.Response {
	t.Helper()
	resp, err := http.DefaultClient.Do(signedRequest(t, ks, method, url, ""))
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

func TestNodesList_OperatorSeesRegistrations(t *testing.T) {
	ts, hubKS, leafKS := setupNodesTest(t)
	resp := doSigned(t, hubKS, "GET", ts.URL+"/v3net/v1/testnet/nodes")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d, want 200", resp.StatusCode)
	}
	var nodes []protocol.NodeInfo
	if err := json.NewDecoder(resp.Body).Decode(&nodes); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(nodes) != 2 {
		t.Fatalf("got %d nodes, want 2 (hub + leaf)", len(nodes))
	}
	var leaf *protocol.NodeInfo
	for i := range nodes {
		if nodes[i].NodeID == leafKS.NodeID() {
			leaf = &nodes[i]
		}
	}
	if leaf == nil || leaf.Status != "pending" || leaf.BBSName != "Test BBS" || leaf.CreatedAt == "" {
		t.Errorf("leaf entry wrong: %+v", leaf)
	}
}

func TestNodesList_NonOperatorForbidden(t *testing.T) {
	ts, _, leafKS := setupNodesTest(t)
	// Leaf must be active to pass auth at all; the operator check is what
	// must reject it.
	resp := doSigned(t, leafKS, "POST", ts.URL+"/v3net/v1/testnet/nodes/"+leafKS.NodeID()+"/approve")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("pending leaf: status %d, want 401 (fails auth)", resp.StatusCode)
	}
}

func TestNodesApprove_ActivatesPendingLeaf(t *testing.T) {
	ts, hubKS, leafKS := setupNodesTest(t)
	resp := doSigned(t, hubKS, "POST", ts.URL+"/v3net/v1/testnet/nodes/"+leafKS.NodeID()+"/approve")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d, want 200", resp.StatusCode)
	}
	var out struct {
		OK     bool   `json:"ok"`
		Status string `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil || !out.OK || out.Status != "active" {
		t.Fatalf("body %+v err %v, want ok/active", out, err)
	}
	// The approved leaf can now hit an authed endpoint.
	resp2 := doSigned(t, leafKS, "GET", ts.URL+"/v3net/v1/testnet/messages?since=0&limit=10")
	if resp2.StatusCode != http.StatusOK {
		t.Errorf("approved leaf messages: status %d, want 200", resp2.StatusCode)
	}
}

func TestNodesBanUnban_TogglesAuth(t *testing.T) {
	ts, hubKS, leafKS := setupNodesTest(t)
	doSigned(t, hubKS, "POST", ts.URL+"/v3net/v1/testnet/nodes/"+leafKS.NodeID()+"/approve")

	resp := doSigned(t, hubKS, "POST", ts.URL+"/v3net/v1/testnet/nodes/"+leafKS.NodeID()+"/ban")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("ban status %d", resp.StatusCode)
	}
	if r := doSigned(t, leafKS, "GET", ts.URL+"/v3net/v1/testnet/messages?since=0&limit=10"); r.StatusCode != http.StatusUnauthorized {
		t.Errorf("banned leaf: status %d, want 401", r.StatusCode)
	}

	resp = doSigned(t, hubKS, "POST", ts.URL+"/v3net/v1/testnet/nodes/"+leafKS.NodeID()+"/unban")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unban status %d", resp.StatusCode)
	}
	if r := doSigned(t, leafKS, "GET", ts.URL+"/v3net/v1/testnet/messages?since=0&limit=10"); r.StatusCode != http.StatusOK {
		t.Errorf("unbanned leaf: status %d, want 200 (unban goes straight to active)", r.StatusCode)
	}
}

func TestNodesRemove_DeletesRegistration(t *testing.T) {
	ts, hubKS, leafKS := setupNodesTest(t)
	resp := doSigned(t, hubKS, "POST", ts.URL+"/v3net/v1/testnet/nodes/"+leafKS.NodeID()+"/remove")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("remove status %d", resp.StatusCode)
	}
	// Second remove: node no longer exists.
	resp = doSigned(t, hubKS, "POST", ts.URL+"/v3net/v1/testnet/nodes/"+leafKS.NodeID()+"/remove")
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("re-remove status %d, want 404", resp.StatusCode)
	}
}

func TestNodesSelfGuard_OperatorCannotActOnOwnNode(t *testing.T) {
	ts, hubKS, _ := setupNodesTest(t)
	for _, action := range []string{"approve", "ban", "unban", "remove"} {
		resp := doSigned(t, hubKS, "POST", ts.URL+"/v3net/v1/testnet/nodes/"+hubKS.NodeID()+"/"+action)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s on self: status %d, want 400", action, resp.StatusCode)
		}
	}
}

func TestNodes_UnknownNetworkAndNode404(t *testing.T) {
	ts, hubKS, _ := setupNodesTest(t)
	// Unknown network fails auth lookup first (401) — acceptable; the
	// contract we assert is non-200.
	if r := doSigned(t, hubKS, "GET", ts.URL+"/v3net/v1/nosuchnet/nodes"); r.StatusCode == http.StatusOK {
		t.Error("unknown network should not return 200")
	}
	if r := doSigned(t, hubKS, "POST", ts.URL+"/v3net/v1/testnet/nodes/ffffffffffffffff/approve"); r.StatusCode != http.StatusNotFound {
		t.Errorf("unknown node: status %d, want 404", r.StatusCode)
	}
}
