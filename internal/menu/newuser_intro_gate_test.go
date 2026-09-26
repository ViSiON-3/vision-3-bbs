package menu

import (
	"errors"
	"io"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// introGateFixture is a board with a SysOp (user #1), a PRIVMAIL area and a
// freshly signed-up caller who still owes the SysOp the intro message.
type introGateFixture struct {
	e         *MenuExecutor
	um        *user.UserMgr
	mm        *message.MessageManager
	privmail  int
	sysop     *user.User
	newcomer  *user.User
	editorRan int
}

func newIntroGateFixture(t *testing.T, cfg config.ServerConfig) *introGateFixture {
	t.Helper()
	mm, err := message.NewMessageManager(t.TempDir(), t.TempDir(), "TestBBS", nil)
	if err != nil {
		t.Fatalf("NewMessageManager: %v", err)
	}
	if _, err := mm.AddArea(message.MessageArea{Tag: "PRIVMAIL", Name: "Private Mail", AreaType: "local"}); err != nil {
		t.Fatalf("AddArea: %v", err)
	}
	// The user store seeds the default SysOp account as user #1 — the account
	// the gate addresses.
	area, ok := mm.GetAreaByTag("PRIVMAIL")
	if !ok {
		t.Fatal("PRIVMAIL area missing")
	}
	um, err := user.NewUserManager(t.TempDir())
	if err != nil {
		t.Fatalf("NewUserManager: %v", err)
	}
	sysop, ok := um.GetUserByID(1)
	if !ok {
		t.Fatal("default SysOp (user #1) was not created")
	}
	newcomer, err := um.AddUser("password", "Newcomer", "New Comer", "There")
	if err != nil {
		t.Fatalf("AddUser newcomer: %v", err)
	}
	newcomer.AccessLevel = 25
	newcomer.IntroPending = true
	if err := um.UpdateUser(newcomer); err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}

	origPause := accessDeniedPause
	accessDeniedPause = 0
	t.Cleanup(func() { accessDeniedPause = origPause })

	e := &MenuExecutor{MessageMgr: mm, MenuSetPath: t.TempDir()}
	e.SetServerConfig(cfg)
	return &introGateFixture{e: e, um: um, mm: mm, privmail: area.ID, sysop: sysop, newcomer: newcomer}
}

// stubEditor replaces the full-screen editor for the test's duration with one
// that returns the given result, counting how often it is opened.
func (f *introGateFixture) stubEditor(t *testing.T, body string, saved bool, err error) {
	t.Helper()
	orig := introEditor
	t.Cleanup(func() { introEditor = orig })
	introEditor = func(string, io.Reader, io.Writer, ansi.OutputMode, string, string, string, bool,
		string, string, string, string, bool, []string, *editor.InputHandler, ...editor.EditorContext) (string, bool, error) {
		f.editorRan++
		return body, saved, err
	}
}

func (f *introGateFixture) privmailCount(t *testing.T) int {
	t.Helper()
	n, err := f.mm.GetMessageCountForArea(f.privmail)
	if err != nil {
		t.Fatalf("GetMessageCountForArea: %v", err)
	}
	return n
}

func (f *introGateFixture) runGate(t *testing.T) (proceed, sent bool, err error) {
	t.Helper()
	ts := newTestSession("")
	return f.e.runNewUserIntroGate(ts, newTestTerminal(ts), f.um, f.newcomer, 1, ansi.OutputModeCP437, 80, 24)
}

// An idle timeout in the editor used to fall through as an editor failure,
// which cleared the obligation and let the caller in without writing a word.
// It must count as an abandoned attempt instead, exactly like hanging up.
func TestIntroGate_IdleTimeoutCountsAsAbandoned(t *testing.T) {
	f := newIntroGateFixture(t, config.ServerConfig{})
	f.stubEditor(t, "", false, editor.ErrIdleTimeout)

	proceed, sent, err := f.runGate(t)
	if proceed || sent || !errors.Is(err, io.EOF) {
		t.Fatalf("runNewUserIntroGate = (%v, %v, %v), want (false, false, io.EOF)", proceed, sent, err)
	}
	if f.editorRan != 1 {
		t.Errorf("editor opened %d times, want 1", f.editorRan)
	}
	stored, ok := f.um.GetUserByID(f.newcomer.ID)
	if !ok {
		t.Fatal("newcomer missing after gate")
	}
	if !stored.IntroPending {
		t.Error("IntroPending cleared by an idle timeout; the obligation must survive to the next login")
	}
	if stored.IntroAttempts != 1 {
		t.Errorf("IntroAttempts = %d, want 1", stored.IntroAttempts)
	}
	if n := f.privmailCount(t); n != 0 {
		t.Errorf("PRIVMAIL holds %d messages, want 0", n)
	}
}

// Control for the test above: the same fixture delivers a real message and
// clears the obligation when the caller does write one.
func TestIntroGate_SentClearsObligation(t *testing.T) {
	f := newIntroGateFixture(t, config.ServerConfig{})
	f.stubEditor(t, "Hi, I found you on a BBS list.", true, nil)

	proceed, sent, err := f.runGate(t)
	if !proceed || !sent || err != nil {
		t.Fatalf("runNewUserIntroGate = (%v, %v, %v), want (true, true, nil)", proceed, sent, err)
	}
	stored, _ := f.um.GetUserByID(f.newcomer.ID)
	if stored.IntroPending {
		t.Error("IntroPending still set after the message was sent")
	}
	if n := f.privmailCount(t); n != 1 {
		t.Fatalf("PRIVMAIL holds %d messages, want 1", n)
	}
}

func (f *introGateFixture) admit(t *testing.T, u *user.User) (bool, error) {
	t.Helper()
	ts := newTestSession("")
	return f.e.AdmitPreAuthenticatedUser(ts, newTestTerminal(ts), f.um, u, 1, ansi.OutputModeCP437, 80, 24)
}

// A password verified at the SSH layer skips the LOGIN prompt, and used to skip
// the intro gate with it: the caller went straight to the main menu.
func TestAdmitPreAuthenticatedUser_RunsIntroGate(t *testing.T) {
	f := newIntroGateFixture(t, config.ServerConfig{})
	f.stubEditor(t, "", false, io.EOF)

	admitted, err := f.admit(t, f.newcomer)
	if admitted || !errors.Is(err, io.EOF) {
		t.Fatalf("AdmitPreAuthenticatedUser = (%v, %v), want (false, io.EOF)", admitted, err)
	}
	if f.editorRan != 1 {
		t.Errorf("intro editor opened %d times, want 1", f.editorRan)
	}
	stored, _ := f.um.GetUserByID(f.newcomer.ID)
	if !stored.IntroPending || stored.IntroAttempts != 1 {
		t.Errorf("after abandoning: IntroPending=%v IntroAttempts=%d, want true/1", stored.IntroPending, stored.IntroAttempts)
	}
}

func TestAdmitPreAuthenticatedUser_SentIsAdmitted(t *testing.T) {
	f := newIntroGateFixture(t, config.ServerConfig{})
	f.stubEditor(t, "Hello!", true, nil)

	admitted, err := f.admit(t, f.newcomer)
	if !admitted || err != nil {
		t.Fatalf("AdmitPreAuthenticatedUser = (%v, %v), want (true, nil)", admitted, err)
	}
	if n := f.privmailCount(t); n != 1 {
		t.Errorf("PRIVMAIL holds %d messages, want 1", n)
	}
}

// The SSH path also skipped the logon-level check the LOGIN prompt applies. A
// caller below it is refused outright (the session handler disconnects them)
// rather than sent to the LOGIN prompt to authenticate a second time.
func TestAdmitPreAuthenticatedUser_EnforcesLogonLevel(t *testing.T) {
	f := newIntroGateFixture(t, config.ServerConfig{LogonLevel: 50})
	f.stubEditor(t, "", false, errors.New("editor must not open"))
	f.newcomer.IntroPending = false

	admitted, err := f.admit(t, f.newcomer)
	if admitted || err != nil {
		t.Fatalf("AdmitPreAuthenticatedUser = (%v, %v), want (false, nil)", admitted, err)
	}
	if f.editorRan != 0 {
		t.Errorf("intro editor opened %d times for a caller with nothing owed", f.editorRan)
	}
}

func TestAdmitPreAuthenticatedUser_OrdinaryUserAdmitted(t *testing.T) {
	f := newIntroGateFixture(t, config.ServerConfig{LogonLevel: 10})
	f.stubEditor(t, "", false, errors.New("editor must not open"))

	admitted, err := f.admit(t, f.sysop)
	if !admitted || err != nil {
		t.Fatalf("AdmitPreAuthenticatedUser = (%v, %v), want (true, nil)", admitted, err)
	}
	if f.editorRan != 0 {
		t.Errorf("intro editor opened %d times for a caller with nothing owed", f.editorRan)
	}
}

// The pre-gate idle timeout goes through the session-level store, so it
// survives an InputHandler reset; once admitted, the caller's own timeout
// replaces it (a co-SysOp or above is exempt), and session teardown clears it.
func TestAdmitPreAuthenticatedUser_IdleTimeoutLifecycle(t *testing.T) {
	f := newIntroGateFixture(t, config.ServerConfig{SessionIdleTimeoutMinutes: 5, CoSysOpLevel: 250})
	f.stubEditor(t, "Hello!", true, nil)

	ts := newTestSession("")
	t.Cleanup(func() { ClearSessionIdleTimeout(ts) })
	admitted, err := f.e.AdmitPreAuthenticatedUser(ts, newTestTerminal(ts), f.um, f.newcomer, 1, ansi.OutputModeCP437, 80, 24)
	if !admitted || err != nil {
		t.Fatalf("AdmitPreAuthenticatedUser = (%v, %v), want (true, nil)", admitted, err)
	}

	stored := func() (time.Duration, bool) {
		v, ok := sessionIdleTimeouts.Load(ts)
		if !ok {
			return 0, false
		}
		return v.(time.Duration), true
	}
	if d, ok := stored(); !ok || d != 5*time.Minute {
		t.Fatalf("pre-gate timeout = (%v, %v), want (5m, stored)", d, ok)
	}

	f.sysop.AccessLevel = 255
	f.e.ApplyUserIdleTimeout(ts, f.sysop)
	if d, ok := stored(); !ok || d != 0 {
		t.Errorf("after ApplyUserIdleTimeout(sysop) = (%v, %v), want (0, stored)", d, ok)
	}

	ClearSessionIdleTimeout(ts)
	if _, ok := stored(); ok {
		t.Error("ClearSessionIdleTimeout left the session's timeout stored")
	}
}
