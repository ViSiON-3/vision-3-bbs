//go:build !windows

package menu

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"golang.org/x/term"
)

// scriptedScreenCtx is a caller on a menu set whose menu file name names the
// PICK door, a v3_script door running script; an empty script leaves the
// door's file missing. keys are the caller's keystrokes.
func scriptedScreenCtx(t *testing.T, name, script, keys string) (*cmdCtx, *doorcovSession, *user.UserMgr) {
	t.Helper()
	um, u := newUserConfigTestUser(t)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "mnu"), 0o755); err != nil {
		t.Fatal(err)
	}
	if script != "" {
		if err := os.WriteFile(filepath.Join(root, "picker.js"), []byte(script), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	b, _ := json.Marshal(MenuRecord{Title: "Themed", Script: "PICK dark"})
	if err := os.WriteFile(filepath.Join(root, "mnu", name+".MNU"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	e := konfigTestExecutor(t)
	e.MenuSetPath = root
	doors := e.DoorRegistry()
	if doors == nil {
		doors = map[string]config.DoorConfig{}
	}
	doors["PICK"] = config.DoorConfig{Type: "v3_script", Script: "picker.js", WorkingDirectory: root}
	e.SetDoorRegistry(doors)
	ds := newDoorcovSession()
	ds.send(keys)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	s := hookSession{doorcovSession: ds, ctx: &hookCtx{Context: ctx}}
	t.Cleanup(func() { resetSessionIH(s); ds.eof() })
	return &cmdCtx{e: e, s: s, terminal: term.NewTerminal(s, ""),
		userManager: um, currentUser: u, nodeNumber: 1, sessionStartTime: time.Now(),
		outputMode: ansi.OutputModeUTF8, termWidth: 80, termHeight: 24}, ds, um
}

// konfigPickerSrc prints each setting's key and value and the view's other
// members, then answers with the caller's next key.
const konfigPickerSrc = `var m = v3.menu;
v3.console.write("[" + [m.name, m.title, v3.prompt.kind, v3.args.join(","), m.selected].join("/") + "]");
m.items.forEach(function (it) { v3.console.write(it.key + ":" + it.value + ";"); });
v3.console.write("[notice " + m.notice + "]");
v3.prompt.answer(v3.console.getkey());`

func TestKonfigScriptPicksSettingsAndTheirEditorsSave(t *testing.T) {
	// D toggles Hot Keys; H edits the location, typed ahead of the editor.
	c, ds, um := scriptedScreenCtx(t, "KONFIG", konfigPickerSrc, "dH\x15North |13\rQ")
	got, next, err := runUserKonfig(c, "MAIN")
	if err != nil {
		t.Fatal(err)
	}
	out := ds.output()
	if !got.HotKeys || !reloadUser(t, um).HotKeys || reloadUser(t, um).GroupLocation != "North |13" {
		t.Fatalf("edits were lost: hotkeys=%v location=%q", got.HotKeys, got.GroupLocation)
	}
	if next != "GOTO:MAIN" {
		t.Fatalf("next = %q", next)
	}
	for _, want := range []string{"[KONFIG/Themed/menu/dark/", "D:On;", "H:North |13;", "J:********;"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q: %q", want, out)
		}
	}
	if strings.Contains(out, got.PasswordHash) {
		t.Error("the password hash reached the script")
	}
}

func TestKonfigScriptThatCannotRunLeavesTheBuiltInForm(t *testing.T) {
	c, ds, _ := scriptedScreenCtx(t, "KONFIG", "", "q")
	_, next, err := runUserKonfig(c, "MAIN")
	if err != nil {
		t.Fatal(err)
	}
	if out := stripEscapes(ds.output()); next != "GOTO:MAIN" || !strings.Contains(out, "Screen Width") {
		t.Fatalf("next=%q output=%q", next, out)
	}
}

func TestTwitListScriptDrawsEntriesAndAnswersCommands(t *testing.T) {
	script := `var m = v3.menu;
v3.console.write("[" + m.name + " body " + m.body.join("|") + "]");
v3.console.write("[items " + m.items.map(function (i) { return i.key; }).join("") + "]");
v3.console.write("[hint " + m.hint + "][notice " + m.notice + "]");
v3.prompt.answer(v3.console.getkey());`
	// C clears the list (confirmed with Y and Enter), then Q leaves.
	c, ds, um := scriptedScreenCtx(t, "TWITLIST", script, "cy\rq")
	c.currentUser.TwitList = []user.TwitEntry{{Name: "Loud One"}, {Name: "Far Away", Addr: "21:1/100"}}
	// Store the two entries, so the check after Clear can only pass if
	// Clear saved the empty list.
	if err := um.UpdateUser(c.currentUser); err != nil {
		t.Fatal(err)
	}
	if n := len(reloadUser(t, um).TwitList); n != 2 {
		t.Fatalf("stored list has %d entries before Clear, want 2", n)
	}
	if _, _, err := runCfgTwitList(c, ""); err != nil {
		t.Fatal(err)
	}
	out := ds.output()
	for _, want := range []string{"[TWITLIST body  1  Loud One| 2  Far Away@21:1/100]", "[items ADCQ]", "[hint 2 of "} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q: %q", want, out)
		}
	}
	if n := len(reloadUser(t, um).TwitList); n != 0 {
		t.Errorf("list has %d entries after Clear", n)
	}
}

// A script that answers with a key the Twit List has no command for, without
// reading the caller, must not be run again in place of the built-in screen.
func TestTwitListScriptUnknownAnswerLeavesTheBuiltInScreen(t *testing.T) {
	c, ds, _ := scriptedScreenCtx(t, "TWITLIST", `v3.console.write("[drawn]"); v3.prompt.answer("Z");`, "q\r")
	done := make(chan error, 1)
	go func() {
		_, _, err := runCfgTwitList(c, "")
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("Twit List still running; script drawn %d times", strings.Count(ds.output(), "[drawn]"))
	}
	if n := strings.Count(ds.output(), "[drawn]"); n != 1 {
		t.Errorf("script ran %d times, want 1", n)
	}
}
