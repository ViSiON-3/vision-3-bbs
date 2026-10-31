package menu

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// hostileV3NetText holds a screen code, raw CSI and OSC sequences and the 8-bit
// form of CSI, as a hub or another node could send them.
const hostileV3NetText = "Evil|CL\x1b[2J\x1b]0;owned\x07\u009b2J"

// assertV3NetTextSafe fails if hostileV3NetText acted on the screen in out. The
// screens clear themselves, so a clear only counts straight after the hub's text.
func assertV3NetTextSafe(t *testing.T, out string) {
	t.Helper()
	for _, bad := range []string{"Evil\x1b", "|CL\x1b", "||CL", "\x1b]0;", "\x07", "\u009b"} {
		if strings.Contains(out, bad) {
			t.Errorf("hub text reached the screen as %q: %q", bad, out)
		}
	}
	if !strings.Contains(out, "Evil|CL") {
		t.Errorf("hub text should show its pipes as text: %q", out)
	}
}

// A hub's area name travels from the NAL through the queued notice to the
// sysop's login screen.
func TestSysopNoticeOfferDoesNotRunHubScreenCodes(t *testing.T) {
	e, um := newAreaFixture(t)
	mm, err := message.NewMessageManager(t.TempDir(), t.TempDir(), "TestBBS", nil)
	if err != nil {
		t.Fatal(err)
	}
	e.MessageMgr = mm

	e.NoteV3NetNAL(um, "felonynet", nalWith("fel.a"), "TEST")
	n := nalWith("fel.a", "fel.evil")
	n.Areas[1].Name = hostileV3NetText
	if queued := e.NoteV3NetNAL(um, "felonynet", n, "TEST"); queued == 0 {
		t.Fatal("no offer queued")
	}
	e.V3NetStatus = &fakeV3NetStatus{network: "felonynet", hubURL: "https://hub.example", nal: n}

	assertV3NetTextSafe(t, runNoticeScreen(t, e, 255, "n\r"))
}

func TestCoordinatorScreensDoNotRunHubText(t *testing.T) {
	fake := newManageFake(true)
	fake.proposals = []protocol.AreaProposal{{
		ID: "p1", Tag: hostileV3NetText, Name: hostileV3NetText, FromBBS: hostileV3NetText,
		AccessMode: hostileV3NetText, ProposedAt: hostileV3NetText,
	}}
	fake.nal.Areas[0].Name = hostileV3NetText
	fake.nal.Areas[0].ManagerNodeID = hostileV3NetText

	assertV3NetTextSafe(t, runManageScreen(t, fake, "P\rQ\rM\rQ\rQ\r", runV3NetCoordinator))
}

func TestCoordinatorScreensDoNotRunHubErrors(t *testing.T) {
	fake := newManageFake(true)
	fake.listErr = errors.New(hostileV3NetText)

	assertV3NetTextSafe(t, runManageScreen(t, fake, "P\rQ\r", runV3NetCoordinator))
}

func TestApprovalPromptDoesNotRunHubAccessMode(t *testing.T) {
	fake := newManageFake(true)
	fake.proposals = []protocol.AreaProposal{{ID: "p1", Tag: "test.new", Name: "New", AccessMode: hostileV3NetText}}

	assertV3NetTextSafe(t, runManageScreen(t, fake, "P\rA 1\r\rQ\r", runV3NetCoordinator))
}

func TestAccessRequestsDoNotRunNodeText(t *testing.T) {
	fake := newManageFake(false)
	fake.requests["test.mine"] = []protocol.AccessRequest{{
		NodeID: hostileV3NetText, BBSName: hostileV3NetText, RequestedAt: hostileV3NetText,
	}}

	out := runManageScreen(t, fake, "A 1\r\r", runV3NetAccessRequests)
	assertV3NetTextSafe(t, out)
	if !strings.Contains(out, "Approved Evil|CL") {
		t.Errorf("expected the approval to name the board as written: %q", out)
	}
}

// A pipe in a cell is doubled for the expander, which draws it as one column.
func TestV3NetCellKeepsItsColumnWidth(t *testing.T) {
	if got := string(ansi.ReplacePipeCodes([]byte(v3netCell("a|b", 5)))); got != "a|b  " {
		t.Errorf("v3netCell = %q", got)
	}
	// A cut through the doubled pipe would leave a lone "|" for the next
	// column's text to complete into a code.
	if got := string(ansi.ReplacePipeCodes([]byte(v3netClip("abcd|CLefgh", 7) + "CL"))); strings.Contains(got, "\x1b") {
		t.Errorf("v3netClip left a live code: %q", got)
	}
}

func TestAreaBrowserDoesNotRunHubText(t *testing.T) {
	fake := newManageFake(false)
	fake.nal.Areas = []protocol.Area{{
		Tag: "test.evil", Name: hostileV3NetText,
		Access: protocol.AreaAccess{Mode: "\x1b]0;owned\x07"},
	}}

	assertV3NetTextSafe(t, runManageScreen(t, fake, "q", runV3NetAreas))
}

type registryFake struct {
	manageFake
	url string
}

func (f *registryFake) RegistryURL() string { return f.url }

func TestRegistryScreenDoesNotRunRegistryText(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"networks": []protocol.RegistryEntry{{
			Name: hostileV3NetText, Description: hostileV3NetText, HubURL: hostileV3NetText,
		}}})
	}))
	defer ts.Close()

	fake := &registryFake{manageFake: *newManageFake(false), url: ts.URL}
	e := &MenuExecutor{V3NetStatus: fake, RootConfigPath: t.TempDir()}
	sess := newTestSession("\r")
	c := &cmdCtx{
		e: e, s: sess, terminal: newTestTerminal(sess),
		currentUser: &user.User{Handle: "Sysop", AccessLevel: 255},
		outputMode:  ansi.OutputModeUTF8, termWidth: 80, termHeight: 24,
	}
	if _, _, err := runV3NetRegistry(c, ""); err != nil && !errors.Is(err, errInputAborted) {
		t.Fatalf("runV3NetRegistry: %v", err)
	}
	assertV3NetTextSafe(t, sess.output())
}

func TestKeyNoticeDoesNotRunNetworkName(t *testing.T) {
	e, um := newAreaFixture(t)
	e.SetStrings(func() (s config.StringsConfig) { s.V3NetKeyChangedNotice = "Key for %s (%s)"; return }())
	if e.NoteV3NetKeyChange(um, hostileV3NetText, "hub") == 0 {
		t.Fatal("no notice queued")
	}
	q, err := peekSysopNotices(sysopNoticesPath(e.GetServerConfig().DataDir), 1)
	if err != nil || len(q) != 1 {
		t.Fatalf("queue = %+v, err = %v", q, err)
	}
	assertV3NetTextSafe(t, string(ansi.ReplacePipeCodes([]byte(q[0].Text))))
}

// An offer queued by an earlier build holds the hub's text. With no string to
// rebuild the question from, the stored text is what is asked.
func TestSysopNoticeStoredOfferTextIsCleaned(t *testing.T) {
	e, _ := newAreaFixture(t)
	e.SetStrings(config.StringsConfig{})
	e.V3NetStatus = &fakeV3NetStatus{network: "felonynet", hubURL: "https://hub.example", nal: nalWith("fel.a", "fel.evil")}
	n := sysopNotice{Text: "|12" + hostileV3NetText, V3NetNetwork: "felonynet", V3NetTag: "fel.evil"}
	if err := enqueueSysopNotice(sysopNoticesPath(e.GetServerConfig().DataDir), 1, n); err != nil {
		t.Fatal(err)
	}
	assertV3NetTextSafe(t, runNoticeScreen(t, e, 255, "n\r"))
}

func TestV3NetSubscribeStoresPlainNameAndRefusesBadTag(t *testing.T) {
	root := t.TempDir()
	if err := config.SaveV3NetConfig(root, config.V3NetConfig{}); err != nil {
		t.Fatal(err)
	}
	mm, err := message.NewMessageManager(t.TempDir(), t.TempDir(), "TestBBS", nil)
	if err != nil {
		t.Fatal(err)
	}
	area := protocol.Area{Tag: "fel.evil", Name: "|12" + hostileV3NetText}
	if err := v3netSubscribe(root, mm, "felonynet", "https://hub.example", area, true); err != nil {
		t.Fatal(err)
	}
	got, ok := mm.GetAreaByTag("fel.evil")
	if !ok {
		t.Fatal("area not created")
	}
	if strings.ContainsAny(got.Name, "|\x1b\x07") {
		t.Errorf("stored name still live: %q", got.Name)
	}

	for _, tag := range []string{"../../etc", "fel.|CL", "fel.a\x1b[2J"} {
		area.Tag = tag
		if err := v3netSubscribe(root, mm, "felonynet", "https://hub.example", area, true); err == nil {
			t.Errorf("tag %q was accepted", tag)
		}
	}
	if boards := v3netSubscribedBoards(root, "felonynet"); len(boards) != 1 {
		t.Errorf("boards = %v, want only fel.evil", boards)
	}
}

func TestNoteV3NetNALDoesNotOfferBadTag(t *testing.T) {
	e, um := newAreaFixture(t)
	e.NoteV3NetNAL(um, "felonynet", nalWith("fel.a"), "TEST")
	if queued := e.NoteV3NetNAL(um, "felonynet", nalWith("fel.a", "fel.|CL"), "TEST"); queued != 0 {
		t.Errorf("queued %d offers for an invalid tag", queued)
	}
}
