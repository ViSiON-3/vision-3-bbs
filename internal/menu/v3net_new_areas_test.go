package menu

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

func nalWith(tags ...string) *protocol.NAL {
	n := &protocol.NAL{Network: "felonynet"}
	for _, tag := range tags {
		n.Areas = append(n.Areas, protocol.Area{Tag: tag, Name: "Area " + tag, Access: protocol.AreaAccess{Mode: protocol.AccessModeOpen}})
	}
	return n
}

func TestRecordV3NetAreas(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v3net_seen_areas.json")

	// The first NAL for a network only records: those areas predate us.
	if fresh, err := recordV3NetAreas(path, "felonynet", nalWith("fel.a", "fel.b"), nil); err != nil || len(fresh) != 0 {
		t.Fatalf("first sighting: fresh=%v err=%v, want none", fresh, err)
	}
	if fresh, _ := recordV3NetAreas(path, "felonynet", nalWith("fel.a", "fel.b"), nil); len(fresh) != 0 {
		t.Errorf("unchanged NAL: fresh=%v, want none", fresh)
	}

	fresh, err := recordV3NetAreas(path, "felonynet", nalWith("fel.a", "fel.b", "fel.c", "fel.d"), nil)
	if err != nil || len(fresh) != 2 || fresh[0].Tag != "fel.c" || fresh[1].Tag != "fel.d" {
		t.Fatalf("two added: fresh=%v err=%v, want fel.c and fel.d", fresh, err)
	}
	if fresh, _ := recordV3NetAreas(path, "felonynet", nalWith("fel.a", "fel.b", "fel.c", "fel.d"), nil); len(fresh) != 0 {
		t.Errorf("areas already offered came back: %v", fresh)
	}

	// Another network starts its own seen set.
	if fresh, _ := recordV3NetAreas(path, "othernet", nalWith("oth.a"), nil); len(fresh) != 0 {
		t.Errorf("first sighting of a second network: fresh=%v, want none", fresh)
	}

	// A file holding JSON null is treated as empty rather than panicking.
	if err := os.WriteFile(path, []byte("null"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := recordV3NetAreas(path, "felonynet", nalWith("fel.a"), nil); err != nil {
		t.Errorf("null file: %v", err)
	}
}

func TestRecordV3NetAreasFirstNALOffersRecentAreas(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v3net_seen_areas.json")
	now := time.Now().UTC()

	n := nalWith("fel.old", "fel.undated", "fel.recent", "fel.bad")
	n.Areas[0].Added = now.Add(-v3netRecentAreaWindow - time.Hour).Format(time.RFC3339)
	n.Areas[2].Added = now.Add(-48 * time.Hour).Format(time.RFC3339)
	n.Areas[3].Added = "last tuesday"

	// Only the area added inside the window is new to a node seeing this
	// network for the first time; undated and unparseable times count as old.
	fresh, err := recordV3NetAreas(path, "felonynet", n, nil)
	if err != nil || len(fresh) != 1 || fresh[0].Tag != "fel.recent" {
		t.Fatalf("first sighting: fresh=%v err=%v, want only fel.recent", fresh, err)
	}
	if fresh, _ := recordV3NetAreas(path, "felonynet", n, nil); len(fresh) != 0 {
		t.Errorf("recent area offered twice: %v", fresh)
	}

	// A failed offer leaves the network unseen, so the next NAL offers it again.
	other := nalWith("oth.recent")
	other.Areas[0].Added = now.Format(time.RFC3339)
	fail := func([]protocol.Area) error { return errors.New("no") }
	if _, err := recordV3NetAreas(path, "othernet", other, fail); err == nil {
		t.Fatal("offer error was swallowed")
	}
	if fresh, _ := recordV3NetAreas(path, "othernet", other, nil); len(fresh) != 1 {
		t.Errorf("retry after failed offer: fresh=%v, want oth.recent", fresh)
	}
}

func TestNoteV3NetNALFirstNALSkipsCarriedRecentArea(t *testing.T) {
	e, um := newAreaFixture(t)
	n := nalWith("fel.a", "fel.b")
	stamp := time.Now().UTC().Format(time.RFC3339)
	n.Areas[0].Added = stamp // already carried by the fixture
	n.Areas[1].Added = stamp
	if got := e.NoteV3NetNAL(um, "felonynet", n, "ME"); got != 1 {
		t.Fatalf("queued %d notices, want 1 (fel.b to the sysop)", got)
	}
	q, _ := peekSysopNotices(sysopNoticesPath(e.GetServerConfig().DataDir), 1)
	if len(q) != 1 || q[0].V3NetTag != "fel.b" {
		t.Errorf("sysop queue = %+v, want fel.b", q)
	}
}

// newAreaFixture returns an executor whose data and config live in temp dirs,
// a v3net.json already carrying fel.a on felonynet, and a user manager with a
// sysop (ID 1), a co-sysop (ID 2) and a regular user (ID 3).
func newAreaFixture(t *testing.T) (*MenuExecutor, *user.UserMgr) {
	t.Helper()
	root := t.TempDir()
	e := &MenuExecutor{RootConfigPath: root}
	e.SetServerConfig(config.ServerConfig{SysOpLevel: 255, CoSysOpLevel: 250, DataDir: filepath.Join(root, "data")})
	e.SetStrings(config.StringsConfig{V3NetNewAreaNotice: "New %s area: %s. Add?"})
	if err := config.SaveV3NetConfig(root, config.V3NetConfig{Leaves: []config.V3NetLeafConfig{
		{Network: "felonynet", HubURL: "https://hub.example", Boards: []string{"fel.a"}},
	}}); err != nil {
		t.Fatal(err)
	}
	um := user.NewUserMgrForTest(
		&user.User{ID: 1, Handle: "Sysop", AccessLevel: 255},
		&user.User{ID: 2, Handle: "CoSysop", AccessLevel: 250},
		&user.User{ID: 3, Handle: "Caller", AccessLevel: 10},
	)
	return e, um
}

func TestNoteV3NetNALQueuesOffersForSysops(t *testing.T) {
	e, um := newAreaFixture(t)
	if n := e.NoteV3NetNAL(um, "felonynet", nalWith("fel.a", "fel.b"), "ME"); n != 0 {
		t.Fatalf("first NAL queued %d notices, want 0", n)
	}

	next := nalWith("fel.a", "fel.b", "fel.c", "fel.d", "fel.e")
	next.Areas[3].Access.Mode = protocol.AccessModeClosed // fel.d: closed to us
	next.Areas[4].Access.Mode = protocol.AccessModeClosed // fel.e: closed, but we are allow-listed
	next.Areas[4].Access.AllowList = []string{"ME"}
	if n := e.NoteV3NetNAL(um, "felonynet", next, "ME"); n != 2 {
		t.Fatalf("queued %d notices, want 2 (fel.c and fel.e, sysop only)", n)
	}

	path := sysopNoticesPath(e.GetServerConfig().DataDir)
	got, _ := peekSysopNotices(path, 1)
	if len(got) != 2 || got[0].V3NetTag != "fel.c" || got[1].V3NetTag != "fel.e" {
		t.Fatalf("sysop queue = %+v, want fel.c then fel.e", got)
	}
	if got[0].Text != "New Felonynet area: Area fel.c. Add?" || got[0].V3NetNetwork != "felonynet" {
		t.Errorf("notice = %+v", got[0])
	}
	for _, id := range []int{2, 3} {
		if q, _ := peekSysopNotices(path, id); len(q) != 0 {
			t.Errorf("user %d was offered areas: %+v", id, q)
		}
	}
}

func TestNoteV3NetNALSkipsCarriedAreasAndMissingText(t *testing.T) {
	e, um := newAreaFixture(t)
	e.NoteV3NetNAL(um, "felonynet", nalWith("fel.b"), "ME")
	// fel.a is new to the seen set but already in v3net.json.
	if n := e.NoteV3NetNAL(um, "felonynet", nalWith("fel.a", "fel.b"), "ME"); n != 0 {
		t.Errorf("offered an area the BBS already carries (%d notices)", n)
	}

	e.SetStrings(config.StringsConfig{})
	if n := e.NoteV3NetNAL(um, "felonynet", nalWith("fel.a", "fel.b", "fel.z"), "ME"); n != 0 {
		t.Errorf("blank string still queued %d notices", n)
	}

	// fel.z was not offered, so it must not have been marked seen: once the
	// string has text again, the next NAL offers it.
	e.SetStrings(config.StringsConfig{V3NetNewAreaNotice: "New %s area: %s. Add?"})
	if n := e.NoteV3NetNAL(um, "felonynet", nalWith("fel.a", "fel.b", "fel.z"), "ME"); n != 1 {
		t.Errorf("area not offered after the failed attempt (%d notices), want 1", n)
	}
}

// TestNoteV3NetNALRetryDoesNotDuplicate covers a retry after a partial queue:
// a sysop who already holds an offer for an area is not given a second one.
func TestNoteV3NetNALRetryDoesNotDuplicate(t *testing.T) {
	e, um := newAreaFixture(t)
	e.NoteV3NetNAL(um, "felonynet", nalWith("fel.a"), "ME")
	queueOffer(t, e, "fel.b", "Area fel.b") // as if an earlier attempt got this far

	if n := e.NoteV3NetNAL(um, "felonynet", nalWith("fel.a", "fel.b"), "ME"); n != 0 {
		t.Errorf("queued %d notices, want 0 (sysop already has the offer)", n)
	}
	got, _ := peekSysopNotices(sysopNoticesPath(e.GetServerConfig().DataDir), 1)
	if len(got) != 1 {
		t.Errorf("sysop queue = %+v, want the one existing offer", got)
	}
}

// runNoticeScreen runs SYSOPNOTICES for the sysop with the given keystrokes.
func runNoticeScreen(t *testing.T, e *MenuExecutor, level int, input string) string {
	t.Helper()
	ts := newTestSession(input)
	c := &cmdCtx{
		e: e, s: ts, terminal: newTestTerminal(ts),
		currentUser: &user.User{ID: 1, Handle: "Sysop", AccessLevel: level},
		outputMode:  ansi.OutputModeUTF8, termWidth: 80, termHeight: 24,
	}
	if _, _, err := runSysopNotices(c, ""); err != nil && !errors.Is(err, errInputAborted) {
		t.Fatalf("runSysopNotices: %v", err)
	}
	return ts.out.String()
}

func queueOffer(t *testing.T, e *MenuExecutor, tag, name string) {
	t.Helper()
	n := sysopNotice{Text: "fallback " + tag, V3NetNetwork: "felonynet", V3NetTag: tag, V3NetName: name}
	if err := enqueueSysopNotice(sysopNoticesPath(e.GetServerConfig().DataDir), 1, n); err != nil {
		t.Fatal(err)
	}
}

func TestSysopNoticesAddsAcceptedAreas(t *testing.T) {
	e, _ := newAreaFixture(t)
	mm, err := message.NewMessageManager(t.TempDir(), t.TempDir(), "TestBBS", nil)
	if err != nil {
		t.Fatal(err)
	}
	e.MessageMgr = mm
	reloads := 0
	e.V3NetReload = func() error { reloads++; return nil }
	e.V3NetStatus = &fakeV3NetStatus{network: "felonynet", hubURL: "https://hub.example", nal: nalWith("fel.a", "fel.music", "fel.warez")}

	path := sysopNoticesPath(e.GetServerConfig().DataDir)
	if err := enqueueSysopNotice(path, 1, sysopNotice{Text: "|12New user|07: Bob"}); err != nil {
		t.Fatal(err)
	}
	queueOffer(t, e, "fel.music", "Music")
	queueOffer(t, e, "fel.warez", "Warez")
	queueOffer(t, e, "fel.gone", "Gone") // dropped by the hub since

	// Y adds Music, N declines Warez, Enter dismisses the pause.
	out := runNoticeScreen(t, e, 255, "yn\r")

	if !strings.Contains(out, "New user") {
		t.Errorf("plain notice not shown: %q", out)
	}
	// The question uses the area's current name from the NAL.
	if !strings.Contains(out, "New Felonynet area: Area fel.music. Add?") || !strings.Contains(out, "New Felonynet area: Area fel.warez. Add?") {
		t.Errorf("offers not asked: %q", out)
	}
	if strings.Contains(out, "Gone") {
		t.Errorf("offered an area the hub has dropped: %q", out)
	}
	if !strings.Contains(out, "Added fel.music. Now active.") || reloads != 1 {
		t.Errorf("expected one live reload after adding fel.music (reloads=%d): %q", reloads, out)
	}

	boards := v3netSubscribedBoards(e.RootConfigPath, "felonynet")
	if !boards["fel.music"] || boards["fel.warez"] {
		t.Errorf("v3net.json boards = %v, want fel.music added and fel.warez not", boards)
	}
	if _, ok := mm.GetAreaByTag("fel.music"); !ok {
		t.Error("no local message area was created for fel.music")
	}
	if q, _ := peekSysopNotices(path, 1); len(q) != 0 {
		t.Errorf("queue not emptied: %+v", q)
	}
}

func TestSysopNoticesKeepsOffersWhenInterrupted(t *testing.T) {
	e, _ := newAreaFixture(t)
	e.V3NetStatus = &fakeV3NetStatus{network: "felonynet", hubURL: "https://hub.example", nal: nalWith("fel.a", "fel.music", "fel.warez")}
	queueOffer(t, e, "fel.music", "Music")
	queueOffer(t, e, "fel.warez", "Warez")

	// N settles Music, then the input ends at the Warez prompt.
	runNoticeScreen(t, e, 255, "n")

	q, _ := peekSysopNotices(sysopNoticesPath(e.GetServerConfig().DataDir), 1)
	if len(q) != 1 || q[0].V3NetTag != "fel.warez" {
		t.Errorf("queue = %+v, want only the unanswered fel.warez", q)
	}
}

func TestSysopNoticesDropsOffersForNonSysops(t *testing.T) {
	e, _ := newAreaFixture(t)
	e.V3NetStatus = &fakeV3NetStatus{network: "felonynet", hubURL: "https://hub.example", nal: nalWith("fel.a", "fel.music")}
	queueOffer(t, e, "fel.music", "Music")

	// A co-sysop now: the offer is dropped without asking.
	out := runNoticeScreen(t, e, 250, "")
	if strings.Contains(out, "Add?") {
		t.Errorf("co-sysop was asked: %q", out)
	}
	if q, _ := peekSysopNotices(sysopNoticesPath(e.GetServerConfig().DataDir), 1); len(q) != 0 {
		t.Errorf("queue = %+v, want the offer dropped", q)
	}
}

func TestRemoveSysopNoticesKeepsNewerEntries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sysop_notices.json")
	a := sysopNotice{Text: "a"}
	if err := enqueueSysopNotice(path, 1, a); err != nil {
		t.Fatal(err)
	}
	read, _ := peekSysopNotices(path, 1)
	// Something is queued after the caller read the queue.
	if err := enqueueSysopNotice(path, 1, sysopNotice{Text: "b"}); err != nil {
		t.Fatal(err)
	}
	if err := removeSysopNotices(path, 1, read); err != nil {
		t.Fatal(err)
	}
	if q, _ := peekSysopNotices(path, 1); len(q) != 1 || q[0].Text != "b" {
		t.Errorf("queue = %+v, want only b", q)
	}
}

// TestSysopNoticesDeclineShowsReminder covers No: the offer is settled for
// good, and the sysop is told so and how to add the area by hand later.
func TestSysopNoticesDeclineShowsReminder(t *testing.T) {
	e, _ := newAreaFixture(t)
	e.SetStrings(config.StringsConfig{
		V3NetNewAreaNotice:   "New %s area: %s. Add?",
		V3NetNewAreaDeclined: "Not asked about %s again. Add it later from Area Subscriptions.",
	})
	e.V3NetStatus = &fakeV3NetStatus{network: "felonynet", hubURL: "https://hub.example", nal: nalWith("fel.a", "fel.music")}
	queueOffer(t, e, "fel.music", "Music")

	out := runNoticeScreen(t, e, 255, "n\r")

	if !strings.Contains(out, "Not asked about fel.music again. Add it later from Area Subscriptions.") {
		t.Errorf("decline reminder missing: %q", out)
	}
	if q, _ := peekSysopNotices(sysopNoticesPath(e.GetServerConfig().DataDir), 1); len(q) != 0 {
		t.Errorf("declined offer still queued: %+v", q)
	}
	if v3netSubscribedBoards(e.RootConfigPath, "felonynet")["fel.music"] {
		t.Error("declined area was subscribed")
	}
}

// TestSysopNoticesKeepsOffersWhenHubUnavailable covers a network whose leaf
// is gone or whose hub cannot be reached: answering Yes would write a leaf
// with no hub URL or an area the hub has since closed, so nothing is asked
// and the offer waits for the next login.
func TestSysopNoticesKeepsOffersWhenHubUnavailable(t *testing.T) {
	for name, svc := range map[string]*fakeV3NetStatus{
		"no leaf":     {network: "felonynet", nal: nalWith("fel.a", "fel.music")},
		"NAL failing": {network: "felonynet", hubURL: "https://hub.example"},
	} {
		t.Run(name, func(t *testing.T) {
			e, _ := newAreaFixture(t)
			e.V3NetStatus = svc
			queueOffer(t, e, "fel.music", "Music")

			out := runNoticeScreen(t, e, 255, "y\r")
			if strings.Contains(out, "Add?") {
				t.Errorf("asked without a reachable hub: %q", out)
			}
			if q, _ := peekSysopNotices(sysopNoticesPath(e.GetServerConfig().DataDir), 1); len(q) != 1 {
				t.Errorf("queue = %+v, want the offer kept", q)
			}
			if v3netSubscribedBoards(e.RootConfigPath, "felonynet")["fel.music"] {
				t.Error("area was subscribed")
			}
		})
	}
}

// TestSysopNoticesSettlesAreaClosedSinceOffered covers an area the hub has
// closed to this node after the offer was queued.
func TestSysopNoticesSettlesAreaClosedSinceOffered(t *testing.T) {
	e, _ := newAreaFixture(t)
	current := nalWith("fel.a", "fel.music")
	current.Areas[1].Access.Mode = protocol.AccessModeClosed
	e.V3NetStatus = &fakeV3NetStatus{network: "felonynet", hubURL: "https://hub.example", nal: current}
	queueOffer(t, e, "fel.music", "Music")

	out := runNoticeScreen(t, e, 255, "y\r")
	if strings.Contains(out, "Add?") {
		t.Errorf("asked about an area closed to this node: %q", out)
	}
	if q, _ := peekSysopNotices(sysopNoticesPath(e.GetServerConfig().DataDir), 1); len(q) != 0 {
		t.Errorf("queue = %+v, want the offer settled", q)
	}
}

// TestNoteV3NetNALRetrySkipsDeclined covers a retry after a partial queue: a
// sysop who already said No to an area is not asked again.
func TestNoteV3NetNALRetrySkipsDeclined(t *testing.T) {
	e, um := newAreaFixture(t)
	e.V3NetStatus = &fakeV3NetStatus{network: "felonynet", hubURL: "https://hub.example", nal: nalWith("fel.a", "fel.b")}
	e.NoteV3NetNAL(um, "felonynet", nalWith("fel.a"), "ME")
	queueOffer(t, e, "fel.b", "Area fel.b")
	runNoticeScreen(t, e, 255, "n\r")

	// fel.b was never recorded as seen (as after a failed queue), so the next
	// NAL treats it as new again.
	if n := e.NoteV3NetNAL(um, "felonynet", nalWith("fel.a", "fel.b"), "ME"); n != 0 {
		t.Errorf("queued %d notices, want 0 (sysop declined fel.b)", n)
	}
}

// TestSysopNoticesSkipsOfferAnotherSessionIsAsking covers two sysops logged
// in together: the one who gets to an offer second is not asked, and the
// offer waits for their next login.
func TestSysopNoticesSkipsOfferAnotherSessionIsAsking(t *testing.T) {
	e, _ := newAreaFixture(t)
	e.V3NetStatus = &fakeV3NetStatus{network: "felonynet", hubURL: "https://hub.example", nal: nalWith("fel.a", "fel.music")}
	queueOffer(t, e, "fel.music", "Music")

	release, ok := claimV3NetOffer("felonynet", "fel.music")
	if !ok {
		t.Fatal("could not claim the offer")
	}
	defer release()

	out := runNoticeScreen(t, e, 255, "y\r")
	if strings.Contains(out, "Add?") {
		t.Errorf("asked about an offer another session holds: %q", out)
	}
	if q, _ := peekSysopNotices(sysopNoticesPath(e.GetServerConfig().DataDir), 1); len(q) != 1 {
		t.Errorf("queue = %+v, want the offer kept", q)
	}
}
