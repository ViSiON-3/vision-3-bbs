package menu

import (
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// PLACEHOLDER, the handler for menu options that do nothing yet, says the
// command is unknown and leaves the caller where they were.
func TestExeccovPlaceholderCommand(t *testing.T) {
	env, _ := execcovRunEnv(t)
	r := env.runCmd("PLACEHOLDER", env.caller, "", "")
	if r.err != nil || r.next != "" || r.user != env.caller || !r.has("UNKNOWN-CMD") {
		t.Errorf("next=%q user=%s err=%v output:\n%s", r.next, execcovHandle(r.user), r.err, r.text())
	}
}

// MAINLOGOFF asks before logging off; IMMEDIATELOGOFF does not. Both show
// GOODBYE.ANS on the way out, or the goodbye string when there is no such
// screen.
func TestExeccovLogoffCommands(t *testing.T) {
	env, m := execcovRunEnv(t)
	m.write("ansi", "GOODBYE.ANS", "GOODBYE-ART\r\n")
	execcovStrings(env, func(s *config.StringsConfig) {
		s.LogOffStr = "\r\nReally leave? @"
		s.ExecGoodbye = "\r\nBYE-FOR-NOW\r\n"
	})

	r := env.runCmd("MAINLOGOFF", env.caller, "", "Y")
	if r.next != "LOGOFF" || r.user != env.caller || !r.has("Really leave?", "GOODBYE-ART") || r.has("BYE-FOR-NOW") {
		t.Errorf("yes: next=%q user=%s output:\n%s", r.next, execcovHandle(r.user), r.text())
	}
	r = env.runCmd("MAINLOGOFF", env.caller, "", "N")
	if r.next != "" || r.user != env.caller || r.has("GOODBYE-ART") {
		t.Errorf("no: next=%q user=%s output:\n%s", r.next, execcovHandle(r.user), r.text())
	}
	// Enter takes the highlighted answer, which starts on No.
	if r = env.runCmd("MAINLOGOFF", env.caller, "", "\r"); r.next != "" || r.has("GOODBYE-ART") {
		t.Errorf("enter should default to No: next=%q output:\n%s", r.next, r.text())
	}
	if r = env.runCmd("MAINLOGOFF", env.caller, "", ""); r.next != "LOGOFF" || r.user != nil || r.has("GOODBYE-ART") {
		t.Errorf("disconnect at the question: next=%q user=%s", r.next, execcovHandle(r.user))
	}

	r = env.runCmd("IMMEDIATELOGOFF", env.caller, "", "")
	if r.next != "LOGOFF" || r.user != env.caller || !r.has("GOODBYE-ART") || r.has("Really leave?") {
		t.Errorf("immediate: next=%q user=%s output:\n%s", r.next, execcovHandle(r.user), r.text())
	}

	// No GOODBYE.ANS, and no logoff question configured.
	env, _ = execcovRunEnv(t)
	execcovStrings(env, func(s *config.StringsConfig) {
		s.LogOffStr = ""
		s.ExecGoodbye = "\r\nBYE-FOR-NOW\r\n"
	})
	r = env.runCmd("MAINLOGOFF", env.caller, "", "Y")
	if r.next != "LOGOFF" || !r.has("Log off now?", "BYE-FOR-NOW") {
		t.Errorf("fallbacks: next=%q output:\n%s", r.next, r.text())
	}
}

// SHOWVERSION prints the version string and waits at the pause prompt, if
// there is one.
func TestExeccovShowVersion(t *testing.T) {
	env := newMenuEnv(t)
	execcovStrings(env, func(s *config.StringsConfig) {
		s.ExecVersionString = "|15EXECCOV BBS build"
		s.PauseString = "PAUSE-HERE"
	})

	r := env.runCmd("SHOWVERSION", env.caller, "", "\r")
	if r.err != nil || r.next != "" || r.user != nil || !r.has("EXECCOV BBS build", "PAUSE-HERE") {
		t.Errorf("next=%q user=%s err=%v output:\n%s", r.next, execcovHandle(r.user), r.err, r.text())
	}
	if !strings.HasPrefix(r.raw, ansi.ClearScreen()) || strings.Contains(r.raw, "|15") {
		t.Errorf("should clear first and expand pipe codes: %q", r.raw)
	}
	if r = env.runCmd("SHOWVERSION", env.caller, "", ""); r.next != "LOGOFF" {
		t.Errorf("disconnect at pause: next=%q, want LOGOFF", r.next)
	}

	// With no pause string there is nothing to wait at, so running out of
	// input is not seen as a disconnect.
	execcovStrings(env, func(s *config.StringsConfig) { s.PauseString = "" })
	r = env.runCmd("SHOWVERSION", env.caller, "", "")
	if r.next != "" || !r.has("EXECCOV BBS build") || r.has("PAUSE-HERE") {
		t.Errorf("no pause string: next=%q output:\n%s", r.next, r.text())
	}
}

// SHOWSTATS fills YOURSTAT.ANS from the account. A user with no time limit
// reads "Unlimited"; a CP437 caller gets the screen's bytes untouched; a
// missing screen is an error the caller is told about.
func TestExeccovShowStats_ScreenVariants(t *testing.T) {
	env, m := execcovRunEnv(t)
	execcovStrings(env, func(s *config.StringsConfig) {
		s.ExecStatsError = "\r\nSTATS-MISSING %s\r\n"
		s.PauseString = ""
	})

	r := env.runCmd("SHOWSTATS", env.caller, "", "\r")
	if r.err == nil || !strings.Contains(r.err.Error(), "failed to read YOURSTAT.ANS") || !r.has("STATS-MISSING YOURSTAT.ANS") {
		t.Errorf("no screen: err=%v output:\n%s", r.err, r.text())
	}

	m.write("ansi", "YOURSTAT.ANS", "\xb3 |UH level=|UL left=|TL note=|UN \xb3\r\n")
	env.seedUsers(&user.User{ID: 3, Handle: "Roamer", AccessLevel: 20, Validated: true, PrivateNote: "vip"})
	roamer, _ := env.um.GetUserByID(3)

	r = env.runCmd("SHOWSTATS", roamer, "", "\r")
	if r.err != nil || !strings.Contains(r.raw, "│ Roamer level=20 left=Unlimited note=vip │") {
		t.Errorf("UTF-8: err=%v raw=%q", r.err, r.raw)
	}
	// The pause string is empty, so the built-in prompt is used.
	if !r.has("to continue") {
		t.Errorf("fallback pause prompt missing:\n%s", r.text())
	}

	env.outputMode = ansi.OutputModeCP437
	r = env.runCmd("SHOWSTATS", roamer, "", "\r")
	if r.err != nil || !strings.Contains(r.raw, "\xb3 Roamer level=20 left=Unlimited note=vip \xb3") {
		t.Errorf("CP437: err=%v raw=%q", r.err, r.raw)
	}
}

// READMAIL is still a stub: it names the caller, or asks a caller who is not
// logged in to log in.
func TestExeccovReadMailPlaceholder(t *testing.T) {
	env := newMenuEnv(t)
	execcovStrings(env, func(s *config.StringsConfig) {
		s.ExecReadmailLogin = "\r\nLOGIN-TO-READ\r\n"
		s.ExecReadmailPlaceholder = "\r\nMAIL-STUB for %s\r\n"
	})

	if r := env.runCmd("READMAIL", nil, "", ""); r.err != nil || r.user != nil || !r.has("LOGIN-TO-READ") {
		t.Errorf("no user: err=%v output:\n%s", r.err, r.text())
	}
	if r := env.runCmd("READMAIL", env.caller, "", ""); r.err != nil || r.next != "" || !r.has("MAIL-STUB for Caller") {
		t.Errorf("caller: next=%q err=%v output:\n%s", r.next, r.err, r.text())
	}
}

// The DOOR: handler refuses, on the session's stderr, a caller who is not
// logged in, a door that is not configured, and a door above the caller's
// level. None of these is an error to the menu.
func TestExeccovDoorHandlerRefusals(t *testing.T) {
	env := newMenuEnv(t)
	execcovStrings(env, func(s *config.StringsConfig) {
		s.ExecDoorLogin = "\r\nLOGIN-FOR-DOORS\r\n"
		s.ExecDoorNotConfigured = "\r\nNO-SUCH-DOOR %s\r\n"
		s.DoorAccessDenied = "  "
	})
	env.e.SetDoorRegistry(map[string]config.DoorConfig{
		"VAULT": {Code: "VAULT", Name: "The Vault", Commands: []string{"/nonexistent/execcov-door"}, MinAccessLevel: 200},
	})
	door := env.e.RunRegistry["DOOR:"]

	cases := []struct {
		name string
		u    *user.User
		door string
		want string
	}{
		{"not logged in", nil, "vault", "LOGIN-FOR-DOORS"},
		{"not configured", env.caller, "Tetris", "NO-SUCH-DOOR Tetris"},
		// The configured denial text is blank, so the built-in one is used.
		{"level too low", env.caller, "vault", "Access denied to door: vault"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, stderr := execcovRunFn(env.sub(t), door, tc.u, tc.door, "")
			if r.err != nil || r.next != "" || r.user != nil {
				t.Errorf("next=%q user=%s err=%v", r.next, execcovHandle(r.user), r.err)
			}
			if got := testAnsiEscape.ReplaceAllString(stderr, ""); !strings.Contains(got, tc.want) {
				t.Errorf("stderr = %q, want %q", got, tc.want)
			}
			if r.raw != "" {
				t.Errorf("nothing should go to the session itself: %q", r.raw)
			}
		})
	}
}

// A single-instance door another node is already in is reported as busy, on
// stderr, and the caller returns to the menu.
func TestExeccovDoorHandlerBusy(t *testing.T) {
	env := newMenuEnv(t)
	execcovStrings(env, func(s *config.StringsConfig) { s.DoorBusyFormat = "" })
	env.e.SetDoorRegistry(map[string]config.DoorConfig{
		"EXECCOVBUSY": {Code: "EXECCOVBUSY", Name: "Busy Door", Commands: []string{"/nonexistent/execcov-door"}, SingleInstance: true},
	})
	if err := acquireDoorLock("EXECCOVBUSY", 2); err != nil {
		t.Fatalf("taking the door as node 2: %v", err)
	}
	t.Cleanup(func() { releaseDoorLock("EXECCOVBUSY", 2) })

	r, stderr := execcovRunFn(env, env.e.RunRegistry["DOOR:"], env.caller, "execcovbusy", "")
	if r.err != nil || r.next != "" || r.user != nil {
		t.Errorf("next=%q user=%s err=%v", r.next, execcovHandle(r.user), r.err)
	}
	if got := testAnsiEscape.ReplaceAllString(stderr, ""); !strings.Contains(got, "Door is currently in use: execcovbusy") {
		t.Errorf("stderr = %q", got)
	}
}
