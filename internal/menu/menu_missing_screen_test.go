package menu

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A menu the caller goes to whose screen file is missing costs the caller
// that screen and not the call: they see the error line, are asked to press
// Enter, and are back on the menu they came from, still connected.
func TestRunMissingScreenReturnsToPreviousMenu(t *testing.T) {
	for _, c := range []struct {
		name   string
		target func(m *execcovMenus)
	}{
		{"menu without its screen", func(m *execcovMenus) {
			m.write("mnu", "NOART.MNU", "{}")
			m.write("cfg", "NOART.CFG", "[]")
		}},
		{"menu the theme lacks altogether", func(m *execcovMenus) {}},
	} {
		t.Run(c.name, func(t *testing.T) {
			env, m := execcovRunEnv(t)
			m.menu("HOME", MenuRecord{}, "HOME-SCREEN\r\n",
				CommandRecord{Keys: "G", Command: "GOTO:NOART"},
				CommandRecord{Keys: "Q", Command: "LOGOFF"})
			c.target(m)
			r := execcovRun(env, execcovCall{user: env.caller, start: "HOME", input: "G\r\rQ\r"})
			if r.err != nil || r.next != "LOGOFF" {
				t.Fatalf("next=%q err=%v, want LOGOFF and no error; output:\n%s", r.next, r.err, r.text())
			}
			if !r.has("Error reading screen file: NOART.ANS") {
				t.Errorf("no error line; output:\n%s", r.text())
			}
			if n := execcovCount(r, "HOME-SCREEN"); n != 2 {
				t.Errorf("HOME shown %d times, want 2 (before and after); output:\n%s", n, r.text())
			}
		})
	}
}

// A caller who hangs up at the pause after the error line ends the run with
// the screen's error, as before.
func TestRunMissingScreenDisconnectAtPause(t *testing.T) {
	env, m := execcovRunEnv(t)
	m.menu("HOME", MenuRecord{}, "HOME-SCREEN\r\n",
		CommandRecord{Keys: "G", Command: "GOTO:NOART"})
	r := execcovRun(env, execcovCall{user: env.caller, start: "HOME", input: "G\r"})
	if r.err == nil || !strings.Contains(r.err.Error(), "NOART.ANS") {
		t.Fatalf("err=%v, want the NOART.ANS error; output:\n%s", r.err, r.text())
	}
}

// A logged-in caller run from LOGIN is sent to MAIN with LOGIN as the menu
// before it. If MAIN's screen is missing, going back to LOGIN would only send
// them to MAIN again, so the run ends with the error after one error line.
func TestRunMissingScreenNotBackToLogin(t *testing.T) {
	env, m := execcovRunEnv(t)
	m.menu("LOGIN", MenuRecord{}, "LOGIN-SCREEN\r\n")
	if err := os.Remove(filepath.Join(m.dir, "ansi", "MAIN.ANS")); err != nil {
		t.Fatal(err)
	}
	r := execcovRun(env, execcovCall{user: env.caller, start: "LOGIN", input: "\r\r\r"})
	if r.err == nil || !strings.Contains(r.err.Error(), "MAIN.ANS") {
		t.Fatalf("err=%v, want the MAIN.ANS error; output:\n%s", r.err, r.text())
	}
	if n := execcovCount(r, "Error reading screen file: MAIN.ANS"); n != 1 {
		t.Errorf("error line shown %d times, want 1; output:\n%s", n, r.text())
	}
}

// An every-entry autorun that sends the caller to a menu whose screen is
// missing sends them there again on the way back. The way back is not taken a
// second time before a menu has been drawn, so the run ends with the error at
// the second error line, with Enters still unread, instead of repeating until
// the caller hangs up.
func TestRunMissingScreenAutorunNoLoop(t *testing.T) {
	env, m := execcovRunEnv(t)
	m.menu("HOME", MenuRecord{}, "HOME-SCREEN\r\n",
		CommandRecord{Keys: "G", Command: "GOTO:AUTO"})
	m.menu("AUTO", MenuRecord{}, "AUTO-SCREEN\r\n",
		CommandRecord{Keys: "~~", Command: "GOTO:NOART"})
	r := execcovRun(env, execcovCall{user: env.caller, start: "HOME", input: "G\r\r\r\r"})
	if r.err == nil || !strings.Contains(r.err.Error(), "NOART.ANS") {
		t.Fatalf("err=%v, want the NOART.ANS error; output:\n%s", r.err, r.text())
	}
	if n := execcovCount(r, "Error reading screen file: NOART.ANS"); n != 2 {
		t.Errorf("error line shown %d times, want 2; output:\n%s", n, r.text())
	}
}
