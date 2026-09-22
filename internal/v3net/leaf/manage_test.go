package leaf

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// recordedCall is what the fake hub saw for one management request.
type recordedCall struct {
	method, path, nodeID, body string
}

// newManageHub serves canned JSON for the management endpoints and records
// each request so the tests can check the path, method, signature headers and
// body the leaf sent.
func newManageHub(t *testing.T, status int, reply string) (*httptest.Server, *[]recordedCall) {
	t.Helper()
	var calls []recordedCall
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		calls = append(calls, recordedCall{r.Method, r.URL.Path, r.Header.Get("X-V3Net-Node-ID"), string(b)})
		if r.Header.Get("X-V3Net-Signature") == "" {
			t.Errorf("%s %s arrived unsigned", r.Method, r.URL.Path)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(reply))
	}))
	t.Cleanup(ts.Close)
	return ts, &calls
}

func TestListProposalsDecodesHubReply(t *testing.T) {
	ts, calls := newManageHub(t, 200, `[{"id":"p1","tag":"test.chat","name":"Chat","from_bbs":"Sector 7","status":"pending"}]`)
	l, _ := setupLeaf(t, ts.URL, nil)

	got, err := l.ListProposals(context.Background())
	if err != nil {
		t.Fatalf("ListProposals: %v", err)
	}
	if len(got) != 1 || got[0].ID != "p1" || got[0].Tag != "test.chat" {
		t.Fatalf("unexpected proposals %+v", got)
	}
	c := (*calls)[0]
	if c.method != "GET" || c.path != "/v3net/v1/testnet/areas/proposals" {
		t.Errorf("sent %s %s", c.method, c.path)
	}
	if c.nodeID != l.cfg.Keystore.NodeID() {
		t.Errorf("node id header %q, want %q", c.nodeID, l.cfg.Keystore.NodeID())
	}
}

func TestApproveAndRejectProposalPaths(t *testing.T) {
	ts, calls := newManageHub(t, 200, `{"ok":true}`)
	l, _ := setupLeaf(t, ts.URL, nil)
	ctx := context.Background()

	if err := l.ApproveProposal(ctx, "p1", protocol.ProposalApproveRequest{AccessMode: "approval"}); err != nil {
		t.Fatalf("ApproveProposal: %v", err)
	}
	if err := l.RejectProposal(ctx, "p2", protocol.ProposalRejectRequest{Reason: "duplicate"}); err != nil {
		t.Fatalf("RejectProposal: %v", err)
	}

	want := []struct{ path, bodyContains string }{
		{"/v3net/v1/testnet/areas/proposals/p1/approve", `"access_mode":"approval"`},
		{"/v3net/v1/testnet/areas/proposals/p2/reject", `"reason":"duplicate"`},
	}
	for i, w := range want {
		c := (*calls)[i]
		if c.method != "POST" || c.path != w.path || !strings.Contains(c.body, w.bodyContains) {
			t.Errorf("call %d: %s %s body %s, want POST %s containing %s", i, c.method, c.path, c.body, w.path, w.bodyContains)
		}
	}
}

func TestAccessRequestRoundTrip(t *testing.T) {
	ts, calls := newManageHub(t, 200, `[{"node_id":"ABCD1234","bbs_name":"Sector 7","bbs_host":"s7.example","requested_at":"2026-09-01T00:00:00Z"}]`)
	l, _ := setupLeaf(t, ts.URL, nil)
	ctx := context.Background()

	reqs, err := l.ListAccessRequests(ctx, "test.chat")
	if err != nil {
		t.Fatalf("ListAccessRequests: %v", err)
	}
	if len(reqs) != 1 || reqs[0].NodeID != "ABCD1234" {
		t.Fatalf("unexpected requests %+v", reqs)
	}
	if err := l.ApproveAccess(ctx, "test.chat", []string{"ABCD1234"}); err != nil {
		t.Fatalf("ApproveAccess: %v", err)
	}
	if err := l.DenyAccess(ctx, "test.chat", []string{"FFFF0000"}, "spam"); err != nil {
		t.Fatalf("DenyAccess: %v", err)
	}

	paths := []string{
		"/v3net/v1/testnet/areas/test.chat/access/requests",
		"/v3net/v1/testnet/areas/test.chat/access/approve",
		"/v3net/v1/testnet/areas/test.chat/access/deny",
	}
	for i, p := range paths {
		if (*calls)[i].path != p {
			t.Errorf("call %d path %s, want %s", i, (*calls)[i].path, p)
		}
	}
	var deny protocol.NodeIDsRequest
	if err := json.Unmarshal([]byte((*calls)[2].body), &deny); err != nil || deny.Reason != "spam" || len(deny.NodeIDs) != 1 {
		t.Errorf("deny body %s did not carry node id and reason", (*calls)[2].body)
	}
}

// A 403 from the hub carries {"error":"coordinator only"}; the caller should
// see that text rather than a bare status code.
func TestManagementCallSurfacesHubError(t *testing.T) {
	ts, _ := newManageHub(t, 403, `{"error":"coordinator only"}`)
	l, _ := setupLeaf(t, ts.URL, nil)

	_, err := l.ListProposals(context.Background())
	if err == nil || !strings.Contains(err.Error(), "coordinator only") {
		t.Fatalf("expected hub error text, got %v", err)
	}
	if err := l.ApproveAccess(context.Background(), "test.chat", []string{"X"}); err == nil || !strings.Contains(err.Error(), "coordinator only") {
		t.Fatalf("expected hub error text, got %v", err)
	}
}
