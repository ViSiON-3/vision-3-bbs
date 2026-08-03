package configeditor

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/keystore"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// newNodesTestServer returns a server that requires V3Net auth headers and
// serves nodesJSON for GET /nodes, actionJSON for POST /{action}.
func newNodesTestServer(t *testing.T, nodesJSON, actionJSON string) (*httptest.Server, int) {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-V3Net-Node-ID") == "" || r.Header.Get("X-V3Net-Signature") == "" || r.Header.Get("Date") == "" {
			http.Error(w, `{"error":"missing auth headers"}`, http.StatusUnauthorized)
			return
		}
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(nodesJSON))
			return
		}
		_, _ = w.Write([]byte(actionJSON))
	}))
	t.Cleanup(ts.Close)
	u, _ := url.Parse(ts.URL)
	port, _ := strconv.Atoi(u.Port())
	return ts, port
}

func testKeystorePath(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "test.key")
	if _, _, err := keystore.Load(p); err != nil {
		t.Fatalf("create keystore: %v", err)
	}
	return p
}

func TestFetchHubNodes_ReturnsNodes(t *testing.T) {
	nodes := []protocol.NodeInfo{{NodeID: "aaaa000000000001", BBSName: "Test BBS", Status: "pending", CreatedAt: "2026-08-03 12:00:00"}}
	data, _ := json.Marshal(nodes)
	_, port := newNodesTestServer(t, string(data), "{}")

	msg := fetchHubNodes(port, "testnet", testKeystorePath(t))()
	fm, ok := msg.(fetchNodesMsg)
	if !ok {
		t.Fatalf("got %T, want fetchNodesMsg", msg)
	}
	if fm.err != nil || len(fm.nodes) != 1 || fm.nodes[0].NodeID != "aaaa000000000001" {
		t.Errorf("msg %+v", fm)
	}
	if fm.network != "testnet" {
		t.Errorf("network = %q, want testnet", fm.network)
	}
}

func TestFetchHubNodes_MissingKeystoreErrorsWithoutCreatingKey(t *testing.T) {
	_, port := newNodesTestServer(t, "[]", "{}")
	missing := filepath.Join(t.TempDir(), "absent.key")

	msg := fetchHubNodes(port, "testnet", missing)()
	fm := msg.(fetchNodesMsg)
	if fm.err == nil {
		t.Fatal("missing keystore should error")
	}
	if _, statErr := os.Stat(missing); statErr == nil {
		t.Error("fetchHubNodes must not create a keystore file")
	}
}

func TestNodeAction_ReturnsStatus(t *testing.T) {
	_, port := newNodesTestServer(t, "[]", `{"ok":true,"status":"active"}`)

	msg := nodeAction(port, "testnet", testKeystorePath(t), "aaaa000000000001", "approve")()
	am, ok := msg.(nodeActionMsg)
	if !ok {
		t.Fatalf("got %T, want nodeActionMsg", msg)
	}
	if am.err != nil || am.status != "active" || am.nodeID != "aaaa000000000001" || am.action != "approve" {
		t.Errorf("msg %+v", am)
	}
	if am.network != "testnet" {
		t.Errorf("network = %q, want testnet", am.network)
	}
}

func TestNodeAction_HubDownReturnsError(t *testing.T) {
	msg := nodeAction(1, "testnet", testKeystorePath(t), "aaaa000000000001", "approve")()
	am := msg.(nodeActionMsg)
	if am.err == nil {
		t.Fatal("port 1 should refuse connections")
	}
}
