//go:build !windows

package menu

import (
	"strings"
	"testing"
)

// A menu the caller goes to whose screen is missing, and which has no script
// to draw it, costs the caller that screen and not the call: they see
// the error line, are asked to press Enter, and are back on the menu they came
// from, still connected.
func TestRunMissingScreenReturnsToPreviousMenu(t *testing.T) {
	for _, c := range []struct {
		name   string
		target func(t *testing.T, m *execcovMenus)
	}{
		{"menu without its screen", func(t *testing.T, m *execcovMenus) {
			bareMenu(t, m, "NOART", MenuRecord{})
		}},
		{"menu the theme lacks altogether", func(t *testing.T, m *execcovMenus) {}},
		{"menu without its screen whose script cannot run", func(t *testing.T, m *execcovMenus) {
			bareMenu(t, m, "NOART", MenuRecord{Script: "GONE"})
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			env, m := scriptScreenEnv(t)
			m.menu("HOME", MenuRecord{}, "HOME-SCREEN\r\n",
				CommandRecord{Keys: "G", Command: "GOTO:NOART"},
				CommandRecord{Keys: "Q", Command: "LOGOFF"})
			c.target(t, m)
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
	env, m := scriptScreenEnv(t)
	m.menu("HOME", MenuRecord{}, "HOME-SCREEN\r\n",
		CommandRecord{Keys: "G", Command: "GOTO:NOART"},
		CommandRecord{Keys: "Q", Command: "LOGOFF"})
	bareMenu(t, m, "NOART", MenuRecord{})
	r := execcovRun(env, execcovCall{user: env.caller, start: "HOME", input: "G\r"})
	if r.err == nil || !strings.Contains(r.err.Error(), "NOART.ANS") {
		t.Fatalf("err=%v, want the NOART.ANS error; output:\n%s", r.err, r.text())
	}
}
