package menu

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/conference"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor/testterm"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/session"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
	"golang.org/x/term"
)

// alertNet is activityNet with every alert kind on and the hub relaying
// them, and network chat offered.
func alertNet(name string) CentralNetworkInfo {
	n := activityNet(name)
	n.Chat = CentralChatInfo{Enabled: true, Supported: true}
	n.Alerts = CentralAlertInfo{Supported: true, Enabled: map[string]bool{}}
	for _, k := range protocol.CentralAlertKinds {
		n.Alerts.Enabled[k] = true
	}
	return n
}

// alertStrings are short alert formats a test can match.
func alertStrings() config.StringsConfig {
	return config.StringsConfig{
		CentralAlertLogon:    "on %s@%s %s",
		CentralAlertLogoff:   "off %s@%s %s",
		CentralAlertChatJoin: "chat %s@%s %s %s",
		CentralAlertPost:     "post %s@%s %s %s",
	}
}

// alertFixture is a board with a user manager, a session registry, a
// V3Net area "fel.general" bound to testnet that needs s20 to read, and
// a provider offering every alert kind on testnet.
type alertFixture struct {
	e    *MenuExecutor
	um   *user.UserMgr
	fake *centralFake
	area *message.MessageArea
}

func newAlertFixture(t *testing.T) *alertFixture {
	t.Helper()
	root := t.TempDir()
	cfgDir, dataDir := filepath.Join(root, "configs"), filepath.Join(root, "data")
	for _, d := range []string{cfgDir, dataDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	areas, _ := json.Marshal([]message.MessageArea{
		{ID: 1, Position: 1, Tag: "fel.general", Name: "FelonyNet General", ACSRead: "s20", ACSWrite: "s20", BasePath: "msgbases/fel_general", AreaType: "v3net"},
		{ID: 2, Position: 2, Tag: "fel.local", Name: "Not networked", ACSRead: "s10", ACSWrite: "s10", BasePath: "msgbases/fel_local", AreaType: "local"},
		// Open to anyone with a terminal, but in a conference for s50.
		{ID: 3, Position: 3, Tag: "fel.elite", Name: "Elite", ACSRead: "s10|A", ACSWrite: "s10", ConferenceID: 2, BasePath: "msgbases/fel_elite", AreaType: "v3net"},
	})
	confs := `[{"id": 1, "position": 1, "tag": "LOCAL", "name": "Local", "acs": ""},
		{"id": 2, "position": 2, "tag": "ELITE", "name": "Elite", "acs": "s50"}]`
	if err := os.WriteFile(filepath.Join(cfgDir, "conferences.json"), []byte(confs), 0o644); err != nil {
		t.Fatal(err)
	}
	cm, err := conference.NewConferenceManager(cfgDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "message_areas.json"), areas, 0o644); err != nil {
		t.Fatal(err)
	}
	mm, err := message.NewMessageManager(dataDir, cfgDir, "TestBBS", nil)
	if err != nil {
		t.Fatal(err)
	}
	area, ok := mm.GetAreaByTag("fel.general")
	if !ok {
		t.Fatal("area missing")
	}
	um, err := user.NewUserManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fake := &centralFake{nets: []CentralNetworkInfo{alertNet("testnet")}, areaNets: map[int]string{area.ID: "testnet", 3: "testnet"}}
	e := &MenuExecutor{SessionRegistry: session.NewSessionRegistry(), MessageMgr: mm, ConferenceMgr: cm, V3NetCentral: fake, RootConfigPath: t.TempDir()}
	e.SetStrings(alertStrings())
	return &alertFixture{e: e, um: um, fake: fake, area: area}
}

// caller adds an account at level with prefs and a session for it on
// node, past login unless ready is false.
func (f *alertFixture) caller(t *testing.T, node int, handle string, level int, prefs user.CentralAlertPrefs, ready bool) (*session.BbsSession, *user.User) {
	t.Helper()
	u, err := f.um.AddUser("password", handle, handle+" Real", "Loc")
	if err != nil {
		t.Fatal(err)
	}
	u.AccessLevel = level
	u.Validated = true
	if err := f.um.UpdateUser(u); err != nil {
		t.Fatal(err)
	}
	if _, err := f.um.SetCentralConsent(u.ID, func(x *user.User) { x.CentralAlerts = prefs }); err != nil {
		t.Fatal(err)
	}
	stored, _ := f.um.GetUserByID(u.ID)
	// The connection is a test session: a local call with no terminal.
	s := &session.BbsSession{ID: node, NodeID: node, User: stored, StartTime: time.Now(), Channel: newTestSession("")}
	s.SetToastsReady(ready)
	f.e.SessionRegistry.Register(s)
	return s, stored
}

// incoming returns an alert of kind from Alice on board Beta.
func incoming(kind string) protocol.CentralAlert {
	a := protocol.CentralAlert{
		Kind: kind, ID: "abcdefghijklmnopqrstuv", Network: "testnet", NodeID: "beta00000000node",
		BBS: "Beta", Handle: "Alice", ExpiresAt: time.Now().Add(2 * time.Minute).UTC().Format(time.RFC3339),
	}
	switch kind {
	case protocol.CentralAlertChatJoin:
		a.Room = "lobby"
	case protocol.CentralAlertPost:
		a.Area = "fel.general"
	}
	return a
}

// An alert reaches exactly the callers who asked to see its kind and are
// past login; sharing a kind subscribes to nothing, and each kind is its
// own switch. The line names the caller, their board, and the room or the
// area as this board names it.
func TestDeliverCentralAlertToSubscribers(t *testing.T) {
	f := newAlertFixture(t)
	seesAll := user.CentralAlertPrefs{SeeLogons: true, SeeLogoffs: true, SeeChat: true, SeePosts: true}
	watcher, _ := f.caller(t, 1, "Watcher", 30, seesAll, true)
	sharer, _ := f.caller(t, 2, "Sharer", 30, user.CentralAlertPrefs{ShareLogons: true, ShareLogoffs: true, ShareChat: true, SharePosts: true}, true)
	logonsOnly, _ := f.caller(t, 3, "Logons", 30, user.CentralAlertPrefs{SeeLogons: true}, true)
	loggingIn, _ := f.caller(t, 4, "Arriving", 30, seesAll, false)

	want := map[string]string{
		protocol.CentralAlertLogon:    "on Alice@Beta testnet",
		protocol.CentralAlertLogoff:   "off Alice@Beta testnet",
		protocol.CentralAlertChatJoin: "chat Alice@Beta lobby testnet",
		protocol.CentralAlertPost:     "post Alice@Beta FelonyNet General testnet",
	}
	for _, kind := range protocol.CentralAlertKinds {
		f.e.DeliverCentralAlert(incoming(kind), f.um)
		if got := watcher.DrainPages(); len(got) != 1 || got[0] != want[kind] {
			t.Errorf("%s: watcher got %q, want %q", kind, got, want[kind])
		}
		if got := sharer.DrainPages(); len(got) != 0 {
			t.Errorf("%s: a caller who only shares got %q", kind, got)
		}
		got := logonsOnly.DrainPages()
		if (kind == protocol.CentralAlertLogon) != (len(got) == 1) {
			t.Errorf("%s: logons-only caller got %q", kind, got)
		}
		if got := loggingIn.DrainPages(); len(got) != 0 {
			t.Errorf("%s: a caller still logging in got %q", kind, got)
		}
	}
}

// A See switch turned off on another node stops the next alert: delivery
// reads the stored record, not the session's copy.
func TestDeliverCentralAlertReadsStoredChoice(t *testing.T) {
	f := newAlertFixture(t)
	s, u := f.caller(t, 1, "Watcher", 30, user.CentralAlertPrefs{SeeLogons: true}, true)
	if _, err := f.um.SetCentralConsent(u.ID, func(x *user.User) { x.CentralAlerts.SeeLogons = false }); err != nil {
		t.Fatal(err)
	}
	f.e.DeliverCentralAlert(incoming(protocol.CentralAlertLogon), f.um)
	if got := s.DrainPages(); len(got) != 0 {
		t.Errorf("withdrawn subscription still delivered %q", got)
	}
}

// Only callers allowed to see what an alert is about get it: a post only
// to callers who may read the area here, and not at all where this board
// does not carry the area on that network; a chat join not to a caller the
// SysOp barred from network chat; nothing from a caller on the
// recipient's Twit List; nothing expired.
func TestDeliverCentralAlertRespectsAccess(t *testing.T) {
	f := newAlertFixture(t)
	f.e.SetServerConfig(config.ServerConfig{TwitListMax: 10})
	prefs := user.CentralAlertPrefs{SeeLogons: true, SeeChat: true, SeePosts: true}
	reader, _ := f.caller(t, 1, "Reader", 30, prefs, true)
	lowLevel, _ := f.caller(t, 2, "Newbie", 10, prefs, true)
	barred, barredUser := f.caller(t, 3, "Barred", 30, prefs, true)
	if _, err := f.um.SetCentralConsent(barredUser.ID, func(x *user.User) { x.CentralChatBarred = true }); err != nil {
		t.Fatal(err)
	}
	twit, twitUser := f.caller(t, 4, "Twit", 30, prefs, true)
	twitUser.TwitList = []user.TwitEntry{{Name: "Alice", Addr: "beta00000000node"}}
	if err := f.um.UpdateUser(twitUser); err != nil {
		t.Fatal(err)
	}

	f.e.DeliverCentralAlert(incoming(protocol.CentralAlertPost), f.um)
	if got := reader.DrainPages(); len(got) != 1 {
		t.Errorf("reader got %q", got)
	}
	if got := lowLevel.DrainPages(); len(got) != 0 {
		t.Errorf("a caller who cannot read the area got %q", got)
	}
	barred.DrainPages() // may read the area: only chat is barred
	twit.DrainPages()

	other := incoming(protocol.CentralAlertPost)
	other.Network = "othernet" // this board carries fel.general on testnet only
	f.e.DeliverCentralAlert(other, f.um)
	other = incoming(protocol.CentralAlertPost)
	other.Area = "fel.unknown"
	f.e.DeliverCentralAlert(other, f.um)
	if got := reader.DrainPages(); len(got) != 0 {
		t.Errorf("an area this board does not carry on that network was announced: %q", got)
	}

	f.e.DeliverCentralAlert(incoming(protocol.CentralAlertChatJoin), f.um)
	if got := barred.DrainPages(); len(got) != 0 {
		t.Errorf("a caller barred from network chat got %q", got)
	}
	if got := reader.DrainPages(); len(got) != 1 {
		t.Errorf("reader got %q for the chat join", got)
	}

	f.e.DeliverCentralAlert(incoming(protocol.CentralAlertLogon), f.um)
	if got := twit.DrainPages(); len(got) != 0 {
		t.Errorf("a Twit-listed caller's alert was shown: %q", got)
	}
	reader.DrainPages()

	stale := incoming(protocol.CentralAlertLogon)
	stale.ExpiresAt = time.Now().Add(-time.Second).UTC().Format(time.RFC3339)
	f.e.DeliverCentralAlert(stale, f.um)
	if got := reader.DrainPages(); len(got) != 0 {
		t.Errorf("an expired alert was shown: %q", got)
	}
}

// A post alert follows the area's read ACS as evaluated on the viewer's
// own connection, so a term that needs one (A here) is read rather than
// failing the whole string, and the area's conference ACS as well.
func TestDeliverCentralAlertAreaAndConferenceACS(t *testing.T) {
	f := newAlertFixture(t)
	prefs := user.CentralAlertPrefs{SeePosts: true}
	member, _ := f.caller(t, 1, "Member", 60, prefs, true)
	outsider, _ := f.caller(t, 2, "Outsider", 30, prefs, true)

	// fel.general needs s20, an alternative-free string: both may read it.
	f.e.DeliverCentralAlert(incoming(protocol.CentralAlertPost), f.um)
	for name, s := range map[string]*session.BbsSession{"member": member, "outsider": outsider} {
		if got := s.DrainPages(); len(got) != 1 {
			t.Errorf("%s got %q for fel.general", name, got)
		}
	}

	// fel.elite is s10|A, in a conference for s50: only the member may.
	elite := incoming(protocol.CentralAlertPost)
	elite.Area = "fel.elite"
	f.e.DeliverCentralAlert(elite, f.um)
	if got := member.DrainPages(); len(got) != 1 || !strings.Contains(got[0], "Elite") {
		t.Errorf("member got %q for fel.elite", got)
	}
	if got := outsider.DrainPages(); len(got) != 0 {
		t.Errorf("a caller outside the area's conference got %q", got)
	}
}

// A sysop who blanks a kind's string shows no alert of that kind.
func TestDeliverCentralAlertBlankString(t *testing.T) {
	f := newAlertFixture(t)
	strs := alertStrings()
	strs.CentralAlertLogon = ""
	f.e.SetStrings(strs)
	s, _ := f.caller(t, 1, "Watcher", 30, user.CentralAlertPrefs{SeeLogons: true}, true)
	f.e.DeliverCentralAlert(incoming(protocol.CentralAlertLogon), f.um)
	if got := s.DrainPages(); len(got) != 0 {
		t.Errorf("blank string still delivered %q", got)
	}
}

// A caller's action is announced only when their stored record shares
// that kind; never from an invisible session; and a post only in an area
// bound to a network, never private or anonymous.
func TestSendCentralAlertNeedsConsent(t *testing.T) {
	f := newAlertFixture(t)
	sess, u := f.caller(t, 1, "Sharer", 30, user.CentralAlertPrefs{ShareLogons: true, SharePosts: true}, true)

	f.e.SendCentralLogonAlert(f.um, sess, u)
	f.e.SendCentralLogoffAlert(f.um, sess, u) // not shared
	if len(f.fake.alerts) != 1 || f.fake.alerts[0] != (sentAlert{"", protocol.CentralAlertLogon, "Sharer", "", ""}) {
		t.Fatalf("sent %+v", f.fake.alerts)
	}

	f.fake.alerts = nil
	f.e.sendPostAlert(f.um, 1, u, f.area.ID, f.area.Tag, false, false)
	f.e.sendPostAlert(f.um, 1, u, f.area.ID, f.area.Tag, true, false) // private
	f.e.sendPostAlert(f.um, 1, u, f.area.ID, f.area.Tag, false, true) // anonymous
	f.e.sendPostAlert(f.um, 1, u, 2, "fel.local", false, false)       // not networked
	if len(f.fake.alerts) != 1 || f.fake.alerts[0] != (sentAlert{"testnet", protocol.CentralAlertPost, "Sharer", "", "fel.general"}) {
		t.Fatalf("post alerts %+v", f.fake.alerts)
	}

	// Withdrawn on another node: the session's copy still says yes.
	if _, err := f.um.SetCentralConsent(u.ID, func(x *user.User) { x.CentralAlerts.ShareLogons = false }); err != nil {
		t.Fatal(err)
	}
	f.fake.alerts = nil
	f.e.SendCentralLogonAlert(f.um, sess, u)
	if len(f.fake.alerts) != 0 {
		t.Errorf("withdrawn consent still sent %+v", f.fake.alerts)
	}

	if _, err := f.um.SetCentralConsent(u.ID, func(x *user.User) { x.CentralAlerts.ShareLogons = true }); err != nil {
		t.Fatal(err)
	}
	sess.Invisible = true
	f.e.SendCentralLogonAlert(f.um, sess, u)
	if len(f.fake.alerts) != 0 {
		t.Errorf("invisible session announced %+v", f.fake.alerts)
	}
}

// A chat join is announced with the room as network chat names it, and
// not for local chat.
func TestSendChatJoinAlert(t *testing.T) {
	f := newAlertFixture(t)
	_, u := f.caller(t, 1, "Chatter", 30, user.CentralAlertPrefs{ShareChat: true}, true)
	c := &cmdCtx{e: f.e, userManager: f.um, currentUser: u, nodeNumber: 1}
	f.e.sendChatJoinAlert(c, "testnet", "Late Night")
	f.e.sendChatJoinAlert(c, "", "lobby")
	if len(f.fake.alerts) != 1 || f.fake.alerts[0] != (sentAlert{"testnet", protocol.CentralAlertChatJoin, "Chatter", "late-night", ""}) {
		t.Errorf("sent %+v", f.fake.alerts)
	}
}

// runAlertScreen runs My Preferences on an 80x24 terminal for keys.
func runAlertScreen(t *testing.T, f *alertFixture, u *user.User, keys string) *testterm.Term {
	t.Helper()
	screen := testterm.New(80, 24)
	sess := testterm.NewSession(screen, keys)
	t.Cleanup(func() {
		resetSessionIH(sess)
		sessionTermSizes.Delete(sess)
	})
	c := &cmdCtx{e: f.e, s: sess, terminal: term.NewTerminal(sess, ""), userManager: f.um, currentUser: u,
		nodeNumber: 1, outputMode: ansi.OutputModeUTF8, termWidth: 80, termHeight: 24}
	if _, _, err := runV3NetCentralPrefs(c, ""); err != nil {
		t.Fatal(err)
	}
	return screen
}

// My Preferences opens Activity Alerts; each switch there saves on the
// stored record, Share and See apart, and the screens fit 80x24 with
// every feature offered.
func TestCentralAlertsScreen(t *testing.T) {
	f := newAlertFixture(t)
	_, u := f.caller(t, 1, "Caller", 30, user.CentralAlertPrefs{}, true)

	prefs := runAlertScreen(t, f, u, "q\r").Snapshot()
	if !strings.Contains(prefs, "[5] Activity alerts") {
		t.Fatalf("no Activity alerts entry:\n%s", prefs)
	}
	if first := strings.SplitN(prefs, "\n", 2)[0]; !strings.Contains(first, "V3Net Central: Sharing") {
		t.Errorf("My Preferences scrolled off an 80x24 screen:\n%s", prefs)
	}

	// 1 shares logons; 8 sees posts; then back out of both screens.
	runAlertScreen(t, f, u, "5\r1\r8\rq\rq\r")
	stored, _ := f.um.GetUserByID(u.ID)
	if stored.CentralAlerts != (user.CentralAlertPrefs{ShareLogons: true, SeePosts: true}) {
		t.Errorf("stored %+v", stored.CentralAlerts)
	}

	// Draw the Alerts screen alone to check its fit.
	screen := testterm.New(80, 24)
	sess := testterm.NewSession(screen, "q\r")
	t.Cleanup(func() { resetSessionIH(sess); sessionTermSizes.Delete(sess) })
	c := &cmdCtx{e: f.e, s: sess, terminal: term.NewTerminal(sess, ""), userManager: f.um, currentUser: u,
		nodeNumber: 1, outputMode: ansi.OutputModeUTF8, termWidth: 80, termHeight: 24}
	if _, err := runV3NetCentralAlerts(c); err != nil {
		t.Fatal(err)
	}
	snap := screen.Snapshot()
	if first := strings.SplitN(snap, "\n", 2)[0]; !strings.Contains(first, "V3Net Central: Activity Alerts") {
		t.Errorf("Alerts screen scrolled off 80x24:\n%s", snap)
	}
	for _, want := range []string{"[1] I log on", "On", "[8] someone posts", "Caller", "Never a message"} {
		if !strings.Contains(snap, want) {
			t.Errorf("Alerts screen lacks %q:\n%s", want, snap)
		}
	}
}

// A switch for a kind no network offers cannot be turned on, but one left
// on can still be turned off; a board without alerts shows no entry.
func TestCentralAlertsScreenUnoffered(t *testing.T) {
	f := newAlertFixture(t)
	n := alertNet("testnet")
	n.Alerts.Enabled[protocol.CentralAlertPost] = false
	f.fake.nets = []CentralNetworkInfo{n}
	_, u := f.caller(t, 1, "Caller", 30, user.CentralAlertPrefs{ShareLogons: true}, true)

	runAlertScreen(t, f, u, "5\r4\r1\rq\rq\r")
	stored, _ := f.um.GetUserByID(u.ID)
	if stored.CentralAlerts != (user.CentralAlertPrefs{}) {
		t.Errorf("stored %+v: post sharing turned on, or logons not turned off", stored.CentralAlerts)
	}

	f.fake.nets = []CentralNetworkInfo{activityNet("testnet")}
	if snap := runAlertScreen(t, f, u, "q\r").Snapshot(); strings.Contains(snap, "Activity alerts") {
		t.Errorf("Activity alerts offered where no network has them:\n%s", snap)
	}
}

// Before the hub's feature list is read, alerts the sysop has on are
// offered, as the other Central features are; once the hub says it does
// not relay them they are not.
func TestCentralAlertsOfferedWhileSupportUnknown(t *testing.T) {
	n := alertNet("testnet")
	n.SupportKnown, n.Alerts.Supported = false, false
	fake := &centralFake{nets: []CentralNetworkInfo{n}}
	if !centralAlertsOffered(fake) {
		t.Error("alerts hidden while hub support is unknown")
	}
	fake.nets[0].SupportKnown = true
	if centralAlertsOffered(fake) {
		t.Error("alerts offered on a hub that does not relay them")
	}
}

// With alerts offered, My Preferences does not claim nothing is shared or
// that what the caller does never is: alone, it points at the alerts; with
// chat too, its closing line covers chat's joins and lines as well, not
// alerts alone.
func TestCentralPrefsAlertsPreview(t *testing.T) {
	f := newAlertFixture(t)
	n := CentralNetworkInfo{Network: "testnet", Enabled: true, SupportKnown: true, Supported: true}
	n.Alerts = alertNet("testnet").Alerts
	f.fake.nets = []CentralNetworkInfo{n}
	_, u := f.caller(t, 1, "Caller", 30, user.CentralAlertPrefs{}, true)
	const closing = "activity only as switched on"
	snap := runAlertScreen(t, f, u, "q\r").Snapshot()
	if strings.Contains(snap, "offers no feature") || strings.Contains(snap, "or what you are doing") {
		t.Errorf("alerts only: preview denies what alerts share:\n%s", snap)
	}
	if !strings.Contains(snap, "Only activity alerts you share") || !strings.Contains(snap, closing) {
		t.Errorf("alerts only: preview does not mention alerts:\n%s", snap)
	}

	f.fake.nets = []CentralNetworkInfo{alertNet("testnet")} // chat offered too
	snap = runAlertScreen(t, f, u, "q\r").Snapshot()
	if !strings.Contains(snap, "Chat   Caller @") || !strings.Contains(snap, closing) || strings.Contains(snap, "only in alerts") {
		t.Errorf("chat and alerts: closing line should cover every switch:\n%s", snap)
	}
	if first := strings.SplitN(snap, "\n", 2)[0]; !strings.Contains(first, "V3Net Central: Sharing") {
		t.Errorf("chat and alerts: screen scrolled off 80x24:\n%s", snap)
	}
}

// The Sharing screen draws the menu set's V3SHARE.ANS over the switches:
// the shipped art fits 80x24 with every feature offered and a status line
// showing. Art too tall to leave the body room gives way to the built-in
// title rather than scrolling the screen.
func TestCentralPrefsHeaderArt(t *testing.T) {
	f := newAlertFixture(t)
	f.fake.nets = []CentralNetworkInfo{alertNet("testnet")}
	_, u := f.caller(t, 1, "Caller", 30, user.CentralAlertPrefs{}, true)

	f.e.MenuSetPath = filepath.Join("..", "..", "menus", "v3")
	snap := runAlertScreen(t, f, u, "1\rq\r").Snapshot()
	lines := strings.Split(snap, "\n")
	if !strings.Contains(lines[1], "ViSiON/3 Network Sharing") || strings.Contains(snap, "V3Net Central: Sharing") {
		t.Errorf("shipped header art not drawn on top:\n%s", snap)
	}
	for _, want := range []string{"[5] Activity alerts", "Chat   Caller @", "Saved.", "Choice (1-5"} {
		if !strings.Contains(snap, want) {
			t.Errorf("screen lacks %q; scrolled or cut off 80x24:\n%s", want, snap)
		}
	}

	tall := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tall, "ansi"), 0o755); err != nil {
		t.Fatal(err)
	}
	art := strings.Repeat("|13TALL ART\r\n", 12)
	if err := os.WriteFile(filepath.Join(tall, "ansi", centralPrefsArt), []byte(art), 0o644); err != nil {
		t.Fatal(err)
	}
	f.e.MenuSetPath = tall
	snap = runAlertScreen(t, f, u, "q\r").Snapshot()
	if first := strings.SplitN(snap, "\n", 2)[0]; !strings.Contains(first, "V3Net Central: Sharing") || strings.Contains(snap, "TALL ART") {
		t.Errorf("too-tall art should give way to the built-in title:\n%s", snap)
	}

	// Art that fits is drawn, its pipe codes expanded.
	if err := os.WriteFile(filepath.Join(tall, "ansi", centralPrefsArt), []byte("|13MY BOARD|07\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	snap = runAlertScreen(t, f, u, "q\r").Snapshot()
	if first := strings.SplitN(snap, "\n", 2)[0]; !strings.Contains(first, "MY BOARD") || strings.Contains(snap, "|13") {
		t.Errorf("sysop art not drawn as the header:\n%s", snap)
	}
}

// The header art is measured in glyphs, not bytes: the stock art is three
// rows in either encoding, so it fits a room of three rows and not two. A
// known terminal with no room left takes no art; only one of unknown
// height takes it whatever its size.
func TestCentralPrefsArtFit(t *testing.T) {
	e := &MenuExecutor{MenuSetPath: filepath.Join("..", "..", "menus", "v3")}
	for _, mode := range []ansi.OutputMode{ansi.OutputModeUTF8, ansi.OutputModeCP437} {
		for _, tc := range []struct {
			room int
			want bool
		}{{3, true}, {2, false}, {0, false}, {-1, true}} {
			if _, got := e.centralPrefsArt(mode, tc.room); got != tc.want {
				t.Errorf("mode %v, room %d: art drawn = %v, want %v", mode, tc.room, got, tc.want)
			}
		}
	}

	// UTF-8 art is measured the same way.
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "ansi"), 0o755); err != nil {
		t.Fatal(err)
	}
	row := strings.Repeat("█", 70) + "\r\n"
	if err := os.WriteFile(filepath.Join(dir, "ansi", centralPrefsArt), []byte(row+row), 0o644); err != nil {
		t.Fatal(err)
	}
	e.MenuSetPath = dir
	if _, ok := e.centralPrefsArt(ansi.OutputModeUTF8, 2); !ok {
		t.Error("two rows of UTF-8 art measured as more than two")
	}
}
