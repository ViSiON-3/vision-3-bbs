//go:build !windows

package menu

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

// scriptScreenEnv is execcovRunEnv with three v3_script doors: DRAW prints
// "[drawn <menu>]" and answers Q, GONE names a script file that does not
// exist, and SYSOPDRAW is DRAW for access level 200 and up.
func scriptScreenEnv(t *testing.T) (*menuEnv, *execcovMenus) {
	t.Helper()
	env, m := execcovRunEnv(t)
	env.sessionContext = true
	dir := t.TempDir()
	src := `v3.console.print("[drawn " + v3.menu.name + "]"); v3.prompt.answer("Q");`
	if err := os.WriteFile(filepath.Join(dir, "menu.js"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	doors := env.e.DoorRegistry()
	doors["DRAW"] = config.DoorConfig{Type: "v3_script", Script: "menu.js", WorkingDirectory: dir}
	doors["GONE"] = config.DoorConfig{Type: "v3_script", Script: "missing.js", WorkingDirectory: dir}
	doors["SYSOPDRAW"] = config.DoorConfig{Type: "v3_script", Script: "menu.js", WorkingDirectory: dir, MinAccessLevel: 200}
	env.e.SetDoorRegistry(doors)
	return env, m
}

// bareMenu writes a menu whose only command is Q (LOGOFF) and that has no
// screen file.
func bareMenu(t *testing.T, m *execcovMenus, name string, rec MenuRecord) {
	t.Helper()
	m.menu(name, rec, "", CommandRecord{Keys: "Q", Command: "LOGOFF"})
	if err := os.Remove(filepath.Join(m.dir, "ansi", name+".ANS")); err != nil {
		t.Fatal(err)
	}
}

// A menu drawn by its script needs no .ANS; the screen is read only when the
// script cannot run, and then a missing one ends the run as it does for a
// menu with no script.
func TestRunScriptedMenuScreenFile(t *testing.T) {
	t.Run("script draws a menu with no screen", func(t *testing.T) {
		env, m := scriptScreenEnv(t)
		bareMenu(t, m, "BARE", MenuRecord{Script: "DRAW"})
		r := execcovRun(env, execcovCall{user: env.caller, start: "BARE"})
		if r.err != nil || r.next != "LOGOFF" {
			t.Fatalf("next=%q err=%v, want LOGOFF; output:\n%s", r.next, r.err, r.text())
		}
		if !r.has("[drawn BARE]") || r.has("Error reading screen file") {
			t.Errorf("output:\n%s", r.text())
		}
	})

	// A script that cannot run for the caller leaves the menu to its
	// screen, and with none the run ends as for PLAIN, which has no script,
	// before the menu's own checks: an ACS that refuses the caller does not
	// turn the error into a logoff.
	for _, c := range []struct{ door, acs string }{{"GONE", ""}, {"SYSOPDRAW", ""}, {"GONE", "S255"}} {
		t.Run("script door "+c.door+" ACS "+c.acs+" and no screen fails as an unscripted menu", func(t *testing.T) {
			env, m := scriptScreenEnv(t)
			bareMenu(t, m, "BROKEN", MenuRecord{Script: c.door, ACS: c.acs})
			bareMenu(t, m, "PLAIN", MenuRecord{ACS: c.acs})
			plain := execcovRun(env, execcovCall{user: env.caller, start: "PLAIN", input: "Q\r"})
			r := execcovRun(env, execcovCall{user: env.caller, start: "BROKEN", input: "Q\r"})
			if plain.err == nil || r.err == nil {
				t.Fatalf("want errors; unscripted err=%v, scripted err=%v", plain.err, r.err)
			}
			want := strings.ReplaceAll(plain.err.Error(), "PLAIN", "BROKEN")
			if r.err.Error() != want || r.next != plain.next || execcovHandle(r.user) != execcovHandle(plain.user) {
				t.Errorf("scripted: next=%q user=%s err=%q; unscripted: next=%q user=%s err=%q",
					r.next, execcovHandle(r.user), r.err, plain.next, execcovHandle(plain.user), want)
			}
			if !r.has("Error reading screen file: BROKEN.ANS") || r.has("[drawn") {
				t.Errorf("output:\n%s", r.text())
			}
		})
	}

	// Only a screen file that does not exist waits for the script; one that
	// exists but cannot be read ends the run at once.
	t.Run("screen that cannot be read fails though the script can run", func(t *testing.T) {
		env, m := scriptScreenEnv(t)
		bareMenu(t, m, "DIRSCREEN", MenuRecord{Script: "DRAW"})
		if err := os.Mkdir(filepath.Join(m.dir, "ansi", "DIRSCREEN.ANS"), 0o755); err != nil {
			t.Fatal(err)
		}
		r := execcovRun(env, execcovCall{user: env.caller, start: "DIRSCREEN"})
		if r.err == nil || !strings.Contains(r.err.Error(), "failed to read screen file DIRSCREEN.ANS") {
			t.Fatalf("err=%v, want the screen read error; output:\n%s", r.err, r.text())
		}
		if !r.has("Error reading screen file: DIRSCREEN.ANS") || r.has("[drawn") {
			t.Errorf("output:\n%s", r.text())
		}
	})

	t.Run("script draws a menu that has a screen", func(t *testing.T) {
		env, m := scriptScreenEnv(t)
		m.menu("ARTFUL", MenuRecord{Script: "DRAW"}, "ARTFUL-SCREEN\r\n", CommandRecord{Keys: "Q", Command: "LOGOFF"})
		r := execcovRun(env, execcovCall{user: env.caller, start: "ARTFUL"})
		if r.err != nil || r.next != "LOGOFF" || !r.has("[drawn ARTFUL]") || r.has("ARTFUL-SCREEN") {
			t.Errorf("next=%q err=%v output:\n%s", r.next, r.err, r.text())
		}
	})

	t.Run("menu with a screen whose script cannot run shows the screen", func(t *testing.T) {
		env, m := scriptScreenEnv(t)
		m.menu("SHOWN", MenuRecord{Script: "GONE"}, "SHOWN-SCREEN\r\n", CommandRecord{Keys: "Q", Command: "LOGOFF"})
		r := execcovRun(env, execcovCall{user: env.caller, start: "SHOWN", input: "Q\r"})
		if r.err != nil || r.next != "LOGOFF" || !r.has("SHOWN-SCREEN") {
			t.Errorf("next=%q err=%v output:\n%s", r.next, r.err, r.text())
		}
	})
}
