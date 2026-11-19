//go:build !windows

package menu

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

// scriptedMenuEnv is a menu set whose MAIN names script as its SCRIPT. The
// DRAW door prints the items it is given and answers "G", which goes to
// OTHER; FAIL throws before answering. The caller is level 10, so S is
// not theirs, and H is hidden.
func scriptedMenuEnv(t *testing.T, script string) *menuEnv {
	t.Helper()
	env := newMenuEnv(t)
	env.sessionContext = true
	dir := t.TempDir()
	js := `var a = v3.menu.items.map(function (i) { return i.key + "/" + i.label + "/" + i.activity; });
v3.console.print("[drawn " + a.join(",") + "]");
if (v3.args.indexOf("fail") >= 0) { throw new Error("boom"); }
v3.prompt.answer("G");`
	if err := os.WriteFile(filepath.Join(dir, "menu.js"), []byte(js), 0o644); err != nil {
		t.Fatal(err)
	}
	env.e.SetDoorRegistry(map[string]config.DoorConfig{
		"DRAW": {Type: "v3_script", Script: "menu.js", WorkingDirectory: dir},
		"FAIL": {Type: "v3_script", Script: "menu.js", WorkingDirectory: dir, Args: []string{"fail"}},
	})
	m := execcovNewMenus(t, env)
	m.menu("MAIN", MenuRecord{Script: script, UsePrompt: true, Prompt1: "[prompt]"}, "[art]",
		CommandRecord{Keys: "G", Command: "GOTO:OTHER", Label: "Go", NodeActivity: "Going"},
		CommandRecord{Keys: "S", Command: "GOTO:OTHER", ACS: "S200", NodeActivity: "Staff"},
		CommandRecord{Keys: "H", Command: "GOTO:OTHER", Hidden: true},
		CommandRecord{Keys: "Q", Command: "LOGOFF", NodeActivity: "Leaving"})
	m.menu("OTHER", MenuRecord{}, "[other]", CommandRecord{Keys: "Q", Command: "LOGOFF"})
	return env
}

// A menu whose .MNU names a script door is drawn by the script, which gets
// the commands the caller may use and answers in place of the prompt.
func TestScriptedMenu(t *testing.T) {
	env := scriptedMenuEnv(t, "DRAW")
	r := execcovRun(env, execcovCall{user: env.caller, start: "MAIN", input: "Q\r"})
	if r.has("[art]") || r.has("[prompt]") {
		t.Errorf("menu screen or prompt shown under its script:\n%s", r.text())
	}
	if !r.has("[drawn G/Go/Going,Q/Leaving/Leaving]") {
		t.Errorf("items not the caller's visible commands:\n%s", r.text())
	}
	if !r.has("[other]") || r.next != "LOGOFF" {
		t.Errorf("answer G did not go to OTHER: next=%q\n%s", r.next, r.text())
	}
}

// A missing door or a script that fails leaves the menu its screen and
// prompt, which read the caller's key as usual.
func TestScriptedMenuFallback(t *testing.T) {
	for _, script := range []string{"NOPE", "FAIL"} {
		t.Run(script, func(t *testing.T) {
			env := scriptedMenuEnv(t, script)
			r := execcovRun(env, execcovCall{user: env.caller, start: "MAIN", input: "Q\r"})
			if !r.has("[art]", "[prompt]") || r.has("[other]") || r.next != "LOGOFF" {
				t.Errorf("next=%q\n%s", r.next, r.text())
			}
		})
	}
}

// The shipped scripts/examples/menu-lightbar.js lists the menu's commands,
// moves its bar with the arrow keys and answers with the key under it.
func TestStockMenuLightbar(t *testing.T) {
	env := scriptedMenuEnv(t, "LIGHTBAR")
	scripts, err := filepath.Abs(filepath.Join("..", "..", "scripts", "examples"))
	if err != nil {
		t.Fatal(err)
	}
	env.e.SetDoorRegistry(map[string]config.DoorConfig{
		"LIGHTBAR": {Type: "v3_script", Script: "menu-lightbar.js", WorkingDirectory: scripts},
	})
	// Down, Up, Enter picks G, which goes to OTHER; Q there logs off.
	r := execcovRun(env, execcovCall{user: env.caller, start: "MAIN", input: "\x1b[B\x1b[A\rQ\r", hold: true})
	if r.has("[art]") || !r.has("G  Go", "Q  Leaving") || !r.has("[other]") || r.next != "LOGOFF" {
		t.Errorf("next=%q\n%s", r.next, r.text())
	}
}
