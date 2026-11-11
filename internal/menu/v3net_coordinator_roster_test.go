package menu

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/keystore"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// rosterFake is a coordinator's service with a roster and a directory.
type rosterFake struct {
	*manageFake
	self      *keystore.Keystore
	roster    *protocol.CentralRoster
	dir       []protocol.CentralDirectoryEntry
	published [][]protocol.CentralRosterMember
	bases     []int64
	conflict  bool
}

func (f *rosterFake) CentralRoster(string) (*protocol.CentralRoster, error) { return f.roster, nil }
func (f *rosterFake) CentralRosterCandidates(context.Context, string) ([]protocol.CentralDirectoryEntry, error) {
	return f.dir, nil
}
func (f *rosterFake) NodePubKeyB64() string { return f.self.PubKeyBase64() }
func (f *rosterFake) PublishCentralRoster(_ context.Context, network string, base int64, members []protocol.CentralRosterMember, open bool) (*protocol.CentralRoster, error) {
	f.bases = append(f.bases, base)
	if f.conflict {
		return nil, errors.New("leaf: the roster changed since this edit began")
	}
	f.published = append(f.published, members)
	f.roster = &protocol.CentralRoster{Network: network, Seq: int64(len(f.published)) * 10, Members: members, Open: open, CoordPubKeyB64: f.self.PubKeyBase64()}
	return f.roster, nil
}

func rosterKS(t *testing.T) *keystore.Keystore {
	t.Helper()
	ks, _, err := keystore.Load(filepath.Join(t.TempDir(), "k.key"))
	if err != nil {
		t.Fatal(err)
	}
	return ks
}

func listingEntry(ks *keystore.Keystore, name string) protocol.CentralDirectoryEntry {
	return protocol.CentralDirectoryEntry{Listing: protocol.CentralListing{Name: name, NodeID: ks.NodeID(), OriginPubKeyB64: ks.PubKeyBase64()}}
}

func newRosterFake(t *testing.T) (*rosterFake, *keystore.Keystore, *keystore.Keystore) {
	t.Helper()
	alpha, beta := rosterKS(t), rosterKS(t)
	return &rosterFake{
		manageFake: newManageFake(true),
		self:       rosterKS(t),
		dir:        []protocol.CentralDirectoryEntry{listingEntry(alpha, "Alpha BBS"), listingEntry(beta, "Beta BBS")},
	}, alpha, beta
}

func runRosterScreen(t *testing.T, f *rosterFake, input string) string {
	t.Helper()
	e := &MenuExecutor{V3NetStatus: f, RootConfigPath: t.TempDir()}
	ts := newTestSession(input)
	c := &cmdCtx{
		e: e, s: ts, terminal: newTestTerminal(ts),
		currentUser: &user.User{Handle: "Sysop", AccessLevel: 255},
		outputMode:  ansi.OutputModeUTF8, termWidth: 80, termHeight: 24,
	}
	if _, _, err := runV3NetRoster(c, []string{"testnet"}); err != nil && !errors.Is(err, errInputAborted) {
		t.Fatalf("screen returned error: %v", err)
	}
	return ts.output()
}

// The first roster needs a yes, warns that it turns filtering on, and
// always names this board; declining publishes nothing.
func TestRosterFirstPublish(t *testing.T) {
	f, alpha, _ := newRosterFake(t)
	out := runRosterScreen(t, f, "a1\rn\rq\r")
	if len(f.published) != 0 || !strings.Contains(out, "No roster yet") || !strings.Contains(out, "keep the roster either way") {
		t.Fatalf("declined: published %d\n%s", len(f.published), out)
	}
	runRosterScreen(t, f, "a1\ry\rq\r")
	if len(f.published) != 1 {
		t.Fatalf("published %d rosters", len(f.published))
	}
	got := map[string]bool{}
	for _, m := range f.published[0] {
		got[m.NodeID] = true
	}
	if !got[alpha.NodeID()] || !got[f.self.NodeID()] || len(got) != 2 {
		t.Errorf("first roster %v, want Alpha and this board", f.published[0])
	}
}

// With a roster: rows show who is on it; admitting, removing and adding by
// key each publish at once, without asking again.
func TestRosterAdmitRemoveByKey(t *testing.T) {
	f, alpha, beta := newRosterFake(t)
	f.roster = &protocol.CentralRoster{Network: "testnet", Seq: 1, Members: []protocol.CentralRosterMember{
		{NodeID: alpha.NodeID(), PubKeyB64: alpha.PubKeyBase64()},
	}}
	// Row 1 is Alpha (on the roster), row 2 Beta (listed, not on it).
	out := runRosterScreen(t, f, "a2\rr1\rq\r")
	if !strings.Contains(out, "1 boards on the roster") || !strings.Contains(out, "Admitted Beta BBS") || !strings.Contains(out, "Removed Alpha BBS") {
		t.Errorf("screen:\n%s", out)
	}
	if len(f.published) != 2 {
		t.Fatalf("published %d, want 2", len(f.published))
	}
	last := f.published[1]
	if len(last) != 1 || last[0].NodeID != beta.NodeID() {
		t.Errorf("roster now %v, want Beta alone", last)
	}

	gamma := rosterKS(t)
	runRosterScreen(t, f, "k\r"+gamma.PubKeyBase64()+"\rq\r")
	last = f.published[len(f.published)-1]
	if len(last) != 2 || (last[0].NodeID != gamma.NodeID() && last[1].NodeID != gamma.NodeID()) {
		t.Errorf("after K: %v", last)
	}
	out = runRosterScreen(t, f, "k\rnot-a-key\rq\r")
	if !strings.Contains(out, "not an ed25519 public key") {
		t.Errorf("bad key accepted:\n%s", out)
	}
	out = runRosterScreen(t, f, "a1\rq\r")
	if !strings.Contains(out, "already on the roster") {
		t.Errorf("admitting a member twice:\n%s", out)
	}
}

// The Coordinator Panel offers Board Roster only from a service that has
// it.
func TestCoordinatorPanelOffersRoster(t *testing.T) {
	f, _, _ := newRosterFake(t)
	e := &MenuExecutor{V3NetStatus: f, RootConfigPath: t.TempDir()}
	ts := newTestSession("q\r")
	c := &cmdCtx{e: e, s: ts, terminal: newTestTerminal(ts), currentUser: &user.User{Handle: "Sysop", AccessLevel: 255},
		outputMode: ansi.OutputModeUTF8, termWidth: 80, termHeight: 24}
	if _, _, err := runV3NetCoordinator(c, ""); err != nil && !errors.Is(err, errInputAborted) {
		t.Fatal(err)
	}
	if !strings.Contains(ts.output(), "oard roster") {
		t.Errorf("no Board roster entry:\n%s", ts.output())
	}
	plain := runManageScreen(t, newManageFake(true), "q\r", runV3NetCoordinator)
	if strings.Contains(plain, "oard roster") {
		t.Error("Board roster offered by a service without rosters")
	}
}

// Each edit names the roster it started from; a conflict publishes nothing
// and says the roster changed.
func TestRosterEditBaseAndConflict(t *testing.T) {
	f, alpha, _ := newRosterFake(t)
	f.roster = &protocol.CentralRoster{Network: "testnet", Seq: 7, CoordPubKeyB64: f.self.PubKeyBase64(),
		Members: []protocol.CentralRosterMember{{NodeID: alpha.NodeID(), PubKeyB64: alpha.PubKeyBase64()}}}
	runRosterScreen(t, f, "a2\rq\r")
	if len(f.bases) != 1 || f.bases[0] != 7 {
		t.Errorf("edit based on %v, want 7", f.bases)
	}
	f.conflict = true
	out := runRosterScreen(t, f, "r1\rq\r")
	if !strings.Contains(out, "changed since you opened it") {
		t.Errorf("conflict not reported:\n%s", out)
	}
	if len(f.published) != 1 {
		t.Errorf("a conflicting edit was published")
	}
}

// O opens the network after a yes, keeping the members; C closes it again;
// a no publishes nothing.
func TestRosterOpenClose(t *testing.T) {
	f, alpha, _ := newRosterFake(t)
	f.roster = &protocol.CentralRoster{Network: "testnet", Seq: 1, CoordPubKeyB64: f.self.PubKeyBase64(),
		Members: []protocol.CentralRosterMember{{NodeID: alpha.NodeID(), PubKeyB64: alpha.PubKeyBase64()}}}
	runRosterScreen(t, f, "o\rn\rq\r")
	if len(f.published) != 0 {
		t.Fatal("declining to open published")
	}
	out := runRosterScreen(t, f, "o\ry\rq\r")
	if !f.roster.Open || len(f.roster.Members) != 1 || !strings.Contains(out, "Opened") || !strings.Contains(out, "The network is open") {
		t.Fatalf("open: %+v\n%s", f.roster, out)
	}
	if !strings.Contains(out, "]lose") {
		t.Errorf("open roster does not offer Close:\n%s", out)
	}
	runRosterScreen(t, f, "c\ry\rq\r")
	if f.roster.Open || len(f.roster.Members) != 1 {
		t.Errorf("close: %+v", f.roster)
	}
	// Admitting while open keeps it open.
	f.roster.Open = true
	runRosterScreen(t, f, "a2\rq\r")
	if !f.roster.Open || len(f.roster.Members) != 2 {
		t.Errorf("admit while open: %+v", f.roster)
	}
}

// Status says what the roster does: an open one shows all, whether or not
// the hub still serves rosters, and a hub that stopped is called out.
func TestCentralRosterStateCell(t *testing.T) {
	for name, tc := range map[string]struct {
		info  CentralRosterInfo
		known bool
		want  []string
		not   string
	}{
		"closed":              {CentralRosterInfo{Supported: true, Held: true, Members: 3}, true, []string{"on", "3"}, "open"},
		"open":                {CentralRosterInfo{Supported: true, Held: true, Open: true, Members: 3}, true, []string{"open", "all shown"}, "stopped"},
		"open, hub stopped":   {CentralRosterInfo{Held: true, Open: true, Members: 3}, true, []string{"open", "all shown", "stopped"}, "boards"},
		"closed, hub stopped": {CentralRosterInfo{Held: true, Members: 3}, true, []string{"on", "stopped"}, "open"},
		"none":                {CentralRosterInfo{Supported: true}, true, []string{"none"}, "on,"},
	} {
		cell := string(ansi.StripAnsi(string(ansi.ReplacePipeCodes([]byte(centralRosterState(CentralNetworkInfo{SupportKnown: tc.known, Roster: tc.info}))))))
		for _, w := range tc.want {
			if !strings.Contains(cell, w) {
				t.Errorf("%s: %q lacks %q", name, cell, w)
			}
		}
		if strings.Contains(cell, tc.not) {
			t.Errorf("%s: %q has %q", name, cell, tc.not)
		}
	}
}
