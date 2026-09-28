package menu

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// fakeV3NetStatus is a minimal V3NetStatusProvider backed by a single
// in-memory NAL, standing in for a hub HTTP fetch in tests.
type fakeV3NetStatus struct {
	network string
	hubURL  string
	nal     *protocol.NAL
}

func (f *fakeV3NetStatus) NodeID() string                 { return "TEST" }
func (f *fakeV3NetStatus) HubActive() bool                { return false }
func (f *fakeV3NetStatus) LeafCount() int                 { return 1 }
func (f *fakeV3NetStatus) LeafNetworks() []string         { return []string{f.network} }
func (f *fakeV3NetStatus) NetworkForArea(int) string      { return f.network }
func (f *fakeV3NetStatus) HubURLForNetwork(string) string { return f.hubURL }
func (f *fakeV3NetStatus) RegistryURL() string            { return "" }
func (f *fakeV3NetStatus) FetchNALForNetwork(ctx context.Context, network string) (*protocol.NAL, error) {
	return f.nal, nil
}
func (f *fakeV3NetStatus) ProposeArea(network string, req protocol.AreaProposalRequest) (*protocol.ProposalResponse, error) {
	return nil, nil
}
func (f *fakeV3NetStatus) ListProposals(context.Context, string) ([]protocol.AreaProposal, error) {
	return nil, nil
}
func (f *fakeV3NetStatus) ApproveProposal(context.Context, string, string, protocol.ProposalApproveRequest) error {
	return nil
}
func (f *fakeV3NetStatus) RejectProposal(context.Context, string, string, protocol.ProposalRejectRequest) error {
	return nil
}
func (f *fakeV3NetStatus) SetAreaManager(context.Context, string, string, string) error {
	return nil
}
func (f *fakeV3NetStatus) ListAccessRequests(context.Context, string, string) ([]protocol.AccessRequest, error) {
	return nil, nil
}
func (f *fakeV3NetStatus) ApproveAccess(context.Context, string, string, []string) error { return nil }
func (f *fakeV3NetStatus) DenyAccess(context.Context, string, string, []string, string) error {
	return nil
}

// runV3NetAreas builds each row as " %s %-24s %-28s %-8s %s" (status, tag,
// name, access, network) then clamps the whole line to termWidth-1 with
// len(line) and line[:maxW] — both byte counts. tag/name are already padded
// to a fixed rune width via padRight/truncateStr, but the trailing network
// name is appended unpadded: a multi-byte network name can push the line's
// byte length over the limit even while its visible (rune) width still fits,
// triggering a truncation that lands mid-rune. Network names are hub-supplied
// (this NAL stands in for the hub's HTTP response), so this is reachable
// without local misconfiguration.
func TestRunV3NetAreasRowNotSplitMidRune(t *testing.T) {
	// 10 CJK runes = 30 bytes. Prefix (status+tag+name+access, all ASCII,
	// padded) is exactly 68 visible columns/bytes, so the full row is 78
	// visible columns (fits under maxW=79) but 98 bytes (over it).
	network := strings.Repeat("日", 10)

	nal := &protocol.NAL{
		Network: network,
		Areas: []protocol.Area{
			{
				Tag:    "aa.bb",
				Name:   "TestArea",
				Access: protocol.AreaAccess{Mode: "open"},
			},
		},
	}

	e := &MenuExecutor{
		V3NetStatus:    &fakeV3NetStatus{network: network, hubURL: "https://hub.example", nal: nal},
		RootConfigPath: t.TempDir(),
	}
	ts := newTestSession("q")
	terminal := newTestTerminal(ts)

	c := &cmdCtx{
		e: e, s: ts, terminal: terminal, currentUser: &user.User{Handle: "Tester", AccessLevel: 255},
		outputMode: ansi.OutputModeUTF8, termWidth: 80, termHeight: 24,
	}

	if _, _, err := runV3NetAreas(c, ""); err != nil {
		t.Fatalf("runV3NetAreas: %v", err)
	}

	out := ts.output()
	// The item row's Network column should render the full, intact name.
	// A byte-based clamp instead truncates it mid-rune, e.g. to
	// "日日日µù" (3 intact runes plus stray CP437 glyphs from the dangling bytes).
	if !strings.Contains(out, "open     "+network) {
		t.Errorf("item row does not contain the intact network name %q after \"open\": %q", network, out)
	}
}

// v3netScreenFake is a fakeV3NetStatus whose leaf networks, NAL fetch error
// and proposal submission are scripted, recording every proposal it gets.
type v3netScreenFake struct {
	fakeV3NetStatus
	nets       []string
	fetchErr   error
	proposeErr error
	proposals  []protocol.AreaProposalRequest
}

func (f *v3netScreenFake) LeafNetworks() []string { return f.nets }
func (f *v3netScreenFake) FetchNALForNetwork(ctx context.Context, network string) (*protocol.NAL, error) {
	if f.fetchErr != nil {
		return nil, f.fetchErr
	}
	return f.nal, nil
}
func (f *v3netScreenFake) ProposeArea(network string, req protocol.AreaProposalRequest) (*protocol.ProposalResponse, error) {
	if f.proposeErr != nil {
		return nil, f.proposeErr
	}
	f.proposals = append(f.proposals, req)
	return &protocol.ProposalResponse{ProposalID: "prop-1", Status: "pending"}, nil
}

// newV3NetScreenFake serves a NAL for network with the given area tags.
func newV3NetScreenFake(network string, tags ...string) *v3netScreenFake {
	nal := &protocol.NAL{Network: network}
	for _, tag := range tags {
		nal.Areas = append(nal.Areas, protocol.Area{Tag: tag, Name: "Name of " + tag, Access: protocol.AreaAccess{Mode: "open"}})
	}
	return &v3netScreenFake{
		fakeV3NetStatus: fakeV3NetStatus{network: network, hubURL: "https://hub.invalid", nal: nal},
		nets:            []string{network},
	}
}

// v3netLeaf returns the leaf for network in env's v3net.json, if any.
func v3netLeaf(t *testing.T, env *menuEnv, network string) (config.V3NetLeafConfig, bool) {
	t.Helper()
	cfg, err := config.LoadV3NetConfig(env.cfgDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range cfg.Leaves {
		if l.Network == network {
			return l, true
		}
	}
	return config.V3NetLeafConfig{}, false
}

// TestV3NetAreasSubscribeAndUnsubscribe pins V3NETAREAS' Space toggle on a
// network with no leaf yet: it asks whether new areas join newscan, then
// writes a leaf holding the board and creates the local message area with
// that choice; Space again removes the leaf but keeps the message area.
func TestV3NetAreasSubscribeAndUnsubscribe(t *testing.T) {
	env := newMenuEnv(t)
	env.e.V3NetStatus = newV3NetScreenFake("testnet", "tst.general", "tst.chat")
	reloads := 0
	env.e.V3NetReload = func() error { reloads++; return nil }

	r := env.runCmd("V3NETAREAS", env.sysop, "", " Nq")
	if !r.has("V3Net Areas: testnet", "tst.general", "tst.chat", "Add testnet areas to users' newscan by default?",
		"Subscribed to tst.general. Now active.") {
		t.Errorf("subscribe screen:\n%s", r.text())
	}
	leaf, ok := v3netLeaf(t, env, "testnet")
	if !ok || len(leaf.Boards) != 1 || leaf.Boards[0] != "tst.general" || leaf.HubURL != "https://hub.invalid" || leaf.AutoJoinEnabled() {
		t.Fatalf("leaf = %+v (found %v), want tst.general with auto-join off", leaf, ok)
	}
	area, ok := env.e.MessageMgr.GetAreaByTag("tst.general")
	if !ok || area.AreaType != "v3net" || area.Network != "testnet" || area.AutoJoin {
		t.Fatalf("message area = %+v (found %v)", area, ok)
	}

	// A second board joins the existing leaf without asking again.
	r = env.runCmd("V3NETAREAS", env.sysop, "", "\x1b[B q")
	if r.has("newscan by default?") {
		t.Errorf("asked again for a network that already has a leaf:\n%s", r.text())
	}
	if leaf, _ := v3netLeaf(t, env, "testnet"); len(leaf.Boards) != 2 {
		t.Fatalf("boards = %v, want both", leaf.Boards)
	}

	env.runCmd("V3NETAREAS", env.sysop, "", " \x1b[B q")
	if _, ok := v3netLeaf(t, env, "testnet"); ok {
		t.Error("leaf kept after unsubscribing every board")
	}
	if _, ok := env.e.MessageMgr.GetAreaByTag("tst.general"); !ok {
		t.Error("unsubscribing deleted the message area")
	}
	if reloads != 4 {
		t.Errorf("reloads = %d, want one per toggle (4)", reloads)
	}
}

// TestV3NetAreasReloadFailureSaysRestart pins that when live apply fails the
// change is still saved and the status says a restart is needed.
func TestV3NetAreasReloadFailureSaysRestart(t *testing.T) {
	env := newMenuEnv(t)
	env.e.V3NetStatus = newV3NetScreenFake("felonynet", "fel.one")
	env.e.V3NetReload = func() error { return errors.New("hub down") }

	r := env.runCmd("V3NETAREAS", env.sysop, "", " q")
	if !r.has("Live apply failed (hub down); restart to activate.") {
		t.Errorf("subscribe status:\n%s", r.text())
	}
	if leaf, _ := v3netLeaf(t, env, "felonynet"); len(leaf.Boards) != 1 || leaf.Boards[0] != "fel.one" {
		t.Errorf("felonynet boards = %v, want [fel.one]", leaf.Boards)
	}
	r = env.runCmd("V3NETAREAS", env.sysop, "", " q")
	if !r.has("Live apply failed (hub down); restart to apply.") {
		t.Errorf("unsubscribe status:\n%s", r.text())
	}

	// Unsubscribing the last board dropped the leaf, so subscribing asks
	// the newscan question again.
	env.e.V3NetReload = nil
	if r := env.runCmd("V3NETAREAS", env.sysop, "", " Yq"); !r.has("Subscribed to fel.one. Restart to activate.") {
		t.Errorf("no-reload subscribe:\n%s", r.text())
	}
	if r := env.runCmd("V3NETAREAS", env.sysop, "", " q"); !r.has("Unsubscribed from fel.one. Restart to apply.") {
		t.Errorf("no-reload unsubscribe:\n%s", r.text())
	}
}

// TestV3NetAreasNavigatesLongList pins the lightbar's paging over more areas
// than fit: End reaches the last area, Home returns to the first, and the
// position counter follows.
func TestV3NetAreasNavigatesLongList(t *testing.T) {
	env := newMenuEnv(t)
	tags := make([]string, 40)
	for i := range tags {
		tags[i] = fmt.Sprintf("tst.a%02d", i)
	}
	env.e.V3NetStatus = newV3NetScreenFake("testnet", tags...)

	r := env.runCmd("V3NETAREAS", env.sysop, "", "\x1b[F\x1b[A\x1b[6~\x1b[5~\x1b[H\x1b[Bq")
	if !r.has("tst.a39", "40/40", "39/40", "2/40") {
		t.Errorf("navigation:\n%s", r.text())
	}
	if r := env.runCmd("V3NETAREAS", env.sysop, "", ""); r.next != "LOGOFF" {
		t.Errorf("disconnect: next = %q", r.next)
	}
}

// TestV3NetAreasMessages pins V3NETAREAS' notices: no leaf networks, a hub
// with no NAL published (404), another fetch error, and an empty NAL.
func TestV3NetAreasMessages(t *testing.T) {
	env := newMenuEnv(t)
	fake := newV3NetScreenFake("testnet")
	env.e.V3NetStatus = fake

	fake.nets = nil
	if r := env.runCmd("V3NETAREAS", env.sysop, "", "\r"); !r.has("No V3Net subscriptions configured.") {
		t.Errorf("no networks:\n%s", r.text())
	}
	fake.nets = []string{"testnet"}
	fake.fetchErr = errors.New("status 404")
	if r := env.runCmd("V3NETAREAS", env.sysop, "", "\r"); !r.has("No area list published for testnet yet.") {
		t.Errorf("404:\n%s", r.text())
	}
	fake.fetchErr = errors.New("connection refused")
	if r := env.runCmd("V3NETAREAS", env.sysop, "", "\r"); !r.has("Could not fetch area list: testnet: connection refused") {
		t.Errorf("fetch error:\n%s", r.text())
	}
	fake.fetchErr = nil
	if r := env.runCmd("V3NETAREAS", env.sysop, "othernet", "\r"); !r.has("No areas found in NAL.") {
		t.Errorf("empty NAL:\n%s", r.text())
	}
	env.e.V3NetStatus = nil
	if r := env.runCmd("V3NETAREAS", env.sysop, "", "\r"); r.raw != "" {
		t.Errorf("V3Net disabled should print nothing:\n%s", r.text())
	}
}
