package hub

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/keystore"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// setupProposalTest starts a manual-review hub with an empty NAL, the hub
// operator registered as coordinator and one active leaf.
func setupProposalTest(t *testing.T) (*Hub, *httptest.Server, *keystore.Keystore, *keystore.Keystore) {
	t.Helper()
	h, hubKS := setupTestHubManual(t)
	ts := httptest.NewServer(h.newMux())
	t.Cleanup(ts.Close)
	leafKS := loadTestKeystore(t, "leaf.key")
	registerAndActivate(t, ts, h, hubKS, "Hub BBS", "hub.example.net")
	registerAndActivate(t, ts, h, leafKS, "Leaf BBS", "leaf.example.net")
	seedTestNALForProposals(t, h, hubKS)
	return h, ts, hubKS, leafKS
}

// propose submits a proposal body as ks and returns the hub's response.
func propose(t *testing.T, ts *httptest.Server, ks *keystore.Keystore, body string) protocol.ProposalResponse {
	t.Helper()
	var resp protocol.ProposalResponse
	if code := sendSigned(t, ks, "POST", ts.URL+"/v3net/v1/testnet/areas/propose", body, &resp); code != http.StatusOK {
		t.Fatalf("propose: expected 200, got %d", code)
	}
	return resp
}

func TestPropose_Validation(t *testing.T) {
	h, ts, _, leafKS := setupProposalTest(t)
	url := ts.URL + "/v3net/v1/testnet/areas/propose"

	cases := []struct {
		name, body string
		want       int
	}{
		{"invalid JSON", `{"tag":`, http.StatusBadRequest},
		{"missing name", `{"tag":"gen.test"}`, http.StatusBadRequest},
		{"bad access mode", `{"tag":"gen.test","name":"Test","access_mode":"secret"}`, http.StatusUnprocessableEntity},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if code := sendSigned(t, leafKS, "POST", url, tc.body, nil); code != tc.want {
				t.Errorf("expected %d, got %d", tc.want, code)
			}
		})
	}

	pending, err := h.proposals.ListPending("testnet")
	if err != nil {
		t.Fatalf("ListPending: %v", err)
	}
	if len(pending) != 0 {
		t.Errorf("rejected proposals must not be stored, got %+v", pending)
	}
}

func TestPropose_DefaultsAndStoreGet(t *testing.T) {
	h, ts, _, leafKS := setupProposalTest(t)

	ch, cancel := h.broadcaster.Subscribe("testnet")
	defer cancel()

	// Language and access mode are optional and default to en / open.
	resp := propose(t, ts, leafKS, `{"tag":"gen.minimal","name":"Minimal"}`)
	if !resp.OK || resp.Status != "pending" || resp.ProposalID == "" {
		t.Fatalf("unexpected propose response: %+v", resp)
	}

	var ev protocol.AreaProposedPayload
	if err := json.Unmarshal(waitEvent(t, ch, protocol.EventAreaProposed).Data, &ev); err != nil {
		t.Fatalf("decode area_proposed: %v", err)
	}
	want := protocol.AreaProposedPayload{
		Network: "testnet", Tag: "gen.minimal", FromNode: leafKS.NodeID(), ProposalID: resp.ProposalID,
	}
	if ev != want {
		t.Errorf("area_proposed = %+v, want %+v", ev, want)
	}

	p, err := h.proposals.Get(resp.ProposalID)
	if err != nil || p == nil {
		t.Fatalf("Get proposal: %v, %v", p, err)
	}
	if p.Tag != "gen.minimal" || p.Name != "Minimal" || p.Language != "en" ||
		p.AccessMode != protocol.AccessModeOpen || p.AllowANSI || p.FromNode != leafKS.NodeID() ||
		p.Status != "pending" {
		t.Errorf("unexpected stored proposal: %+v", p)
	}

	missing, err := h.proposals.Get("no-such-id")
	if err != nil || missing != nil {
		t.Errorf("Get(unknown) = %v, %v; want nil, nil", missing, err)
	}
}

func TestListProposals_EmptyAndStorageError(t *testing.T) {
	h, ts, hubKS, _ := setupProposalTest(t)
	url := ts.URL + "/v3net/v1/testnet/areas/proposals"

	// With nothing queued the coordinator gets an empty array, not null.
	var raw json.RawMessage
	if code := sendSigned(t, hubKS, "GET", url, "", &raw); code != http.StatusOK {
		t.Fatalf("list: expected 200, got %d", code)
	}
	if strings.TrimSpace(string(raw)) != "[]" {
		t.Errorf("empty proposal list body = %s, want []", raw)
	}

	if _, err := h.db.Exec(`DROP TABLE area_proposals`); err != nil {
		t.Fatalf("drop table: %v", err)
	}
	if code := sendSigned(t, hubKS, "GET", url, "", nil); code != http.StatusInternalServerError {
		t.Errorf("list with table gone: expected 500, got %d", code)
	}
	if _, err := h.proposals.Get("any"); err == nil {
		t.Error("Get with table gone should fail")
	}
	if err := h.proposals.Resolve("any", "approved", ""); err == nil {
		t.Error("Resolve with table gone should fail")
	}
}

func TestApproveProposal_Errors(t *testing.T) {
	h, ts, hubKS, leafKS := setupProposalTest(t)
	id := propose(t, ts, leafKS, proposalBody).ProposalID
	base := ts.URL + "/v3net/v1/testnet/areas/proposals/"

	if code := sendSigned(t, leafKS, "POST", base+id+"/approve", "", nil); code != http.StatusForbidden {
		t.Errorf("approve as non-coordinator: expected 403, got %d", code)
	}
	if code := sendSigned(t, hubKS, "POST", base+"no-such-id/approve", "", nil); code != http.StatusNotFound {
		t.Errorf("approve unknown proposal: expected 404, got %d", code)
	}
	if code := sendSigned(t, hubKS, "POST", base+id+"/approve", `{"access_mode":`, nil); code != http.StatusBadRequest {
		t.Errorf("approve with invalid JSON: expected 400, got %d", code)
	}

	// None of the failures may have resolved the proposal or touched the NAL.
	if p, _ := h.proposals.Get(id); p == nil || p.Status != "pending" {
		t.Errorf("proposal should still be pending, got %+v", p)
	}
	if areas := fetchNAL(t, ts).Areas; len(areas) != 0 {
		t.Errorf("NAL should still be empty, got %+v", areas)
	}

	// An empty body is accepted; a second approval finds nothing pending.
	if code := sendSigned(t, hubKS, "POST", base+id+"/approve", "", nil); code != http.StatusOK {
		t.Fatalf("approve: expected 200, got %d", code)
	}
	if code := sendSigned(t, hubKS, "POST", base+id+"/approve", "", nil); code != http.StatusNotFound {
		t.Errorf("approve already-approved proposal: expected 404, got %d", code)
	}
}

func TestApproveProposal_DuplicateTagDoesNotDuplicateArea(t *testing.T) {
	h, ts, hubKS, leafKS := setupProposalTest(t)
	first := propose(t, ts, leafKS, proposalBody).ProposalID
	second := propose(t, ts, leafKS, proposalBody).ProposalID
	base := ts.URL + "/v3net/v1/testnet/areas/proposals/"

	for _, id := range []string{first, second} {
		if code := sendSigned(t, hubKS, "POST", base+id+"/approve", "", nil); code != http.StatusOK {
			t.Fatalf("approve %s: expected 200, got %d", id, code)
		}
	}

	if areas := fetchNAL(t, ts).Areas; len(areas) != 1 || areas[0].Tag != "gen.test" {
		t.Errorf("expected a single gen.test area, got %+v", areas)
	}
	// The redundant proposal is still closed out so it leaves the queue.
	if p, _ := h.proposals.Get(second); p == nil || p.Status != "approved" {
		t.Errorf("second proposal should be resolved as approved, got %+v", p)
	}
	var reason string
	if err := h.db.QueryRow(`SELECT reason FROM area_proposals WHERE id = ?`, second).Scan(&reason); err != nil {
		t.Fatalf("read reason: %v", err)
	}
	if reason != "area already exists" {
		t.Errorf("reason = %q, want %q", reason, "area already exists")
	}
}

func TestApproveProposal_CreatesNALWhenNoneExists(t *testing.T) {
	h, hubKS := setupTestHub(t) // auto-approves areas, no NAL seeded
	ts := httptest.NewServer(h.newMux())
	defer ts.Close()
	leafKS := loadTestKeystore(t, "leaf.key")
	registerLeaf(t, ts, leafKS)

	resp := propose(t, ts, leafKS, proposalBody)
	if resp.Status != "approved" {
		t.Fatalf("expected auto-approved proposal, got %+v", resp)
	}

	n := fetchNAL(t, ts)
	if n.Network != "testnet" || n.CoordNodeID != hubKS.NodeID() {
		t.Errorf("bootstrapped NAL network/coordinator = %q/%q", n.Network, n.CoordNodeID)
	}
	area := n.FindArea("gen.test")
	if area == nil {
		t.Fatal("gen.test missing from bootstrapped NAL")
	}
	if area.ManagerNodeID != leafKS.NodeID() || area.ManagerPubKeyB64 != leafKS.PubKeyBase64() {
		t.Errorf("manager = %q, want proposer %q", area.ManagerNodeID, leafKS.NodeID())
	}
	if !area.Policy.AllowANSI || area.Policy.MaxBodyBytes != protocol.MaxBodyBytes {
		t.Errorf("unexpected area policy: %+v", area.Policy)
	}
}

func TestRejectProposal_Errors(t *testing.T) {
	h, ts, hubKS, leafKS := setupProposalTest(t)
	id := propose(t, ts, leafKS, proposalBody).ProposalID
	base := ts.URL + "/v3net/v1/testnet/areas/proposals/"

	if code := sendSigned(t, leafKS, "POST", base+id+"/reject", `{"reason":"no"}`, nil); code != http.StatusForbidden {
		t.Errorf("reject as non-coordinator: expected 403, got %d", code)
	}
	if code := sendSigned(t, hubKS, "POST", base+"no-such-id/reject", "", nil); code != http.StatusNotFound {
		t.Errorf("reject unknown proposal: expected 404, got %d", code)
	}
	if code := sendSigned(t, hubKS, "POST", base+id+"/reject", `{"reason":`, nil); code != http.StatusBadRequest {
		t.Errorf("reject with invalid JSON: expected 400, got %d", code)
	}
	if p, _ := h.proposals.Get(id); p == nil || p.Status != "pending" {
		t.Errorf("proposal should still be pending, got %+v", p)
	}

	ch, cancel := h.broadcaster.Subscribe("testnet")
	defer cancel()

	// The reason is optional: an empty body rejects without one.
	if code := sendSigned(t, hubKS, "POST", base+id+"/reject", "", nil); code != http.StatusOK {
		t.Fatalf("reject: expected 200, got %d", code)
	}
	var ev protocol.ProposalRejectedPayload
	if err := json.Unmarshal(waitEvent(t, ch, protocol.EventProposalRejected).Data, &ev); err != nil {
		t.Fatalf("decode proposal_rejected: %v", err)
	}
	want := protocol.ProposalRejectedPayload{Network: "testnet", Tag: "gen.test", NodeID: leafKS.NodeID()}
	if ev != want {
		t.Errorf("proposal_rejected = %+v, want %+v", ev, want)
	}
	if p, _ := h.proposals.Get(id); p == nil || p.Status != "rejected" {
		t.Errorf("proposal should be rejected, got %+v", p)
	}

	// A rejected proposal can be neither rejected again nor approved.
	if code := sendSigned(t, hubKS, "POST", base+id+"/reject", "", nil); code != http.StatusNotFound {
		t.Errorf("second reject: expected 404, got %d", code)
	}
	if code := sendSigned(t, hubKS, "POST", base+id+"/approve", "", nil); code != http.StatusNotFound {
		t.Errorf("approve rejected proposal: expected 404, got %d", code)
	}
	if areas := fetchNAL(t, ts).Areas; len(areas) != 0 {
		t.Errorf("rejected proposal must not reach the NAL, got %+v", areas)
	}
}
