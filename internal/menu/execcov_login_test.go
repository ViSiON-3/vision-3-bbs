package menu

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"golang.org/x/crypto/bcrypt"
)

// execcovLoginUser is an account whose password is "secret". The hash is made
// at bcrypt's minimum cost: AddUser's default cost takes seconds per login
// under the race detector.
func execcovLoginUser(t *testing.T, id int, handle string, level int) *user.User {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte("secret"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	return &user.User{ID: id, Handle: handle, RealName: handle + " Tester", PasswordHash: string(hash),
		AccessLevel: level, Validated: true, TimeLimit: 60}
}

// execcovLoginEnv is execcovRunEnv plus a LOGIN menu whose screen has the
// handle (|P) and password (|O) fields, and a lockout checker that records
// what the login prompt tells it. Three accounts can log in with "secret":
// Alice (level 10), Lowly (5, below the logon level) and Boss (255). After a
// login, LOGIN.CFG sends staff to STAFFM and everyone else to FASTLOGN.
func execcovLoginEnv(t *testing.T) (*menuEnv, *execcovMenus, *fakeLockout) {
	t.Helper()
	env, m := execcovRunEnv(t)
	m.menu("LOGIN", MenuRecord{}, "LOGIN-SCREEN\r\nHandle: |P\r\nPassword: |O\r\nFOOTER-ROW\r\n",
		CommandRecord{Keys: "", Command: "RUN:AUTHENTICATE", Hidden: true},
		CommandRecord{Keys: "", Command: "GOTO:STAFFM", ACS: "S255", Hidden: true},
		CommandRecord{Keys: "", Command: "GOTO:FASTLOGN", Hidden: true})
	env.seedUsers(
		execcovLoginUser(t, 3, "Alice", 10),
		execcovLoginUser(t, 4, "Lowly", 5),
		execcovLoginUser(t, 5, "Boss", 255))
	lock := newFakeLockout()
	env.e.IPLockoutCheck = lock
	env.remoteIP = "203.0.113.5"
	execcovStrings(env, func(s *config.StringsConfig) {
		s.ExecLoginIncorrect = "BAD-LOGIN"
		s.ExecIPLockout = "LOCKED %d MIN"
		s.ExecAccessDenied = "NO-ENTRY"
		s.ExecLoginCriticalError = "LOGIN-BROKEN"
	})
	return env, m, lock
}

// The right handle and password at the LOGIN screen end the run with the
// account and the first default command in LOGIN.CFG the caller may use. The
// detected terminal size and a starting message and file area are saved to
// the account, and the IP's failed attempts are cleared.
func TestExeccovLogin_Success(t *testing.T) {
	env, _, lock := execcovLoginEnv(t)

	r := execcovRun(env, execcovCall{start: "LOGIN", input: "alice\rsecret\r"})
	if r.err != nil || r.next != "GOTO:FASTLOGN" || execcovHandle(r.user) != "Alice" {
		t.Fatalf("next=%q user=%s err=%v, want GOTO:FASTLOGN as Alice", r.next, execcovHandle(r.user), r.err)
	}
	if !r.has("LOGIN-SCREEN", "******") || r.has("secret") {
		t.Errorf("screen or masking wrong:\n%s", r.text())
	}
	if len(lock.cleared) != 1 || lock.cleared[0] != "203.0.113.5" || len(lock.failed) != 0 {
		t.Errorf("lockout cleared=%v failed=%v", lock.cleared, lock.failed)
	}

	saved := env.mustDiskUser(r.user.ID)
	if saved.ScreenWidth != 80 || saved.ScreenHeight != 24 {
		t.Errorf("saved screen = %dx%d, want 80x24", saved.ScreenWidth, saved.ScreenHeight)
	}
	if saved.CurrentMessageAreaID == 0 || saved.CurrentMessageAreaTag == "" {
		t.Errorf("no starting message area saved: %d/%q", saved.CurrentMessageAreaID, saved.CurrentMessageAreaTag)
	}
	if saved.CurrentFileAreaID == 0 || saved.CurrentFileAreaTag == "" {
		t.Errorf("no starting file area saved: %d/%q", saved.CurrentFileAreaID, saved.CurrentFileAreaTag)
	}
}

// The LOGIN screen is cut to the terminal's height like any other, and goes
// out as raw bytes to a CP437 caller.
func TestExeccovLogin_ScreenFitAndEncoding(t *testing.T) {
	env, m, _ := execcovLoginEnv(t)
	m.write("ansi", "LOGIN.ANS", "\xdb LOGIN-SCREEN\r\nHandle: |P\r\nPassword: |O\r\nFOOTER-ROW\r\n")

	r := execcovRun(env, execcovCall{start: "LOGIN", input: "alice\rsecret\r", height: 3})
	if r.next != "GOTO:FASTLOGN" || !strings.Contains(r.raw, "█ LOGIN-SCREEN") || r.has("FOOTER-ROW") {
		t.Errorf("UTF-8, 3 rows: next=%q raw=%q", r.next, r.raw)
	}
	if u := env.mustDiskUser(r.user.ID); u.ScreenHeight != 3 {
		t.Errorf("saved height = %d, want 3", u.ScreenHeight)
	}

	env.outputMode = ansi.OutputModeCP437
	r = execcovRun(env, execcovCall{start: "LOGIN", input: "alice\rsecret\r"})
	if r.next != "GOTO:FASTLOGN" || !strings.Contains(r.raw, "\xdb LOGIN-SCREEN") || !r.has("FOOTER-ROW") {
		t.Errorf("CP437, 24 rows: next=%q raw=%q", r.next, r.raw)
	}
}

// A refused login redraws the LOGIN screen for another go. The refusals: a
// wrong password (counted against the IP), a locked-out IP (even with the
// right password), and an account below the logon level.
func TestExeccovLogin_Refused(t *testing.T) {
	t.Run("wrong password", func(t *testing.T) {
		env, _, lock := execcovLoginEnv(t)
		r := execcovRun(env, execcovCall{start: "LOGIN", input: "alice\rguess\r"})
		if r.err != nil || r.next != "LOGOFF" || r.user != nil {
			t.Errorf("next=%q user=%s err=%v", r.next, execcovHandle(r.user), r.err)
		}
		if !r.has("BAD-LOGIN") || execcovCount(r, "LOGIN-SCREEN") != 2 {
			t.Errorf("output:\n%s", r.text())
		}
		if lock.failed["203.0.113.5"] != 1 || len(lock.cleared) != 0 {
			t.Errorf("lockout failed=%v cleared=%v", lock.failed, lock.cleared)
		}
	})
	t.Run("locked IP", func(t *testing.T) {
		env, _, lock := execcovLoginEnv(t)
		lock.lockedIP, lock.until = "203.0.113.5", time.Now().Add(5*time.Minute)
		r := execcovRun(env, execcovCall{start: "LOGIN", input: "alice\rsecret\r"})
		if r.next != "LOGOFF" || r.user != nil || !r.has("LOCKED 5 MIN") || execcovCount(r, "LOGIN-SCREEN") != 2 {
			t.Errorf("next=%q user=%s output:\n%s", r.next, execcovHandle(r.user), r.text())
		}
		if len(lock.failed) != 0 {
			t.Errorf("a locked-out attempt should not be counted again: %v", lock.failed)
		}
	})
	t.Run("below logon level", func(t *testing.T) {
		env, _, lock := execcovLoginEnv(t)
		r := execcovRun(env, execcovCall{start: "LOGIN", input: "lowly\rsecret\r"})
		if r.next != "LOGOFF" || r.user != nil || !r.has("NO-ENTRY") || execcovCount(r, "LOGIN-SCREEN") != 2 {
			t.Errorf("next=%q user=%s output:\n%s", r.next, execcovHandle(r.user), r.text())
		}
		if len(lock.cleared) != 0 {
			t.Errorf("refused login cleared the IP's failures: %v", lock.cleared)
		}
	})
}

// A blank handle just redraws the screen. ESC or Ctrl+C at either field asks
// "Abort Login?": Yes ends the call, No redraws. A disconnect ends the call.
func TestExeccovLogin_BlankAbortAndDisconnect(t *testing.T) {
	env, _, lock := execcovLoginEnv(t)
	cases := []struct {
		name, input string
		screens     int
		abortAsked  bool
	}{
		{"blank handle", "\r", 2, false},
		{"esc at handle, yes", "\x1bY", 1, true},
		{"esc at handle, no", "\x1bN", 2, true},
		{"esc at password, no", "alice\r\x1bN", 2, true},
		{"ctrl-c at password, yes", "alice\r\x03Y", 1, true},
		{"disconnect at handle", "ali", 1, false},
		{"disconnect at password", "alice\rsec", 1, false},
		{"disconnect at abort prompt", "\x1b", 1, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := execcovRun(env.sub(t), execcovCall{start: "LOGIN", input: tc.input})
			if r.err != nil || r.next != "LOGOFF" || r.user != nil {
				t.Errorf("next=%q user=%s err=%v, want LOGOFF with no user", r.next, execcovHandle(r.user), r.err)
			}
			if got := execcovCount(r, "LOGIN-SCREEN"); got != tc.screens {
				t.Errorf("LOGIN screen drawn %d times, want %d:\n%s", got, tc.screens, r.text())
			}
			if got := r.has("Abort Login?"); got != tc.abortAsked {
				t.Errorf("abort confirmation shown = %v, want %v:\n%s", got, tc.abortAsked, r.text())
			}
		})
	}
	if len(lock.failed) != 0 {
		t.Errorf("none of these is a failed password: %v", lock.failed)
	}
}

// Typing "new" as the handle starts the new user application; hanging up
// inside it ends the call.
func TestExeccovLogin_NewStartsApplication(t *testing.T) {
	env, _, _ := execcovLoginEnv(t)
	r := execcovRun(env, execcovCall{start: "LOGIN", input: "NEW\r"})
	if r.err != nil || r.next != "LOGOFF" || r.user != nil {
		t.Errorf("next=%q user=%s err=%v", r.next, execcovHandle(r.user), r.err)
	}
	if execcovCount(r, "LOGIN-SCREEN") != 1 || r.has("BAD-LOGIN") {
		t.Errorf("\"new\" should not be treated as a handle:\n%s", r.text())
	}
}

// A LOGIN screen without both field markers cannot take a login: the caller
// is told and the run fails.
func TestExeccovLogin_ScreenMissingFields(t *testing.T) {
	env, m, _ := execcovLoginEnv(t)
	m.write("ansi", "LOGIN.ANS", "LOGIN-SCREEN\r\nHandle: |P\r\n")

	r := execcovRun(env, execcovCall{start: "LOGIN", input: "alice\rsecret\r"})
	if r.err == nil || !strings.Contains(r.err.Error(), "missing login coordinates") || r.user != nil {
		t.Errorf("user=%s err=%v", execcovHandle(r.user), r.err)
	}
	if !r.has("LOGIN-BROKEN") {
		t.Errorf("output:\n%s", r.text())
	}
}

// After a login the next step comes from LOGIN.CFG's default commands. The
// first one the caller's level reaches is used; if there is none, or the file
// cannot be read, the caller is logged off with an error.
func TestExeccovLogin_PostAuthAction(t *testing.T) {
	env, m, _ := execcovLoginEnv(t)
	r := execcovRun(env, execcovCall{start: "LOGIN", input: "boss\rsecret\r"})
	if r.err != nil || r.next != "GOTO:STAFFM" || execcovHandle(r.user) != "Boss" {
		t.Errorf("sysop: next=%q user=%s err=%v, want GOTO:STAFFM", r.next, execcovHandle(r.user), r.err)
	}

	m.menu("LOGIN", MenuRecord{}, "Handle: |P\r\nPassword: |O\r\n",
		CommandRecord{Keys: "", Command: "RUN:AUTHENTICATE"},
		CommandRecord{Keys: "X", Command: "GOTO:MAIN"},
		CommandRecord{Keys: "", Command: "GOTO:STAFFM", ACS: "S255"})
	r = execcovRun(env, execcovCall{start: "LOGIN", input: "alice\rsecret\r"})
	if r.next != "LOGOFF" || r.err == nil || !strings.Contains(r.err.Error(), "no accessible default command") {
		t.Errorf("nothing for a caller: next=%q err=%v", r.next, r.err)
	}

	m.write("cfg", "LOGIN.CFG", "{not json")
	r = execcovRun(env, execcovCall{start: "LOGIN", input: "alice\rsecret\r"})
	if r.next != "LOGOFF" || r.err == nil || !strings.Contains(r.err.Error(), "failed loading LOGIN.CFG") {
		t.Errorf("broken LOGIN.CFG: next=%q err=%v", r.next, r.err)
	}
}

// A caller who is already logged in (SSH key auth, say) skips the LOGIN
// screen and lands in MAIN with a starting area chosen and saved.
func TestExeccovLogin_AlreadyAuthenticated(t *testing.T) {
	env, m, _ := execcovLoginEnv(t)
	m.menu("MAIN", MenuRecord{}, "MAIN-SCREEN area=|CA/|CAN.\r\n")

	r := execcovRun(env, execcovCall{user: env.caller, start: "LOGIN"})
	if r.err != nil || r.next != "LOGOFF" || execcovHandle(r.user) != "Caller" {
		t.Fatalf("next=%q user=%s err=%v", r.next, execcovHandle(r.user), r.err)
	}
	if r.has("LOGIN-SCREEN") || !r.has("MAIN-SCREEN") {
		t.Fatalf("should go straight to MAIN:\n%s", r.text())
	}

	saved := env.mustDiskUser(env.caller.ID)
	area, ok := env.e.MessageMgr.GetAreaByID(saved.CurrentMessageAreaID)
	if !ok || saved.CurrentFileAreaID == 0 {
		t.Fatalf("saved areas: message %d (found=%v), file %d", saved.CurrentMessageAreaID, ok, saved.CurrentFileAreaID)
	}
	if want := "area=" + area.Tag + "/" + area.Name + "."; !r.has(want) {
		t.Errorf("MAIN should name the chosen area, want %q:\n%s", want, r.text())
	}
}

// execcovLoginSeq runs the login sequence as u over a session with a stderr
// stream, returning the menu RunLoginSequence says to enter.
func execcovLoginSeq(env *menuEnv, u *user.User, input string) (r runResult, menu string) {
	env.t.Helper()
	fn := func(c *cmdCtx, _ string) (*user.User, string, error) {
		var err error
		menu, err = env.e.RunLoginSequence(c.s, c.terminal, c.userManager, c.currentUser,
			c.nodeNumber, c.sessionStartTime, c.outputMode, c.termWidth, c.termHeight)
		return nil, "", err
	}
	r, _ = execcovRunFn(env, fn, u, "", input)
	return r, menu
}

// The login sequence runs its items in order: each may clear the screen
// first and pause after, is skipped for a caller below its level, and is
// skipped with a log line when its command is unknown or its file missing.
// It ends at MAIN.
func TestExeccovLoginSequence_Items(t *testing.T) {
	env, m := execcovRunEnv(t)
	m.write("ansi", "WELCOME.ANS", "WELCOME-ART\r\n")
	m.write("ansi", "STAFF.ANS", "STAFF-ART\r\n")
	execcovStrings(env, func(s *config.StringsConfig) {
		s.PauseString = "PAUSE-HERE"
		s.ExecFileLoadError = "\r\nNOFILE %s\r\n"
	})
	env.e.SetLoginSequence([]config.LoginItem{
		{Command: "DISPLAYFILE", Data: " WELCOME.ANS ", ClearScreen: true, PauseAfter: true},
		{Command: "DISPLAYFILE", Data: "STAFF.ANS", SecLevel: 255},
		{Command: "NOSUCHCOMMAND", PauseAfter: true},
		{Command: "DISPLAYFILE"},
		{Command: "DISPLAYFILE", Data: "MISSING.ANS"},
	})

	r, menu := execcovLoginSeq(env, env.caller, "\r")
	if r.err != nil || menu != "MAIN" {
		t.Fatalf("menu=%q err=%v, want MAIN", menu, r.err)
	}
	if !strings.Contains(r.raw, "\x1b[2J\x1b[H") || !r.has("WELCOME-ART", "NOFILE MISSING.ANS") || r.has("STAFF-ART") {
		t.Errorf("caller output:\n%s", r.text())
	}
	if execcovCount(r, "PAUSE-HERE") != 1 {
		t.Errorf("want one pause, after the first item only:\n%s", r.text())
	}

	if r, menu = execcovLoginSeq(env, env.sysop, "\r"); menu != "MAIN" || !r.has("WELCOME-ART", "STAFF-ART") {
		t.Errorf("sysop: menu=%q output:\n%s", menu, r.text())
	}

	// Hanging up at the pause ends the call.
	r, menu = execcovLoginSeq(env, env.caller, "")
	if menu != "LOGOFF" || !errors.Is(r.err, io.EOF) || r.has("NOFILE") {
		t.Errorf("disconnect at pause: menu=%q err=%v output:\n%s", menu, r.err, r.text())
	}

	// With no pause string configured there is still a prompt to wait at.
	execcovStrings(env, func(s *config.StringsConfig) { s.PauseString = "" })
	if r, menu = execcovLoginSeq(env, env.caller, "\r"); menu != "MAIN" || !r.has("to continue") {
		t.Errorf("default pause: menu=%q output:\n%s", menu, r.text())
	}
}

// A DOOR: item goes to the door handler, and what that returns can cut the
// sequence short: a GOTO names the menu to enter instead of MAIN, LOGOFF or
// a disconnect ends the call. Later items do not run.
func TestExeccovLoginSequence_DoorItem(t *testing.T) {
	env, m := execcovRunEnv(t)
	m.write("ansi", "AFTER.ANS", "AFTER-DOOR\r\n")
	env.e.SetLoginSequence([]config.LoginItem{
		{Command: "DOOR:Trivia"},
		{Command: "DISPLAYFILE", Data: "AFTER.ANS"},
	})
	var (
		doors []string
		next  string
		fail  error
	)
	env.e.RunRegistry["DOOR:"] = func(c *cmdCtx, name string) (*user.User, string, error) {
		doors = append(doors, name)
		return c.currentUser, next, fail
	}

	r, menu := execcovLoginSeq(env, env.caller, "")
	if r.err != nil || menu != "MAIN" || len(doors) != 1 || doors[0] != "Trivia" || !r.has("AFTER-DOOR") {
		t.Errorf("door ran: menu=%q doors=%v err=%v output:\n%s", menu, doors, r.err, r.text())
	}

	next = "GOTO:filem"
	if r, menu = execcovLoginSeq(env, env.caller, ""); r.err != nil || menu != "FILEM" || r.has("AFTER-DOOR") {
		t.Errorf("door GOTO: menu=%q err=%v output:\n%s", menu, r.err, r.text())
	}
	next = "LOGOFF"
	if r, menu = execcovLoginSeq(env, env.caller, ""); r.err != nil || menu != "LOGOFF" || r.has("AFTER-DOOR") {
		t.Errorf("door LOGOFF: menu=%q err=%v output:\n%s", menu, r.err, r.text())
	}

	// A door that fails is logged and the sequence carries on; a disconnect
	// inside it is the end of the call.
	next, fail = "", errors.New("door crashed")
	if r, menu = execcovLoginSeq(env, env.caller, ""); r.err != nil || menu != "MAIN" || !r.has("AFTER-DOOR") {
		t.Errorf("door error: menu=%q err=%v output:\n%s", menu, r.err, r.text())
	}
	fail = io.EOF
	if r, menu = execcovLoginSeq(env, env.caller, ""); !errors.Is(r.err, io.EOF) || menu != "LOGOFF" || r.has("AFTER-DOOR") {
		t.Errorf("disconnect in door: menu=%q err=%v output:\n%s", menu, r.err, r.text())
	}

	delete(env.e.RunRegistry, "DOOR:")
	if r, menu = execcovLoginSeq(env, env.caller, ""); r.err != nil || menu != "MAIN" || !r.has("AFTER-DOOR") {
		t.Errorf("no door handler: menu=%q err=%v output:\n%s", menu, r.err, r.text())
	}
}

// RUNDOOR runs a script with the node number as its argument and the session
// as its output. A script that is missing, unnamed or fails does not stop
// the login.
func TestExeccovLoginSequence_RunDoorScript(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs a POSIX shell script")
	}
	env, _ := execcovRunEnv(t)
	dir := t.TempDir()
	script := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		return p
	}
	ok := script("ok.sh", `echo "SCRIPT-SAYS node=$1"`)
	bad := script("bad.sh", `echo "SCRIPT-FAILING"; exit 3`)

	env.e.SetLoginSequence([]config.LoginItem{
		{Command: "RUNDOOR", Data: " " + ok + " "},
		{Command: "RUNDOOR", Data: bad},
		{Command: "RUNDOOR", Data: filepath.Join(dir, "missing.sh")},
		{Command: "RUNDOOR"},
	})
	r, menu := execcovLoginSeq(env, env.caller, "")
	if r.err != nil || menu != "MAIN" {
		t.Fatalf("menu=%q err=%v", menu, r.err)
	}
	if !r.has("SCRIPT-SAYS node=1", "SCRIPT-FAILING") {
		t.Errorf("script output missing:\n%s", r.text())
	}
}

// NMAILSCAN counts the caller's unread private mail and offers to read it.
// No drops back into the login; Yes opens the mail reader.
func TestExeccovNewMailScan(t *testing.T) {
	env := newMsgEnv(t)
	execcovStrings(env, func(s *config.StringsConfig) {
		s.ExecNoNewMail = "\r\nNO-NEW-MAIL\r\n"
		s.ExecNewMailCount = "\r\nNEW-MAIL=%d\r\n"
	})

	if r := env.run(runNewMailScan, env.caller, "", ""); r.err != nil || !r.has("NO-NEW-MAIL") || execcovHandle(r.user) != "Caller" {
		t.Errorf("empty mailbox: user=%s err=%v output:\n%s", execcovHandle(r.user), r.err, r.text())
	}
	if r := env.run(runNewMailScan, nil, "", ""); r.user != nil || r.raw != "" {
		t.Errorf("no user: user=%s output %q", execcovHandle(r.user), r.raw)
	}

	seedPrivmail(env)
	r := env.run(runNewMailScan, env.caller, "", "N")
	if r.next != "" || execcovHandle(r.user) != "Caller" || !r.has("NEW-MAIL=2", "Read it now?") || r.has("for-caller-1-body") {
		t.Errorf("declined: next=%q user=%s output:\n%s", r.next, execcovHandle(r.user), r.text())
	}
	if r = env.run(runNewMailScan, env.sysop, "", "N"); !r.has("NEW-MAIL=1") {
		t.Errorf("sysop has one message:\n%s", r.text())
	}
	if r = env.run(runNewMailScan, env.caller, "", ""); r.next != "LOGOFF" || r.user != nil {
		t.Errorf("disconnect at the offer: next=%q user=%s", r.next, execcovHandle(r.user))
	}

	r = env.run(runNewMailScan, env.caller, "", "YQ")
	if !r.has("NEW-MAIL=2", "for-caller-1-body") {
		t.Errorf("accepted: the reader should open on the first message:\n%s", r.text())
	}

	// Without a message manager there is nothing to scan.
	mm := env.e.MessageMgr
	env.e.MessageMgr = nil
	if r = env.run(runNewMailScan, env.caller, "", ""); r.user != env.caller || r.raw != "" {
		t.Errorf("no message manager: user=%s output %q", execcovHandle(r.user), r.raw)
	}
	env.e.MessageMgr = mm

	// Mail already read is not new.
	env.markRead(privmailAreaID, "Caller", 4)
	if r = env.run(runNewMailScan, env.caller, "", ""); !r.has("NO-NEW-MAIL") || r.has("NEW-MAIL=") {
		t.Errorf("all read:\n%s", r.text())
	}
}

// The handle and password are typed at the screen's |P and |O positions, in
// the colour the art had there, and a refusal is written two rows below the
// lower field, wherever the art puts the two.
func TestExeccovLogin_FieldPlacement(t *testing.T) {
	env, m, _ := execcovLoginEnv(t)
	// Password on row 1, handle on row 4: the message goes on row 6.
	m.write("ansi", "LOGIN.ANS", "Password: \x1b[1;36m|O\x1b[0m\r\n\r\n\r\nHandle: \x1b[1;33m|P\x1b[0m\r\n")

	r := execcovRun(env, execcovCall{start: "LOGIN", input: "alice\rguess\r"})
	handleAt, passAt := ansi.MoveCursor(4, 9), ansi.MoveCursor(1, 11)
	hi, pi := strings.Index(r.raw, handleAt), strings.Index(r.raw, passAt)
	if hi < 0 || pi < hi {
		t.Fatalf("want the cursor at the handle field (4,9) then the password field (1,11): %q", r.raw)
	}
	if !strings.HasPrefix(r.raw[hi+len(handleAt):], "\x1b[") || !strings.Contains(r.raw[hi:pi], "33m") {
		t.Errorf("handle field should be typed in the art's yellow: %q", r.raw[hi:pi])
	}
	if !strings.HasPrefix(r.raw[pi+len(passAt):], "\x1b[") || !strings.Contains(r.raw[pi:pi+len(passAt)+12], "36m") {
		t.Errorf("password field should be typed in the art's cyan: %q", r.raw[pi:])
	}
	if !strings.Contains(r.raw, ansi.MoveCursor(6, 1)+"BAD-LOGIN") {
		t.Errorf("refusal should be written at row 6: %q", r.raw)
	}

	// The usual layout, handle above password: two rows below the password.
	m.write("ansi", "LOGIN.ANS", "Handle: |P\r\nPassword: |O\r\n")
	r = execcovRun(env, execcovCall{start: "LOGIN", input: "alice\rguess\r"})
	if !strings.Contains(r.raw, ansi.MoveCursor(4, 1)+"BAD-LOGIN") {
		t.Errorf("refusal should be written at row 4: %q", r.raw)
	}
}
