package menu

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// manageFake is a V3NetStatusProvider whose NAL, proposal queue and access
// request queue live in memory, recording every approve, deny and reject so
// the screen tests can assert what reached the hub.
type manageFake struct {
	fakeV3NetStatus
	proposals []protocol.AreaProposal
	requests  map[string][]protocol.AccessRequest // by area tag
	calls     []string
	listErr   error
}

func (f *manageFake) ListProposals(context.Context, string) ([]protocol.AreaProposal, error) {
	return f.proposals, f.listErr
}
func (f *manageFake) ApproveProposal(_ context.Context, net, id string, _ protocol.ProposalApproveRequest) error {
	f.calls = append(f.calls, "approve-proposal "+net+" "+id)
	f.proposals = nil
	return nil
}
func (f *manageFake) RejectProposal(_ context.Context, net, id string, req protocol.ProposalRejectRequest) error {
	f.calls = append(f.calls, "reject-proposal "+net+" "+id+" "+req.Reason)
	f.proposals = nil
	return nil
}
func (f *manageFake) ListAccessRequests(_ context.Context, _, tag string) ([]protocol.AccessRequest, error) {
	return f.requests[tag], f.listErr
}
func (f *manageFake) ApproveAccess(_ context.Context, net, tag string, ids []string) error {
	f.calls = append(f.calls, "approve-access "+net+" "+tag+" "+strings.Join(ids, ","))
	delete(f.requests, tag)
	return nil
}
func (f *manageFake) DenyAccess(_ context.Context, net, tag string, ids []string, reason string) error {
	f.calls = append(f.calls, "deny-access "+net+" "+tag+" "+strings.Join(ids, ",")+" "+reason)
	delete(f.requests, tag)
	return nil
}

func newManageFake(coordinator bool) *manageFake {
	coord := "OTHER"
	if coordinator {
		coord = "TEST"
	}
	return &manageFake{
		fakeV3NetStatus: fakeV3NetStatus{network: "testnet", nal: &protocol.NAL{
			Network:     "testnet",
			CoordNodeID: coord,
			Areas: []protocol.Area{
				{Tag: "test.mine", Name: "Mine", ManagerNodeID: "TEST"},
				{Tag: "test.theirs", Name: "Theirs", ManagerNodeID: "SOMEONE"},
			},
		}},
		requests: map[string][]protocol.AccessRequest{},
	}
}

func runManageScreen(t *testing.T, fake *manageFake, input string, fn RunnableFunc) string {
	t.Helper()
	e := &MenuExecutor{V3NetStatus: fake, RootConfigPath: t.TempDir()}
	ts := newTestSession(input)
	c := &cmdCtx{
		e: e, s: ts, terminal: newTestTerminal(ts),
		currentUser: &user.User{Handle: "Sysop", AccessLevel: 255},
		outputMode:  ansi.OutputModeUTF8, termWidth: 80, termHeight: 24,
	}
	if _, _, err := fn(c, ""); err != nil && !errors.Is(err, errInputAborted) {
		t.Fatalf("screen returned error: %v", err)
	}
	return ts.output()
}

func TestAccessRequestsListsOnlyManagedAreasAndApproves(t *testing.T) {
	fake := newManageFake(false)
	fake.requests["test.mine"] = []protocol.AccessRequest{{NodeID: "AAAA1111", BBSName: "Sector 7", RequestedAt: time.Now().Add(-2 * time.Hour).Format(time.RFC3339)}}
	fake.requests["test.theirs"] = []protocol.AccessRequest{{NodeID: "BBBB2222", BBSName: "Not Mine"}}

	out := runManageScreen(t, fake, "A 1\r\r", runV3NetAccessRequests)

	if !strings.Contains(out, "Sector 7") || strings.Contains(out, "Not Mine") {
		t.Errorf("expected only the managed area's request to be listed, got %q", out)
	}
	if !strings.Contains(out, "2h ago") {
		t.Errorf("expected a relative age, got %q", out)
	}
	if len(fake.calls) != 1 || fake.calls[0] != "approve-access testnet test.mine AAAA1111" {
		t.Errorf("hub calls %v, want one approve for AAAA1111", fake.calls)
	}
	if !strings.Contains(out, "Approved Sector 7") {
		t.Errorf("expected an approval status line, got %q", out)
	}
}

func TestAccessRequestsDenyCarriesReason(t *testing.T) {
	fake := newManageFake(false)
	fake.requests["test.mine"] = []protocol.AccessRequest{{NodeID: "AAAA1111", BBSName: "Sector 7"}}

	runManageScreen(t, fake, "d1\rspam\r\r", runV3NetAccessRequests)

	if len(fake.calls) != 1 || fake.calls[0] != "deny-access testnet test.mine AAAA1111 spam" {
		t.Errorf("hub calls %v, want one deny with reason", fake.calls)
	}
}

func TestAccessRequestsRejectsBadCommandWithoutCallingHub(t *testing.T) {
	fake := newManageFake(false)
	fake.requests["test.mine"] = []protocol.AccessRequest{{NodeID: "AAAA1111", BBSName: "Sector 7"}}

	out := runManageScreen(t, fake, "X 1\rA 9\rq\r", runV3NetAccessRequests)

	if len(fake.calls) != 0 {
		t.Errorf("bad commands must not reach the hub, got %v", fake.calls)
	}
	if strings.Count(out, "Enter A or D") != 2 {
		t.Errorf("expected two usage hints, got %q", out)
	}
}

func TestAccessRequestsWhenNothingManaged(t *testing.T) {
	fake := newManageFake(false)
	fake.nal.Areas[0].ManagerNodeID = "SOMEONE"

	out := runManageScreen(t, fake, "\r", runV3NetAccessRequests)
	if !strings.Contains(out, "does not manage any areas") {
		t.Errorf("expected the not-a-manager notice, got %q", out)
	}
}

func TestCoordinatorPanelRefusesNonCoordinator(t *testing.T) {
	fake := newManageFake(false)
	out := runManageScreen(t, fake, "\r", runV3NetCoordinator)
	if !strings.Contains(out, "not the coordinator") {
		t.Errorf("expected the not-coordinator notice, got %q", out)
	}
	if len(fake.calls) != 0 {
		t.Errorf("no hub calls expected, got %v", fake.calls)
	}
}

func TestCoordinatorPanelApprovesAndRejectsProposals(t *testing.T) {
	fake := newManageFake(true)
	fake.proposals = []protocol.AreaProposal{
		{ID: "p1", Tag: "test.chat", Name: "Chat", FromBBS: "Sector 7", AccessMode: "open", Status: "pending"},
		{ID: "p2", Tag: "test.warez", Name: "Warez", FromBBS: "Bad BBS", AccessMode: "open", Status: "pending"},
	}

	// Open the queue, reject #2 with a reason, then the fake empties the
	// queue, so the queue screen returns to the panel; quit.
	out := runManageScreen(t, fake, "p\rR 2\rnope\rq\r", runV3NetCoordinator)

	if !strings.Contains(out, "(2)") {
		t.Errorf("panel should show the pending count, got %q", out)
	}
	if !strings.Contains(out, "test.chat") || !strings.Contains(out, "Sector 7") {
		t.Errorf("queue should list proposals, got %q", out)
	}
	if len(fake.calls) != 1 || fake.calls[0] != "reject-proposal testnet p2 nope" {
		t.Errorf("hub calls %v, want one reject of p2", fake.calls)
	}

	fake = newManageFake(true)
	fake.proposals = []protocol.AreaProposal{{ID: "p1", Tag: "test.chat", Name: "Chat"}}
	runManageScreen(t, fake, "P\ra1\rQ\r", runV3NetCoordinator)
	if len(fake.calls) != 1 || fake.calls[0] != "approve-proposal testnet p1" {
		t.Errorf("hub calls %v, want one approve of p1", fake.calls)
	}
}

func TestCoordinatorPanelShowsHubError(t *testing.T) {
	fake := newManageFake(true)
	fake.listErr = errors.New("hub: coordinator only")
	out := runManageScreen(t, fake, "q\r", runV3NetCoordinator)
	if !strings.Contains(out, "coordinator only") {
		t.Errorf("expected the hub error on screen, got %q", out)
	}
}

func TestV3NetAgoAcceptsHubAndRFC3339Timestamps(t *testing.T) {
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	cases := map[string]string{
		"2026-09-22 09:30:00":  "2h ago", // SQLite datetime('now'), as the hub stores it
		"2026-09-20T12:00:00Z": "2d ago", // RFC 3339
		"2026-09-22 11:59:40":  "just now",
		"garbage":              "garbage",
	}
	for in, want := range cases {
		if got := v3netAgo(in, now); got != want {
			t.Errorf("v3netAgo(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestV3NetRolesToleratesNilNAL(t *testing.T) {
	fake := &fakeV3NetStatus{network: "testnet"} // nal left nil
	coord, managed, errs := v3netRoles(context.Background(), fake)
	if len(coord) != 0 || len(managed) != 0 || len(errs) != 1 || !strings.Contains(errs[0], "no NAL") {
		t.Fatalf("got coord=%v managed=%v errs=%v", coord, managed, errs)
	}
}

func TestParseListCommand(t *testing.T) {
	cases := []struct {
		in     string
		action byte
		row    int
		ok     bool
	}{
		{"A 3", 'A', 3, true}, {"a3", 'A', 3, true}, {" d 12 ", 'D', 12, true},
		{"q", 'Q', 0, false}, {"", 0, 0, false}, {"A 0", 'A', 0, false}, {"A x", 'A', 0, false},
	}
	for _, tc := range cases {
		action, row, ok := parseListCommand(tc.in)
		if action != tc.action || row != tc.row || ok != tc.ok {
			t.Errorf("parseListCommand(%q) = (%q, %d, %v), want (%q, %d, %v)", tc.in, action, row, ok, tc.action, tc.row, tc.ok)
		}
	}
}
