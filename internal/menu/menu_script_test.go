//go:build !windows

package menu

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

// menuScriptSrc prints what v3.menu holds, then answers as its arguments
// say: goto (a menu), throw (answers, then fails), silent (no answer), or
// the first listed command's key.
const menuScriptSrc = `var m = v3.menu;
v3.console.print("[menu " + [m.name, m.title, v3.prompt.kind, v3.args.join(",")].join("/") + "]");
v3.console.print("[items " + m.items.map(function (i) { return i.key + "=" + i.label; }).join(",") + "]");
v3.console.print("[keys " + m.shortcuts.join(",") + "]");
v3.console.print("[expand " + m.expand("|UH on |CC") + "]");
var a = v3.args;
if (a.indexOf("goto") >= 0) { v3.prompt.answer("GOTO:WHOSON"); }
else if (a.indexOf("throw") >= 0) { v3.prompt.answer("W"); throw new Error("boom"); }
else if (a.indexOf("silent") < 0) { v3.prompt.answer(m.items[0].key); }`

// runMenuScript runs the fixture's MAIN menu through runMenuScript with a
// DRAW door registered, and returns what the loop got back and the output.
func (f *stringHookFixture) runMenuScript(t *testing.T, rec *MenuRecord, commands []CommandRecord) (st *runLoopState, line string, act loopAction, handled bool, out string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "menu.js"), []byte(menuScriptSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	doors := f.e.DoorRegistry()
	doors["DRAW"] = config.DoorConfig{Type: "v3_script", Script: "menu.js", WorkingDirectory: dir, Args: []string{"door"}}
	doors["SYSOPDRAW"] = config.DoorConfig{Type: "v3_script", Script: "menu.js", WorkingDirectory: dir, MinAccessLevel: 200}
	f.e.SetDoorRegistry(doors)

	terminal := newDoorcovTerminal(f.s.doorcovSession)
	defer resetSessionIH(f.s)
	st = &runLoopState{e: f.e, s: f.s, terminal: terminal, nodeNumber: 3, sessionStartTime: time.Now(),
		outputMode: ansi.OutputModeUTF8, termWidth: 80, termHeight: 24,
		currentMenuName: "MAIN", currentUser: f.bbs.User}
	type result struct {
		line    string
		act     loopAction
		err     error
		handled bool
	}
	done := make(chan result, 1)
	go func() {
		l, a, err, h := st.runMenuScript(rec, commands)
		done <- result{l, a, err, h}
	}()
	select {
	case r := <-done:
		if r.err != nil {
			t.Fatalf("runMenuScript: %v; output:\n%s", r.err, f.term())
		}
		return st, r.line, r.act, r.handled, f.term()
	case <-time.After(handlerWait):
		t.Fatalf("runMenuScript still running after %v; output:\n%s", handlerWait, f.term())
		return nil, "", 0, false, ""
	}
}

func TestMenuScript(t *testing.T) {
	// The fixture's caller is level 50.
	commands := []CommandRecord{
		{Keys: "//", Command: "RUN:X", NodeActivity: "Main Menu"},
		{Keys: "W", Command: "RUN:WHO", NodeActivity: "Who's Online", Label: "Who"},
		{Keys: "G /G", Command: "LOGOFF", Hidden: true, Label: "Bye"},
		{Keys: "S", Command: "GOTO:ADMIN", ACS: "S200", Label: "Sysop"},
		{Keys: "X", Command: "RUN:X", NodeActivity: "Extras"},
	}

	t.Run("script sees the menu and its answer is the command line", func(t *testing.T) {
		f := newStringHookFixture(t, "")
		_, line, act, handled, out := f.runMenuScript(t, &MenuRecord{Title: "Main Menu", Script: "draw blue"}, commands)
		if !handled || act != loopFallthrough || line != "W" {
			t.Errorf("handled=%v act=%v line=%q", handled, act, line)
		}
		// expand fills in the prompt's placeholder codes for the caller.
		for _, want := range []string{"[menu MAIN/Main Menu/menu/door,blue]", "[items W=Who]", "[keys W,G,/G,X]", "[expand Neo on None]"} {
			if !strings.Contains(out, want) {
				t.Errorf("output lacks %q: %q", want, out)
			}
		}
	})

	t.Run("without labels every visible command is listed by activity", func(t *testing.T) {
		f := newStringHookFixture(t, "")
		plain := []CommandRecord{
			{Keys: "W", NodeActivity: "Who's Online"},
			{Keys: "X", NodeActivity: "Extras"},
			{Keys: "G", Hidden: true},
		}
		_, _, _, _, out := f.runMenuScript(t, &MenuRecord{Script: "DRAW"}, plain)
		if !strings.Contains(out, "[items W=Who's Online,X=Extras]") {
			t.Errorf("output=%q", out)
		}
	})

	t.Run("menu set's script draws a menu that names none", func(t *testing.T) {
		f := newStringHookFixture(t, "")
		f.e.SetTheme(config.ThemeConfig{MenuScript: "DRAW goto"})
		st, _, act, handled, _ := f.runMenuScript(t, &MenuRecord{}, commands)
		if !handled || act != loopContinue || st.currentMenuName != "WHOSON" || st.previousMenuName != "MAIN" {
			t.Errorf("handled=%v act=%v current=%q previous=%q", handled, act, st.currentMenuName, st.previousMenuName)
		}
	})

	for name, tc := range map[string]struct {
		script, theme string
		ran           bool
	}{
		"NONE turns the menu set's script off": {script: "NONE", theme: "DRAW"},
		"no script":                            {},
		"no such door":                         {script: "NOPE"},
		"door that is not a script":            {script: "NATIVE"},
		"script file missing":                  {script: "GONE"},
		"door the caller may not use":          {script: "SYSOPDRAW"},
		"script that fails":                    {script: "DRAW throw", ran: true},
		"script that does not answer":          {script: "DRAW silent", ran: true},
	} {
		t.Run(name+" leaves the menu to its screen", func(t *testing.T) {
			f := newStringHookFixture(t, "")
			f.e.SetTheme(config.ThemeConfig{MenuScript: tc.theme})
			st, line, act, handled, out := f.runMenuScript(t, &MenuRecord{Script: tc.script}, commands)
			if handled || act != loopFallthrough || line != "" || st.currentMenuName != "MAIN" {
				t.Errorf("handled=%v act=%v line=%q current=%q", handled, act, line, st.currentMenuName)
			}
			if ran := strings.Contains(out, "[menu "); ran != tc.ran {
				t.Errorf("script ran=%v, want %v; output=%q", ran, tc.ran, out)
			}
		})
	}
}

// The shortcuts a script is given are the keys matchCommand accepts: split on
// spaces only, in the case the command file has them.
func TestMenuViewShortcutsAreTheKeysMatchCommandAccepts(t *testing.T) {
	st := &runLoopState{currentMenuName: "TEST"}
	commands := []CommandRecord{
		{Keys: "x", Command: "GOTO:X"},
		{Keys: "A\tB", Command: "GOTO:AB"},
		{Keys: "M  MSG", Command: "GOTO:M"},
	}
	view := st.menuView(&MenuRecord{}, commands)
	for _, key := range view.Shortcuts {
		if _, _, ok := matchCommand(commands, key, func(string, string) bool { return true }); !ok {
			t.Errorf("shortcut %q runs no command", key)
		}
	}
	if got, want := strings.Join(view.Shortcuts, ","), "x,A\tB,M,MSG"; got != want {
		t.Errorf("shortcuts %q, want %q", got, want)
	}
}

// unmatchedScriptSrc counts its runs in v3.data and answers ZZZ, a key no
// menu here has, until its third run, then Q, so that a loop of runs ends.
const unmatchedScriptSrc = `var n = (v3.data.get("runs") || 0) + 1;
v3.data.set("runs", n);
v3.console.print("[run]");
v3.prompt.answer(n < 3 ? "ZZZ" : "Q");`

// A script answering a key the menu does not have, where that would bring
// the same menu straight back, runs once for the visit: the menu then shows
// its own screen and prompt, where the caller's Q logs off. This holds for a
// script named as a door and for one in the theme's scripts/ directory. A
// FALLBACK to another menu still takes the caller there.
func TestScriptedMenuUnmatchedAnswer(t *testing.T) {
	cases := []struct {
		name, script, fallback string
		wantScreen             string
	}{
		{"door, no fallback", "DRAW", "", "[art]"},
		{"door, fallback to itself", "DRAW", "main", "[art]"},
		{"theme script, no fallback", "loop.js", "", "[art]"},
		{"theme script, fallback to itself", "loop.js", "MAIN", "[art]"},
		{"door, fallback to another menu", "DRAW", "SUB", "[sub]"},
		{"theme script, fallback to another menu", "loop.js", "SUB", "[sub]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newMenuEnv(t)
			env.sessionContext = true
			dir := filepath.Join(t.TempDir(), "scripts")
			writeThemeFile(t, []byte(unmatchedScriptSrc), dir, "draw.js")
			env.e.SetDoorRegistry(map[string]config.DoorConfig{
				"DRAW": {Type: "v3_script", Script: "draw.js", WorkingDirectory: dir},
			})
			m := execcovNewMenus(t, env)
			writeThemeFile(t, []byte(unmatchedScriptSrc), m.dir, "scripts", "loop.js")
			m.menu("MAIN", MenuRecord{Script: tc.script, Fallback: tc.fallback, UsePrompt: true, Prompt1: "[prompt]"}, "[art]",
				CommandRecord{Keys: "Q", Command: "LOGOFF"})
			m.menu("SUB", MenuRecord{}, "[sub]", CommandRecord{Keys: "Q", Command: "LOGOFF"})
			r := execcovRun(env, execcovCall{user: env.caller, start: "MAIN", input: "Q\r"})
			if n := strings.Count(r.text(), "[run]"); n != 1 || !r.has(tc.wantScreen) || r.next != "LOGOFF" {
				t.Errorf("script ran %d times, next=%q\n%s", n, r.next, r.text())
			}
			if tc.wantScreen == "[art]" && !r.has("[prompt]") {
				t.Errorf("menu prompt not shown\n%s", r.text())
			}
		})
	}
}
