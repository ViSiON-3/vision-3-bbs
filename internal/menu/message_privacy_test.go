package menu

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	configtemplates "github.com/ViSiON-3/vision-3-bbs/templates/configs"
)

// privacyFixture is a BBS on the shipped configs (so PRIVMAIL is the real
// shipped area: read ACS s25, auto-joined) whose PRIVMAIL holds a public
// note, a private message from Sysop to Bob, and a private message signed and
// addressed only by real names ("Sam Sysop" to "Bob Builder"). Only handles
// identify a user, so that last message belongs to no one. Carol is a third
// user who passes the area's ACS but is party to neither private message.
type privacyFixture struct {
	e                 *MenuExecutor
	um                *user.UserMgr
	privID            int
	sysop, bob, carol *user.User
}

func newPrivacyFixture(t *testing.T) *privacyFixture {
	t.Helper()
	root := t.TempDir()
	cfgDir, dataDir := filepath.Join(root, "configs"), filepath.Join(root, "data")
	for _, d := range []string{cfgDir, dataDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := fs.WalkDir(configtemplates.FS, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := configtemplates.FS.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(cfgDir, p), b, 0o644)
	}); err != nil {
		t.Fatal(err)
	}
	strs, err := config.LoadStrings(cfgDir)
	if err != nil {
		t.Fatal(err)
	}
	menuSet, err := filepath.Abs(filepath.Join("..", "..", "menus", "v3"))
	if err != nil {
		t.Fatal(err)
	}
	mm, err := message.NewMessageManager(dataDir, cfgDir, "TestBBS", nil)
	if err != nil {
		t.Fatal(err)
	}
	priv, ok := mm.GetAreaByTag("PRIVMAIL")
	if !ok {
		t.Fatal("shipped PRIVMAIL area missing")
	}

	users := []*user.User{
		{ID: 1, Handle: "Sysop", RealName: "Sam Sysop", AccessLevel: 255, Validated: true},
		{ID: 2, Handle: "Bob", RealName: "Bob Builder", AccessLevel: 30, Validated: true},
		{ID: 3, Handle: "Carol", RealName: "Carol Singer", AccessLevel: 30, Validated: true},
	}
	for _, u := range users {
		u.CurrentMsgConferenceID = priv.ConferenceID
		u.CurrentMessageAreaID = priv.ID
		u.CurrentMessageAreaTag = priv.Tag
		u.TaggedMessageAreaTags = []string{priv.Tag}
		u.MsgHdr = 2 // a set style skips the first-read header picker
	}
	seed, err := json.Marshal(users)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "users.json"), seed, 0o644); err != nil {
		t.Fatal(err)
	}
	um, err := user.NewUserManager(dataDir)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := mm.AddMessage(priv.ID, "Sysop", "All", "Public note", "PUBLIC-BODY", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := mm.AddPrivateMessage(priv.ID, "Sysop", "Bob", "Secret plans", "SECRET-BODY", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := mm.AddPrivateMessage(priv.ID, "Sam Sysop", "Bob Builder", "Real-name note", "REALNAME-BODY", ""); err != nil {
		t.Fatal(err)
	}

	e := &MenuExecutor{MenuSetPath: menuSet, RootConfigPath: cfgDir, MessageMgr: mm}
	e.SetStrings(strs)
	e.SetServerConfig(config.ServerConfig{SysOpLevel: 255, CoSysOpLevel: 250, DataDir: dataDir})
	f := &privacyFixture{e: e, um: um, privID: priv.ID}
	f.sysop, _ = um.GetUserByID(1)
	f.bob, _ = um.GetUserByID(2)
	f.carol, _ = um.GetUserByID(3)
	return f
}

// run drives fn as u with scripted keystrokes and returns the output with
// ANSI escapes stripped. Input running out reads as a disconnect.
func (f *privacyFixture) run(t *testing.T, fn RunnableFunc, u *user.User, input string) string {
	t.Helper()
	ts := newTestSession(input)
	t.Cleanup(func() { resetSessionIH(ts) })
	c := &cmdCtx{
		e: f.e, s: ts, terminal: newTestTerminal(ts), userManager: f.um,
		currentUser: u, nodeNumber: 1, sessionStartTime: time.Now(),
		outputMode: ansi.OutputModeUTF8, termWidth: 80, termHeight: 24,
	}
	done := make(chan error, 1)
	go func() {
		_, _, err := fn(c, "")
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, errInputAborted) {
			t.Fatalf("handler: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("handler still running after its input ran out")
	}
	return testAnsiEscape.ReplaceAllString(ts.output(), "")
}

// TestReadMsgsHidesOthersPrivateMail pins #465 for READMSGS: stepping through
// PRIVMAIL, a user who passes the area ACS still never sees a private message
// they didn't send or receive, while the public note stays readable.
func TestReadMsgsHidesOthersPrivateMail(t *testing.T) {
	f := newPrivacyFixture(t)
	out := f.run(t, runReadMsgs, f.carol, "NNNQ")
	if !strings.Contains(out, "PUBLIC-BODY") {
		t.Errorf("public note not shown to Carol:\n%s", out)
	}
	for _, leak := range []string{"SECRET-BODY", "Secret plans", "REALNAME-BODY", "Real-name note"} {
		if strings.Contains(out, leak) {
			t.Errorf("READMSGS showed Carol %q from someone else's private mail", leak)
		}
	}
}

// TestReadMsgsShowsPrivateMailToItsParties pins that the recipient and the
// sender, by handle, still read their own private mail, and that mail
// addressed only by real name is shown to neither (a real name is not an
// identity).
func TestReadMsgsShowsPrivateMailToItsParties(t *testing.T) {
	f := newPrivacyFixture(t)
	for _, tc := range []struct {
		name string
		u    *user.User
	}{{"recipient", f.bob}, {"sender", f.sysop}} {
		t.Run(tc.name, func(t *testing.T) {
			out := f.run(t, runReadMsgs, tc.u, "NNNQ")
			if !strings.Contains(out, "SECRET-BODY") {
				t.Errorf("%s did not see their private mail:\n%s", tc.name, out)
			}
			if strings.Contains(out, "REALNAME-BODY") {
				t.Errorf("%s saw mail addressed only by real name", tc.name)
			}
		})
	}
}

// TestListMsgsHidesOthersPrivateMail pins #465 for LISTMSGS: other users'
// private messages are left out of the list entirely.
func TestListMsgsHidesOthersPrivateMail(t *testing.T) {
	f := newPrivacyFixture(t)
	out := f.run(t, runListMsgs, f.carol, "")
	if !strings.Contains(out, "Public note") {
		t.Errorf("public note missing from Carol's list:\n%s", out)
	}
	for _, leak := range []string{"Secret plans", "Real-name note"} {
		if strings.Contains(out, leak) {
			t.Errorf("LISTMSGS listed %q to Carol", leak)
		}
	}
	if out := f.run(t, runListMsgs, f.bob, ""); !strings.Contains(out, "Secret plans") {
		t.Errorf("Bob's list is missing his own private mail:\n%s", out)
	}
}

// TestNewscanHidesOthersPrivateMail pins #465 for NEWSCAN over the tagged
// PRIVMAIL area: the scan reads the public note but never another user's
// private message.
func TestNewscanHidesOthersPrivateMail(t *testing.T) {
	scanNoticePause = 0
	t.Cleanup(func() { scanNoticePause = time.Second })
	f := newPrivacyFixture(t)
	out := f.run(t, runNewscan, f.carol, "\rRNNNQ")
	if !strings.Contains(out, "PUBLIC-BODY") {
		t.Fatalf("newscan did not reach the public note:\n%s", out)
	}
	for _, leak := range []string{"SECRET-BODY", "REALNAME-BODY"} {
		if strings.Contains(out, leak) {
			t.Errorf("NEWSCAN showed Carol %q", leak)
		}
	}
}

// TestAdoptingARealNameGrantsNoMail pins the review finding on #467: real
// names are neither unique nor fixed, so a user who sets their real name to
// someone else's must not gain access to mail addressed to that name.
func TestAdoptingARealNameGrantsNoMail(t *testing.T) {
	f := newPrivacyFixture(t)
	f.carol.RealName = "Bob Builder"
	for name, fn := range map[string]RunnableFunc{"READMSGS": runReadMsgs, "LISTMSGS": runListMsgs} {
		out := f.run(t, fn, f.carol, "NNNQ")
		for _, leak := range []string{"REALNAME-BODY", "Real-name note", "SECRET-BODY", "Secret plans"} {
			if strings.Contains(out, leak) {
				t.Errorf("%s showed %q to Carol after she adopted Bob's real name", name, leak)
			}
		}
	}
}

// TestWithPrivacyNilUserSeesNoPrivateMail pins that a filter built for no user
// (a logged-out path) rejects every private message and still applies f.
func TestWithPrivacyNilUserSeesNoPrivateMail(t *testing.T) {
	onlyOdd := func(m *message.DisplayMessage) bool { return m.MsgNum%2 == 1 }
	f := withPrivacy(nil, onlyOdd)
	if f(&message.DisplayMessage{MsgNum: 1, IsPrivate: true, To: "", From: ""}) {
		t.Error("nil user saw a private message")
	}
	if !f(&message.DisplayMessage{MsgNum: 1}) || f(&message.DisplayMessage{MsgNum: 2}) {
		t.Error("wrapped filter not applied to public messages")
	}
}
