package menu

import (
	"errors"
	"io"
	"regexp"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"github.com/ViSiON-3/vision-3-bbs/internal/session"
	"github.com/ViSiON-3/vision-3-bbs/internal/types"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// execcovRunEnv is a menuEnv on an empty menu set with two menus, MAIN and
// SUB. MAIN goes to SUB on A and logs off on Q; SUB has no commands beyond
// the ones a test adds. The "unknown command" text is pinned to UNKNOWN-CMD.
func execcovRunEnv(t *testing.T) (*menuEnv, *execcovMenus) {
	t.Helper()
	env := newMenuEnv(t)
	m := execcovNewMenus(t, env)
	m.menu("MAIN", MenuRecord{}, "MAIN-SCREEN\r\n",
		CommandRecord{Keys: "A", Command: "GOTO:sub"},
		CommandRecord{Keys: "Q", Command: "LOGOFF"})
	m.menu("SUB", MenuRecord{ClrScrBefore: true}, "SUB-SCREEN\r\n")
	execcovStrings(env, func(s *config.StringsConfig) {
		s.ExecUnknownCommand = "\r\nUNKNOWN-CMD\r\n"
	})
	return env, m
}

// A GOTO command moves to the named menu, which is looked up in upper case
// and cleared first when its record says so; LOGOFF ends the run and hands
// back the user.
func TestExeccovRun_GotoThenLogoff(t *testing.T) {
	env, m := execcovRunEnv(t)
	m.menu("SUB", MenuRecord{ClrScrBefore: true}, "SUB-SCREEN\r\n",
		CommandRecord{Keys: "Q", Command: "LOGOFF"})

	r := execcovRun(env, execcovCall{user: env.caller, start: "main", input: "a\rq\r"})
	if r.err != nil || r.next != "LOGOFF" || execcovHandle(r.user) != "Caller" {
		t.Fatalf("next=%q user=%s err=%v, want LOGOFF as Caller", r.next, execcovHandle(r.user), r.err)
	}
	if !r.has("MAIN-SCREEN", "SUB-SCREEN") {
		t.Errorf("both screens should be drawn:\n%s", r.text())
	}
	if !strings.Contains(r.raw, ansi.ClearScreen()+"SUB-SCREEN") {
		t.Errorf("SUB has CLR set but was not cleared first: %q", r.raw)
	}
	if strings.Contains(r.raw, ansi.ClearScreen()+"MAIN-SCREEN") {
		t.Errorf("MAIN has no CLR but was cleared: %q", r.raw)
	}
}

// Input that matches no command shows the unknown-command text and redraws
// the menu, unless the menu names a fallback, which is entered instead. Enter
// on its own just redraws. Running out of input is a disconnect.
func TestExeccovRun_UnmatchedInput(t *testing.T) {
	t.Run("no fallback", func(t *testing.T) {
		env, _ := execcovRunEnv(t)
		r := execcovRun(env, execcovCall{user: env.caller, start: "MAIN", input: "Z\rQ\r"})
		if r.next != "LOGOFF" || !r.has("UNKNOWN-CMD") || execcovCount(r, "MAIN-SCREEN") != 2 {
			t.Errorf("next=%q output:\n%s", r.next, r.text())
		}
	})
	t.Run("fallback menu", func(t *testing.T) {
		env, m := execcovRunEnv(t)
		m.menu("MAIN", MenuRecord{Fallback: "sub"}, "MAIN-SCREEN\r\n")
		r := execcovRun(env, execcovCall{user: env.caller, start: "MAIN", input: "Z\r"})
		if r.next != "LOGOFF" || execcovHandle(r.user) != "Caller" {
			t.Errorf("disconnect in SUB: next=%q user=%s", r.next, execcovHandle(r.user))
		}
		if !r.has("SUB-SCREEN") || r.has("UNKNOWN-CMD") {
			t.Errorf("fallback not taken:\n%s", r.text())
		}
	})
	t.Run("enter redraws", func(t *testing.T) {
		env, _ := execcovRunEnv(t)
		r := execcovRun(env, execcovCall{user: env.caller, start: "MAIN", input: "\rQ\r"})
		if execcovCount(r, "MAIN-SCREEN") != 2 || r.has("UNKNOWN-CMD") {
			t.Errorf("output:\n%s", r.text())
		}
	})
}

// ^P returns to the menu the caller came from, and only redraws when there
// is none.
func TestExeccovRun_BackNavigation(t *testing.T) {
	env, _ := execcovRunEnv(t)

	r := execcovRun(env, execcovCall{user: env.caller, start: "MAIN", input: "A\r^P\rQ\r"})
	if r.next != "LOGOFF" || execcovCount(r, "MAIN-SCREEN") != 2 || execcovCount(r, "SUB-SCREEN") != 1 {
		t.Errorf("MAIN > SUB > ^P: next=%q output:\n%s", r.next, r.text())
	}

	// Twice swaps back again.
	r = execcovRun(env, execcovCall{user: env.caller, start: "MAIN", input: "A\r^P\r^P\r"})
	if execcovCount(r, "SUB-SCREEN") != 2 {
		t.Errorf("second ^P should return to SUB:\n%s", r.text())
	}

	r = execcovRun(env, execcovCall{user: env.caller, start: "MAIN", input: "^P\rQ\r"})
	if r.next != "LOGOFF" || execcovCount(r, "MAIN-SCREEN") != 2 || r.has("UNKNOWN-CMD") {
		t.Errorf("^P with nowhere to go: next=%q output:\n%s", r.next, r.text())
	}
}

// A menu with a password asks up to three times. The right one lets the
// caller in; three wrong ones, a disconnect or an abort end the call with no
// user.
func TestExeccovRun_MenuPassword(t *testing.T) {
	env, m := execcovRunEnv(t)
	m.menu("VAULT", MenuRecord{Password: "sesame"}, "VAULT-SCREEN\r\n",
		CommandRecord{Keys: "Q", Command: "LOGOFF"})
	execcovStrings(env, func(s *config.StringsConfig) {
		s.ExecMenuPasswordPrompt = "\r\nPW %s try %d: "
		s.ExecPasswordAccepted = "PW-OK\r\n"
		s.ExecIncorrectPassword = "PW-BAD\r\n"
		s.ExecTooManyAttempts = "PW-LOCKED\r\n"
	})

	r := execcovRun(env, execcovCall{user: env.caller, start: "VAULT", input: "nope\rsesame\rQ\r"})
	if r.next != "LOGOFF" || execcovHandle(r.user) != "Caller" {
		t.Fatalf("after right password: next=%q user=%s", r.next, execcovHandle(r.user))
	}
	if !r.has("PW VAULT try 1: ", "PW-BAD", "PW VAULT try 2: ", "PW-OK") || r.has("try 3") {
		t.Errorf("prompts:\n%s", r.text())
	}
	if strings.Contains(r.text(), "sesame") || !strings.Contains(r.text(), "******") {
		t.Errorf("password should be masked:\n%s", r.text())
	}

	for name, input := range map[string]string{
		"three wrong": "a\rb\rc\r",
		"disconnect":  "ses",
		"ctrl-c":      "\x03",
	} {
		r := execcovRun(env, execcovCall{user: env.caller, start: "VAULT", input: input})
		if r.err != nil || r.next != "LOGOFF" || r.user != nil {
			t.Errorf("%s: next=%q user=%s err=%v, want LOGOFF with no user", name, r.next, execcovHandle(r.user), r.err)
		}
		if got := r.has("PW-LOCKED"); got != (name == "three wrong") {
			t.Errorf("%s: too-many-attempts shown = %v", name, got)
		}
	}
}

// A menu the caller's level does not reach is refused before any of it runs:
// no autorun command, no screen, and the call ends.
func TestExeccovRun_MenuACSDenied(t *testing.T) {
	env, m := execcovRunEnv(t)
	ran := false
	env.e.RunRegistry["EXECCOVAUTO"] = func(c *cmdCtx, _ string) (*user.User, string, error) {
		ran = true
		return c.currentUser, "", nil
	}
	m.menu("STAFF", MenuRecord{ACS: "S255"}, "STAFF-SCREEN\r\n",
		CommandRecord{Keys: "~~", Command: "RUN:EXECCOVAUTO"},
		CommandRecord{Keys: "Q", Command: "LOGOFF"})
	execcovStrings(env, func(s *config.StringsConfig) { s.ExecAccessDenied = "\r\nNO-ENTRY\r\n" })

	r := execcovRun(env, execcovCall{user: env.caller, start: "STAFF", input: "Q\r"})
	if r.err != nil || r.next != "LOGOFF" || r.user != nil {
		t.Errorf("next=%q user=%s err=%v", r.next, execcovHandle(r.user), r.err)
	}
	if !r.has("NO-ENTRY") || r.has("STAFF-SCREEN") || ran {
		t.Errorf("denied menu ran anyway (autorun=%v):\n%s", ran, r.text())
	}

	// Nor is a caller who has not logged in.
	r = execcovRun(env, execcovCall{start: "STAFF", input: "Q\r"})
	if r.next != "LOGOFF" || !r.has("NO-ENTRY") || r.has("STAFF-SCREEN") || ran {
		t.Errorf("guest: next=%q autorun=%v output:\n%s", r.next, ran, r.text())
	}

	r = execcovRun(env, execcovCall{user: env.sysop, start: "STAFF", input: "Q\r"})
	if r.next != "LOGOFF" || !r.has("STAFF-SCREEN") || r.has("NO-ENTRY") || !ran {
		t.Errorf("sysop should get in (autorun=%v): next=%q\n%s", ran, r.next, r.text())
	}
}

// A menu with no screen file, or a screen but no record, stops the run with
// an error and tells the caller which file was the problem.
func TestExeccovRun_MissingMenuFiles(t *testing.T) {
	env, m := execcovRunEnv(t)
	execcovStrings(env, func(s *config.StringsConfig) { s.ExecMenuLoadError = "\r\nLOADERR %s: %v\r\n" })

	r := execcovRun(env, execcovCall{user: env.caller, start: "NOPE", input: "Q\r"})
	if r.err == nil || !strings.Contains(r.err.Error(), "failed to read screen file NOPE.ANS") || r.user != nil {
		t.Errorf("no screen: user=%s err=%v", execcovHandle(r.user), r.err)
	}
	if !r.has("Error reading screen file: NOPE.ANS") {
		t.Errorf("no screen: output:\n%s", r.text())
	}

	m.write("ansi", "HALF.ANS", "HALF-SCREEN\r\n")
	r = execcovRun(env, execcovCall{user: env.caller, start: "HALF", input: "Q\r"})
	if r.err == nil || !strings.Contains(r.err.Error(), "failed to load menu HALF") || r.user != nil {
		t.Errorf("no record: user=%s err=%v", execcovHandle(r.user), r.err)
	}
	if !r.has("LOADERR HALF: ") || r.has("HALF-SCREEN") {
		t.Errorf("no record: output:\n%s", r.text())
	}

	// A command file that cannot be parsed is not fatal: the menu comes up
	// with no commands of its own.
	m.write("cfg", "MAIN.CFG", "{not json")
	r = execcovRun(env, execcovCall{user: env.caller, start: "MAIN", input: "Q\r"})
	if r.err != nil || r.next != "LOGOFF" || execcovCount(r, "MAIN-SCREEN") != 2 || !r.has("UNKNOWN-CMD") {
		t.Errorf("broken commands: next=%q err=%v output:\n%s", r.next, r.err, r.text())
	}
}

// A user whose account has gone since they logged in is refused at the next
// menu rather than carried on as a ghost.
func TestExeccovRun_AccountRemovedMidSession(t *testing.T) {
	env, _ := execcovRunEnv(t)
	execcovStrings(env, func(s *config.StringsConfig) { s.ExecAccessDenied = "\r\nNO-ENTRY\r\n" })

	ghost := &user.User{ID: 42, Handle: "Ghost", AccessLevel: 10}
	r := execcovRun(env, execcovCall{user: ghost, start: "MAIN", input: "Q\r"})
	if r.err != nil || r.next != "LOGOFF" || r.user != nil {
		t.Errorf("next=%q user=%s err=%v", r.next, execcovHandle(r.user), r.err)
	}
	if !r.has("NO-ENTRY") || r.has("MAIN-SCREEN") {
		t.Errorf("output:\n%s", r.text())
	}
}

// Autorun commands run before the screen is drawn: "//" once per session
// (tracked in the AutoRunTracker), "~~" on every visit, and neither when the
// caller fails the command's ACS.
func TestExeccovRun_AutoRunOnceAndEvery(t *testing.T) {
	env, m := execcovRunEnv(t)
	calls := map[string]int{}
	for _, name := range []string{"EXECCOVONCE", "EXECCOVEVERY", "EXECCOVSTAFF"} {
		env.e.RunRegistry[name] = func(c *cmdCtx, _ string) (*user.User, string, error) {
			calls[name]++
			return c.currentUser, "", nil
		}
	}
	m.menu("MAIN", MenuRecord{}, "MAIN-SCREEN\r\n",
		CommandRecord{Keys: "//", Command: "RUN:EXECCOVONCE"},
		CommandRecord{Keys: "~~", Command: "RUN:EXECCOVEVERY"},
		CommandRecord{Keys: "//", Command: "RUN:EXECCOVSTAFF", ACS: "S255"},
		CommandRecord{Keys: "Q", Command: "LOGOFF"})

	tracker := types.AutoRunTracker{}
	r := execcovRun(env, execcovCall{user: env.caller, start: "MAIN", input: "\r\rQ\r", tracker: tracker})
	if r.next != "LOGOFF" || execcovCount(r, "MAIN-SCREEN") != 3 {
		t.Fatalf("next=%q output:\n%s", r.next, r.text())
	}
	if calls["EXECCOVONCE"] != 1 || calls["EXECCOVEVERY"] != 3 || calls["EXECCOVSTAFF"] != 0 {
		t.Errorf("autorun calls = %v, want once=1 every=3 staff=0", calls)
	}
	if !tracker["MAIN:RUN:EXECCOVONCE"] || tracker["MAIN:RUN:EXECCOVSTAFF"] || tracker["MAIN:RUN:EXECCOVEVERY"] {
		t.Errorf("tracker = %v, want only the run-once command that ran", tracker)
	}

	// The same tracker on a later visit keeps the run-once command quiet.
	execcovRun(env, execcovCall{user: env.caller, start: "MAIN", input: "Q\r", tracker: tracker})
	if calls["EXECCOVONCE"] != 1 || calls["EXECCOVEVERY"] != 4 {
		t.Errorf("after second visit calls = %v, want once=1 every=4", calls)
	}
}

// What an autorun command returns steers the run: GOTO enters another menu
// without drawing this one, LOGOFF and an error end the run, and a user it
// hands back replaces the session's.
func TestExeccovRun_AutoRunOutcomes(t *testing.T) {
	t.Run("goto", func(t *testing.T) {
		env, m := execcovRunEnv(t)
		m.menu("MAIN", MenuRecord{}, "MAIN-SCREEN\r\n",
			CommandRecord{Keys: "~~", Command: "GOTO:SUB"},
			CommandRecord{Keys: "~~", Command: "LOGOFF"})
		r := execcovRun(env, execcovCall{user: env.caller, start: "MAIN", input: "^P\r"})
		// ^P in SUB goes back to MAIN, whose autorun bounces to SUB again.
		if r.has("MAIN-SCREEN") || execcovCount(r, "SUB-SCREEN") != 2 {
			t.Errorf("output:\n%s", r.text())
		}
	})
	t.Run("logoff", func(t *testing.T) {
		env, m := execcovRunEnv(t)
		m.menu("MAIN", MenuRecord{}, "MAIN-SCREEN\r\n", CommandRecord{Keys: "~~", Command: "LOGOFF"})
		r := execcovRun(env, execcovCall{user: env.caller, start: "MAIN", input: "A\r"})
		if r.err != nil || r.next != "LOGOFF" || execcovHandle(r.user) != "Caller" || r.has("MAIN-SCREEN") {
			t.Errorf("next=%q user=%s err=%v output:\n%s", r.next, execcovHandle(r.user), r.err, r.text())
		}
	})
	t.Run("error", func(t *testing.T) {
		env, m := execcovRunEnv(t)
		boom := errors.New("boom")
		env.e.RunRegistry["EXECCOVFAIL"] = func(*cmdCtx, string) (*user.User, string, error) {
			return nil, "", boom
		}
		execcovStrings(env, func(s *config.StringsConfig) { s.ExecRunCommandError = "\r\nRUNERR %s: %v\r\n" })
		m.menu("MAIN", MenuRecord{}, "MAIN-SCREEN\r\n", CommandRecord{Keys: "//", Command: "RUN:EXECCOVFAIL"})
		r := execcovRun(env, execcovCall{user: env.caller, start: "MAIN", input: "Q\r"})
		if !errors.Is(r.err, boom) || r.next != "" || r.user != nil {
			t.Errorf("next=%q user=%s err=%v", r.next, execcovHandle(r.user), r.err)
		}
		if !r.has("RUNERR EXECCOVFAIL: boom") || r.has("MAIN-SCREEN") {
			t.Errorf("output:\n%s", r.text())
		}
	})
	t.Run("user replaced", func(t *testing.T) {
		env, m := execcovRunEnv(t)
		env.e.RunRegistry["EXECCOVBUMP"] = func(c *cmdCtx, _ string) (*user.User, string, error) {
			cp := *c.currentUser
			cp.TimesCalled = 777
			return &cp, "", nil
		}
		m.menu("MAIN", MenuRecord{}, "MAIN-SCREEN\r\n",
			CommandRecord{Keys: "//", Command: "RUN:EXECCOVBUMP"},
			CommandRecord{Keys: "Q", Command: "LOGOFF"})
		r := execcovRun(env, execcovCall{user: env.caller, start: "MAIN", input: "Q\r"})
		if r.next != "LOGOFF" || r.user == nil || r.user.TimesCalled != 777 {
			t.Errorf("next=%q user=%+v, want the handler's copy", r.next, r.user)
		}
	})
}

// execcovDispatchEnv adds a menu, CMDS, whose keys each reach a different
// kind of command result. Arguments given to RUN:EXECCOVARGS land in *args.
func execcovDispatchEnv(t *testing.T) (env *menuEnv, m *execcovMenus, args *[]string) {
	t.Helper()
	env, m = execcovRunEnv(t)
	args = &[]string{}
	reg := env.e.RunRegistry
	reg["EXECCOVARGS"] = func(c *cmdCtx, a string) (*user.User, string, error) {
		*args = append(*args, a)
		return c.currentUser, "", nil
	}
	reg["EXECCOVGOTO"] = func(c *cmdCtx, _ string) (*user.User, string, error) {
		return c.currentUser, "GOTO:sub", nil
	}
	reg["EXECCOVLOGOFF"] = func(*cmdCtx, string) (*user.User, string, error) { return nil, "LOGOFF", nil }
	reg["EXECCOVFAIL"] = func(c *cmdCtx, _ string) (*user.User, string, error) {
		return c.currentUser, "", errors.New("boom")
	}
	reg["EXECCOVEOF"] = func(c *cmdCtx, _ string) (*user.User, string, error) { return c.currentUser, "", io.EOF }
	reg["EXECCOVIDLE"] = func(c *cmdCtx, _ string) (*user.User, string, error) {
		return c.currentUser, "", editor.ErrIdleTimeout
	}
	m.menu("CMDS", MenuRecord{}, "CMDS-SCREEN\r\n",
		CommandRecord{Keys: "W X", Command: "RUN:execcovargs one two"},
		CommandRecord{Keys: "##", Command: "RUN:EXECCOVARGS"},
		CommandRecord{Keys: "J", Command: "RUN:EXECCOVGOTO"},
		CommandRecord{Keys: "L", Command: "RUN:EXECCOVLOGOFF"},
		CommandRecord{Keys: "E", Command: "RUN:EXECCOVFAIL"},
		CommandRecord{Keys: "D", Command: "RUN:EXECCOVEOF"},
		CommandRecord{Keys: "I", Command: "RUN:EXECCOVIDLE"},
		CommandRecord{Keys: "N", Command: "RUN:NOSUCHTHING"},
		CommandRecord{Keys: "B", Command: "BOGUS:THING"},
		CommandRecord{Keys: "O", Command: "DOOR:Tetris"},
		CommandRecord{Keys: "S", Command: "RUN:EXECCOVARGS staff", ACS: "S255"},
		CommandRecord{Keys: "Q", Command: "LOGOFF"})
	execcovStrings(env, func(s *config.StringsConfig) {
		s.ExecRunCommandError = "\r\nRUNERR %s: %v\r\n"
		s.ExecRunCommandNotFound = "\r\nNOTFOUND %s\r\n"
		s.ExecRunDoorError = "\r\nDOORERR %s: %v\r\n"
		s.IdleTimeout = "\r\nIDLE-TOO-LONG\r\n"
		s.ExecGoodbye = "\r\nBYE-FOR-NOW\r\n"
	})
	return env, m, args
}

// RUN: commands reach their handler with the rest of the command line as
// arguments; a ## command gets the number typed. Keys are space-separated
// alternatives, and a command the caller's level does not reach is not
// matched at all.
func TestExeccovRun_RunArguments(t *testing.T) {
	env, _, args := execcovDispatchEnv(t)

	r := execcovRun(env, execcovCall{user: env.caller, start: "CMDS", input: "w\rX\r42\rS\rQ\r"})
	if r.next != "LOGOFF" || r.err != nil {
		t.Fatalf("next=%q err=%v", r.next, r.err)
	}
	if got := strings.Join(*args, "|"); got != "one two|one two|42" {
		t.Errorf("handler args = %q, want one two|one two|42", got)
	}
	if execcovCount(r, "UNKNOWN-CMD") != 1 {
		t.Errorf("the sysop-only S should be unknown to a caller, once:\n%s", r.text())
	}

	// The same goes for a caller who has not logged in.
	*args = nil
	r = execcovRun(env, execcovCall{start: "CMDS", input: "S\rW\r"})
	if got := strings.Join(*args, "|"); got != "one two" || execcovCount(r, "UNKNOWN-CMD") != 1 {
		t.Errorf("guest: handler args = %q, want just W's:\n%s", got, r.text())
	}

	*args = nil
	execcovRun(env, execcovCall{user: env.sysop, start: "CMDS", input: "S\rQ\r"})
	if got := strings.Join(*args, "|"); got != "staff" {
		t.Errorf("sysop S: handler args = %q, want staff", got)
	}
}

// What a matched command returns decides what happens next.
func TestExeccovRun_CommandOutcomes(t *testing.T) {
	env, m, _ := execcovDispatchEnv(t)

	t.Run("handler asks for a menu", func(t *testing.T) {
		r := execcovRun(env.sub(t), execcovCall{user: env.caller, start: "CMDS", input: "J\r"})
		if r.next != "LOGOFF" || !r.has("SUB-SCREEN") {
			t.Errorf("next=%q output:\n%s", r.next, r.text())
		}
	})
	t.Run("handler asks for logoff", func(t *testing.T) {
		r := execcovRun(env.sub(t), execcovCall{user: env.caller, start: "CMDS", input: "L\rQ\r"})
		// The handler returned no user, and that is what Run reports.
		if r.err != nil || r.next != "LOGOFF" || r.user != nil || execcovCount(r, "CMDS-SCREEN") != 1 {
			t.Errorf("next=%q user=%s err=%v", r.next, execcovHandle(r.user), r.err)
		}
	})
	t.Run("handler fails", func(t *testing.T) {
		r := execcovRun(env.sub(t), execcovCall{user: env.caller, start: "CMDS", input: "E\rQ\r"})
		if r.err == nil || r.err.Error() != "boom" || r.next != "" || execcovHandle(r.user) != "Caller" {
			t.Errorf("next=%q user=%s err=%v", r.next, execcovHandle(r.user), r.err)
		}
		if !r.has("RUNERR EXECCOVFAIL: boom") {
			t.Errorf("output:\n%s", r.text())
		}
	})
	t.Run("handler sees a disconnect", func(t *testing.T) {
		r := execcovRun(env.sub(t), execcovCall{user: env.caller, start: "CMDS", input: "D\rQ\r"})
		if r.err != nil || r.next != "LOGOFF" || r.user != nil || r.has("RUNERR") {
			t.Errorf("next=%q user=%s err=%v output:\n%s", r.next, execcovHandle(r.user), r.err, r.text())
		}
	})
	t.Run("handler times out idle", func(t *testing.T) {
		r := execcovRun(env.sub(t), execcovCall{user: env.caller, start: "CMDS", input: "I\rQ\r"})
		if r.err != nil || r.next != "LOGOFF" || r.user != nil || !r.has("IDLE-TOO-LONG") {
			t.Errorf("next=%q user=%s err=%v output:\n%s", r.next, execcovHandle(r.user), r.err, r.text())
		}
		// With a TIMEOUT.ANS in the menu set, that is shown instead.
		m.write("ansi", "TIMEOUT.ANS", "TIMEOUT-ART\r\n")
		r = execcovRun(env.sub(t), execcovCall{user: env.caller, start: "CMDS", input: "I\r"})
		if !r.has("TIMEOUT-ART") || r.has("IDLE-TOO-LONG") {
			t.Errorf("TIMEOUT.ANS not used:\n%s", r.text())
		}
	})
	t.Run("no such handler", func(t *testing.T) {
		r := execcovRun(env.sub(t), execcovCall{user: env.caller, start: "CMDS", input: "N\rQ\r"})
		if r.next != "LOGOFF" || !r.has("NOTFOUND NOSUCHTHING") || execcovCount(r, "CMDS-SCREEN") != 2 {
			t.Errorf("next=%q output:\n%s", r.next, r.text())
		}
	})
	t.Run("unrecognised command type", func(t *testing.T) {
		r := execcovRun(env.sub(t), execcovCall{user: env.caller, start: "CMDS", input: "B\rQ\r"})
		if r.next != "LOGOFF" || r.has("UNKNOWN-CMD") || execcovCount(r, "CMDS-SCREEN") != 2 {
			t.Errorf("next=%q output:\n%s", r.next, r.text())
		}
	})
}

// /G hangs up from any menu without asking. G asks first on a menu that does
// not bind it, and a menu's own G wins over the global one.
func TestExeccovRun_GlobalLogoffKeys(t *testing.T) {
	env, m, _ := execcovDispatchEnv(t)

	r := execcovRun(env, execcovCall{user: env.caller, start: "CMDS", input: "/g\rQ\r"})
	// The menu set has no GOODBYE.ANS, so the goodbye string stands in.
	if r.err != nil || r.next != "LOGOFF" || execcovHandle(r.user) != "Caller" || !r.has("BYE-FOR-NOW") {
		t.Errorf("/G: next=%q user=%s err=%v output:\n%s", r.next, execcovHandle(r.user), r.err, r.text())
	}

	r = execcovRun(env, execcovCall{user: env.caller, start: "CMDS", input: "G\rN"})
	if r.has("BYE-FOR-NOW") || execcovCount(r, "CMDS-SCREEN") != 2 {
		t.Errorf("G then No should stay on the menu:\n%s", r.text())
	}
	r = execcovRun(env, execcovCall{user: env.caller, start: "CMDS", input: "G\rY"})
	if r.next != "LOGOFF" || !r.has("BYE-FOR-NOW") || execcovCount(r, "CMDS-SCREEN") != 1 {
		t.Errorf("G then Yes: next=%q output:\n%s", r.next, r.text())
	}

	m.menu("OWNG", MenuRecord{}, "OWNG-SCREEN\r\n", CommandRecord{Keys: "G", Command: "GOTO:SUB"})
	r = execcovRun(env, execcovCall{user: env.caller, start: "OWNG", input: "G\r"})
	if !r.has("SUB-SCREEN") || r.has("BYE-FOR-NOW") {
		t.Errorf("menu's own G should win:\n%s", r.text())
	}
}

// A DOOR: command goes to the registry's door handler with the door's name
// as written, and the handler's result is treated like a RUN: handler's.
func TestExeccovRun_DoorCommand(t *testing.T) {
	env, _, _ := execcovDispatchEnv(t)
	var (
		doors []string
		next  string
		fail  error
	)
	env.e.RunRegistry["DOOR:"] = func(c *cmdCtx, name string) (*user.User, string, error) {
		doors = append(doors, name)
		return c.currentUser, next, fail
	}

	r := execcovRun(env, execcovCall{user: env.caller, start: "CMDS", input: "O\rQ\r"})
	if r.next != "LOGOFF" || len(doors) != 1 || doors[0] != "Tetris" || execcovCount(r, "CMDS-SCREEN") != 2 {
		t.Errorf("next=%q doors=%v output:\n%s", r.next, doors, r.text())
	}

	next = "LOGOFF"
	r = execcovRun(env, execcovCall{user: env.caller, start: "CMDS", input: "O\r"})
	if r.next != "LOGOFF" || execcovHandle(r.user) != "Caller" || execcovCount(r, "CMDS-SCREEN") != 1 {
		t.Errorf("door asked for logoff: next=%q user=%s", r.next, execcovHandle(r.user))
	}

	next, fail = "", errors.New("no dosemu")
	r = execcovRun(env, execcovCall{user: env.caller, start: "CMDS", input: "O\rQ\r"})
	if r.err == nil || r.err.Error() != "no dosemu" || !r.has("DOORERR Tetris: no dosemu") {
		t.Errorf("door failed: err=%v output:\n%s", r.err, r.text())
	}

	fail = io.EOF
	r = execcovRun(env, execcovCall{user: env.caller, start: "CMDS", input: "O\rQ\r"})
	if r.err != nil || r.next != "LOGOFF" || r.user != nil || r.has("DOORERR") {
		t.Errorf("disconnect in door: next=%q user=%s err=%v", r.next, execcovHandle(r.user), r.err)
	}

	// With no door handler at all the command is a no-op.
	delete(env.e.RunRegistry, "DOOR:")
	r = execcovRun(env, execcovCall{user: env.caller, start: "CMDS", input: "O\rQ\r"})
	if r.err != nil || r.next != "LOGOFF" || execcovCount(r, "CMDS-SCREEN") != 2 {
		t.Errorf("no door handler: next=%q err=%v", r.next, r.err)
	}
}

// The node's Who's Online activity follows the menu: the autorun entry's
// text while idling at the menu, the command's own while it runs. Pages
// queued for the node are delivered at the next prompt.
func TestExeccovRun_NodeActivityAndPages(t *testing.T) {
	env, m := execcovRunEnv(t)
	sess := &session.BbsSession{ID: 1, NodeID: 1}
	env.e.SessionRegistry.Register(sess)
	sess.AddPage("PAGE-FROM-NODE-2")

	activity := func() string {
		sess.Mutex.RLock()
		defer sess.Mutex.RUnlock()
		return sess.Activity
	}
	var during, before string
	env.e.RunRegistry["EXECCOVPEEK"] = func(c *cmdCtx, _ string) (*user.User, string, error) {
		during = activity()
		return c.currentUser, "", nil
	}
	env.e.RunRegistry["EXECCOVAUTO"] = func(c *cmdCtx, _ string) (*user.User, string, error) {
		before = activity()
		return c.currentUser, "", nil
	}
	m.menu("MAIN", MenuRecord{}, "MAIN-SCREEN\r\n",
		CommandRecord{Keys: "~~", Command: "RUN:EXECCOVAUTO", NodeActivity: "Lounging"},
		CommandRecord{Keys: "P", Command: "RUN:EXECCOVPEEK", NodeActivity: "Peeking"})

	r := execcovRun(env, execcovCall{user: env.caller, start: "MAIN", input: "P\r"})
	if before != "Lounging" || during != "Peeking" || activity() != "Lounging" {
		t.Errorf("activity at menu=%q, in command=%q, after=%q; want Lounging, Peeking, Lounging", before, during, activity())
	}
	if execcovCount(r, "PAGE-FROM-NODE-2") != 1 {
		t.Errorf("page should be delivered exactly once:\n%s", r.text())
	}

	// A menu with no autorun entry is known by its own name.
	execcovRun(env, execcovCall{user: env.caller, start: "SUB", input: ""})
	if activity() != "SUB" {
		t.Errorf("activity in SUB = %q, want SUB", activity())
	}
}

// A screen taller than the terminal is cut to fit, and a size the caller
// picks mid-session applies from the next screen on.
func TestExeccovRun_ScreenFitsTerminalHeight(t *testing.T) {
	env, m := execcovRunEnv(t)
	env.e.RunRegistry["EXECCOVSHRINK"] = func(c *cmdCtx, _ string) (*user.User, string, error) {
		setSessionTermSize(c.s, 80, 2)
		return c.currentUser, "", nil
	}
	m.menu("TALL", MenuRecord{}, "ROW-ONE\r\nROW-TWO\r\nROW-THREE\r\nROW-FOUR\r\n",
		CommandRecord{Keys: "S", Command: "RUN:EXECCOVSHRINK"})

	r := execcovRun(env, execcovCall{user: env.caller, start: "TALL", input: "S\r", height: 3})
	if execcovCount(r, "ROW-ONE") != 2 || execcovCount(r, "ROW-TWO") != 2 {
		t.Errorf("top rows should be drawn both times:\n%s", r.text())
	}
	if execcovCount(r, "ROW-THREE") != 1 || r.has("ROW-FOUR") {
		t.Errorf("want ROW-THREE once (3 rows, then 2) and ROW-FOUR never:\n%s", r.text())
	}
}

// Screens are CP437 on disk. A CP437 caller gets the bytes as they are and a
// UTF-8 caller gets them converted; tokens in the screen are filled in.
func TestExeccovRun_ScreenEncodingAndTokens(t *testing.T) {
	env, m := execcovRunEnv(t)
	m.menu("ART", MenuRecord{}, "\xdb\xdb ART new=|NEWUSERS area=|CA\r\n")
	m.menu("ADMIN", MenuRecord{}, "waiting={{PENDING_VALIDATIONS}}\r\n")
	env.seedUsers(&user.User{ID: 3, Handle: "Newbie", AccessLevel: 1})
	cfg := env.e.GetServerConfig()
	cfg.AllowNewUsers = false
	env.e.SetServerConfig(cfg)

	r := execcovRun(env, execcovCall{user: env.caller, start: "ART"})
	if !strings.Contains(r.raw, "██ ART new=NO area=None") {
		t.Errorf("UTF-8: %q", r.raw)
	}

	env.outputMode = ansi.OutputModeCP437
	r = execcovRun(env, execcovCall{user: env.caller, start: "ART"})
	if !strings.Contains(r.raw, "\xdb\xdb ART new=NO") {
		t.Errorf("CP437: %q", r.raw)
	}

	r = execcovRun(env, execcovCall{user: env.sysop, start: "ADMIN"})
	if !r.has("waiting=1") {
		t.Errorf("ADMIN should count the unvalidated user:\n%s", r.text())
	}
}

// execcovBar is a three-option lightbar: A, B and C down the left edge.
const execcovBar = `; X,Y,HiLitedColor,RegularColor,HotKey,ReturnValue,DisplayText
1,1,31,5,A,UNUSED,Alpha
1,2,31,5,B,UNUSED,Beta
this line is not a lightbar record
x,3,31,5,Z,UNUSED,BadCoords
1,3,31,5,C,UNUSED,Gamma
`

// A menu with a .BAR file is driven by the lightbar: arrows, Home and End
// move the bar (wrapping at either end), Enter takes the highlighted option,
// and a digit or an option's hotkey takes that option directly. Anything
// else is ignored.
func TestExeccovRun_LightbarSelection(t *testing.T) {
	env, m := execcovRunEnv(t)
	var picks []string
	env.e.RunRegistry["EXECCOVPICK"] = func(c *cmdCtx, a string) (*user.User, string, error) {
		picks = append(picks, a)
		return c.currentUser, "", nil
	}
	m.menu("BARM", MenuRecord{}, "BARM-SCREEN\r\n",
		CommandRecord{Keys: "A", Command: "RUN:EXECCOVPICK A"},
		CommandRecord{Keys: "B", Command: "RUN:EXECCOVPICK B"},
		CommandRecord{Keys: "C", Command: "RUN:EXECCOVPICK C"})
	m.write("bar", "BARM.BAR", execcovBar)

	const up, down, home, end = "\x1b[A", "\x1b[B", "\x1b[H", "\x1b[F"
	cases := []struct {
		name, input, want string
	}{
		{"enter takes the first", "\r", "A"},
		{"down", down + "\r", "B"},
		{"up wraps to the last", up + "\r", "C"},
		{"down wraps to the first", down + down + down + "\n", "A"},
		{"end", end + end + "\r", "C"},
		{"home", down + home + home + "\r", "A"},
		{"digit", "2", "B"},
		{"digit for the highlighted option", "1", "A"},
		{"hotkey, any case", "c", "C"},
		{"ignored keys", "9z\x01" + down + "\r", "B"},
		{"bar resets between picks", "3\r", "C,A"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			picks = nil
			r := execcovRun(env.sub(t), execcovCall{user: env.caller, start: "BARM", input: tc.input})
			if r.err != nil || r.next != "LOGOFF" || execcovHandle(r.user) != "Caller" {
				t.Errorf("disconnect at the bar: next=%q user=%s err=%v", r.next, execcovHandle(r.user), r.err)
			}
			if got := strings.Join(picks, ","); got != tc.want {
				t.Errorf("picked %q, want %q", got, tc.want)
			}
			if !r.has("Alpha", "Beta", "Gamma") || r.has("BadCoords") {
				t.Errorf("options drawn wrongly:\n%s", r.text())
			}
		})
	}
}

// A lightbar option whose hotkey has no command in the menu is reported as
// an unknown command rather than silently redrawn, and a .BAR with nothing
// usable in it leaves the menu taking typed commands.
func TestExeccovRun_LightbarMisconfigured(t *testing.T) {
	env, m := execcovRunEnv(t)
	m.menu("BARM", MenuRecord{}, "BARM-SCREEN\r\n", CommandRecord{Keys: "A", Command: "GOTO:SUB"})
	m.write("bar", "BARM.BAR", "1,1,31,5,A,UNUSED,Alpha\n1,2,31,5,D,UNUSED,Dangling\n")

	r := execcovRun(env, execcovCall{user: env.caller, start: "BARM", input: "2"})
	if !r.has("UNKNOWN-CMD") || execcovCount(r, "BARM-SCREEN") < 2 {
		t.Errorf("dangling hotkey:\n%s", r.text())
	}

	m.write("bar", "BARM.BAR", "; nothing here\nnot,a,record\n")
	r = execcovRun(env, execcovCall{user: env.caller, start: "BARM", input: "A\r"})
	if !r.has("SUB-SCREEN") || r.has("Alpha") {
		t.Errorf("empty .BAR should fall back to typed input:\n%s", r.text())
	}
	// Typed input it is: A alone, without Enter, selects nothing.
	r = execcovRun(env, execcovCall{user: env.caller, start: "BARM", input: "A"})
	if r.next != "LOGOFF" || r.has("SUB-SCREEN") {
		t.Errorf("A without Enter: next=%q output:\n%s", r.next, r.text())
	}
}

// With hot keys a single key is the whole command: forced by the menu for
// everyone, or chosen by the caller for themselves.
func TestExeccovRun_HotKeys(t *testing.T) {
	env, m := execcovRunEnv(t)
	m.menu("HOT", MenuRecord{ForceHotKey: true}, "HOT-SCREEN\r\n",
		CommandRecord{Keys: "A", Command: "GOTO:SUB"},
		CommandRecord{Keys: "Q", Command: "LOGOFF"})

	r := execcovRun(env, execcovCall{user: env.caller, start: "HOT", input: "a"})
	if !r.has("SUB-SCREEN") {
		t.Errorf("forced hot key A should reach SUB without Enter:\n%s", r.text())
	}

	// MAIN does not force them; this caller has them on.
	env.seedUsers(&user.User{ID: 3, Handle: "Quick", AccessLevel: 10, Validated: true, HotKeys: true})
	hot, _ := env.um.GetUserByID(3)
	r = execcovRun(env, execcovCall{user: hot, start: "MAIN", input: "a"})
	if !r.has("SUB-SCREEN") {
		t.Errorf("caller's hot key A should reach SUB without Enter:\n%s", r.text())
	}
	if r = execcovRun(env, execcovCall{user: env.caller, start: "MAIN", input: "a"}); r.has("SUB-SCREEN") {
		t.Errorf("a caller without hot keys needs Enter:\n%s", r.text())
	}
	r = execcovRun(env, execcovCall{user: hot, start: "MAIN", input: "q"})
	if r.next != "LOGOFF" || execcovCount(r, "MAIN-SCREEN") != 1 {
		t.Errorf("hot key Q: next=%q output:\n%s", r.next, r.text())
	}
}

// The prompt is the menu's two prompt lines with the caller's details filled
// in; a caller who has not logged in is a guest.
func TestExeccovRun_PromptPlaceholders(t *testing.T) {
	env, m := execcovRunEnv(t)
	rec := MenuRecord{
		UsePrompt: true,
		Prompt1:   "[|MN] |UH lvl=|LEVEL node=|NODE",
		Prompt2:   "TL=|TL CC=|CC/|CCN FC=|FC/|FCN new=|NEWUSERS >",
	}
	m.menu("PROMPTM", rec, "PROMPTM-SCREEN\r\n")
	conf := env.e.ConferenceMgr.ListConferences()
	if len(conf) == 0 {
		t.Fatal("shipped config has no conferences")
	}
	env.seedUsers(&user.User{
		ID: 3, Handle: "Roamer", AccessLevel: 20, Validated: true,
		CurrentMsgConferenceID: conf[0].ID, CurrentMsgConferenceTag: "MSGTAG",
		CurrentFileConferenceID: conf[0].ID, CurrentFileConferenceTag: "FILETAG",
	})
	roamer, _ := env.um.GetUserByID(3)

	r := execcovRun(env, execcovCall{user: env.caller, start: "PROMPTM"})
	if !r.has("[PROMPTM] Caller lvl=10 node=1", "CC=None/None FC=None/None new=YES >") {
		t.Errorf("caller prompt:\n%s", r.text())
	}
	if !regexp.MustCompile(`TL=(59|60) `).MatchString(r.text()) {
		t.Errorf("60-minute caller should have 59 or 60 left:\n%s", r.text())
	}

	r = execcovRun(env, execcovCall{user: roamer, start: "PROMPTM"})
	want := "TL=Unlimited CC=MSGTAG/" + conf[0].Name + " FC=FILETAG/" + conf[0].Name
	if !r.has("[PROMPTM] Roamer lvl=20", want) {
		t.Errorf("roamer prompt, want %q:\n%s", want, r.text())
	}

	r = execcovRun(env, execcovCall{start: "PROMPTM"})
	if r.next != "LOGOFF" || r.user != nil || !r.has("[PROMPTM] Guest lvl=0", "TL=N/A") {
		t.Errorf("guest prompt: next=%q\n%s", r.next, r.text())
	}
}

// A menu with no prompt text gets the default prompt, and none at all when
// that is empty too or the menu turns prompts off.
func TestExeccovRun_PromptDefaults(t *testing.T) {
	env, m := execcovRunEnv(t)
	m.menu("BARE", MenuRecord{UsePrompt: true, Prompt1: "  "}, "BARE-SCREEN\r\n",
		CommandRecord{Keys: "Q", Command: "LOGOFF"})
	m.menu("QUIET", MenuRecord{UsePrompt: false, Prompt1: "NEVER-SHOWN"}, "QUIET-SCREEN\r\n")
	execcovStrings(env, func(s *config.StringsConfig) { s.DefPrompt = "DEFAULT-PROMPT |MN>" })

	r := execcovRun(env, execcovCall{user: env.caller, start: "BARE"})
	if !r.has("DEFAULT-PROMPT BARE>") {
		t.Errorf("default prompt missing:\n%s", r.text())
	}
	if r = execcovRun(env, execcovCall{user: env.caller, start: "QUIET"}); r.has("NEVER-SHOWN") || r.has("DEFAULT-PROMPT") {
		t.Errorf("USEPROMPT off but a prompt was shown:\n%s", r.text())
	}

	// A prompt is CP437 like the screens: converted for a UTF-8 caller, raw
	// for a CP437 one.
	execcovStrings(env, func(s *config.StringsConfig) { s.DefPrompt = "\xfe GO>" })
	if r = execcovRun(env, execcovCall{user: env.caller, start: "BARE"}); !strings.Contains(r.raw, "■ GO>") {
		t.Errorf("UTF-8 prompt: %q", r.raw)
	}
	env.outputMode = ansi.OutputModeCP437
	if r = execcovRun(env, execcovCall{user: env.caller, start: "BARE"}); !strings.Contains(r.raw, "\xfe GO>") {
		t.Errorf("CP437 prompt: %q", r.raw)
	}
	env.outputMode = ansi.OutputModeUTF8

	execcovStrings(env, func(s *config.StringsConfig) { s.DefPrompt = "" })
	r = execcovRun(env, execcovCall{user: env.caller, start: "BARE", input: "Q\r"})
	// Commands are still read with nothing to prompt with.
	if r.next != "LOGOFF" || execcovCount(r, "BARE-SCREEN") != 1 || r.has("DEFAULT-PROMPT") {
		t.Errorf("no prompt configured: next=%q output:\n%s", r.next, r.text())
	}
}

// On MAIN a prompt line carrying |PV, the count of accounts awaiting
// validation, is shown only to staff and only while someone is waiting.
func TestExeccovRun_MainPromptPendingValidations(t *testing.T) {
	env, m := execcovRunEnv(t)
	m.menu("MAIN", MenuRecord{UsePrompt: true, Prompt1: "WAITING=|PV", Prompt2: "CMD>"}, "MAIN-SCREEN\r\n")

	if r := execcovRun(env, execcovCall{user: env.sysop, start: "MAIN"}); r.has("WAITING=") || !r.has("CMD>") {
		t.Errorf("nobody waiting, sysop:\n%s", r.text())
	}

	env.seedUsers(&user.User{ID: 3, Handle: "Newbie", AccessLevel: 1})
	if r := execcovRun(env, execcovCall{user: env.sysop, start: "MAIN"}); !r.has("WAITING=1", "CMD>") {
		t.Errorf("one waiting, sysop:\n%s", r.text())
	}
	if r := execcovRun(env, execcovCall{user: env.caller, start: "MAIN"}); r.has("WAITING=") || !r.has("CMD>") {
		t.Errorf("one waiting, caller:\n%s", r.text())
	}
}
