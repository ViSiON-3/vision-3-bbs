//go:build !windows

package menu

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// fastLoginPickerSrc shows the menu it is handed and answers with the
// caller's next key, or two for a key that starts with "/".
const fastLoginPickerSrc = `var m = v3.menu;
v3.console.write("[" + m.name + " items " + m.items.map(function (i) { return i.key; }).join(",") +
    " shortcuts " + m.shortcuts.join(",") + "]");
var k = v3.console.getkey();
if (k === "/") k += v3.console.getkey();
v3.prompt.answer(k);`

// fastLoginScriptCtx is scriptedScreenCtx for FASTLOGN, with the shipped
// set's FASTLOGN .ANS, .BAR and .CFG beside the .MNU that names the script.
func fastLoginScriptCtx(t *testing.T, script, keys string) (*cmdCtx, *doorcovSession) {
	t.Helper()
	c, ds, _ := scriptedScreenCtx(t, "FASTLOGN", script, keys)
	copyMenuFiles(t, filepath.Join("..", "..", "menus", "v3"), c.e.MenuSetPath, "FASTLOGN")
	return c, ds
}

// copyMenuFiles copies every file of menu name (ansi/NAME.ANS, cfg/NAME.CFG
// and the rest) from the menu set directory from to the one at to, leaving
// to's .MNU alone if it has one.
func copyMenuFiles(t *testing.T, from, to, name string) {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(from, "*", name+".*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatalf("no %s files in %s", name, from)
	}
	for _, p := range paths {
		sub := filepath.Base(filepath.Dir(p))
		dst := filepath.Join(to, sub, filepath.Base(p))
		if _, err := os.Stat(dst); err == nil {
			continue
		}
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dst, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// runFastLoginWithin runs Fast Logon and fails the test if it has not
// returned within ten seconds.
func runFastLoginWithin(t *testing.T, c *cmdCtx, ds *doorcovSession) (next string, err error) {
	t.Helper()
	type result struct {
		next string
		err  error
	}
	done := make(chan result, 1)
	go func() {
		_, next, err := runFastLogin(c, "")
		done <- result{next, err}
	}()
	select {
	case r := <-done:
		return r.next, r.err
	case <-time.After(10 * time.Second):
		t.Fatalf("Fast Logon still running; output:\n%s", ds.output())
		return "", nil
	}
}

// builtInFastLogin is a line of the shipped FASTLOGN.BAR, drawn only by the
// built-in screen.
const builtInFastLogin = "2. Main Menu"

func TestFastLoginScriptAnswersCommands(t *testing.T) {
	for name, tc := range map[string]struct {
		keys, next string
		err        error
	}{
		"a menu key goes to that menu":       {keys: "2", next: "GOTO:MAIN"},
		"another menu key":                   {keys: "3", next: "GOTO:FILEM"},
		"the full login continues the login": {keys: "1", next: ""},
		"G logs off":                         {keys: "g", next: "LOGOFF"},
		"/G hangs up":                        {keys: "/g", next: "LOGOFF", err: io.EOF},
	} {
		t.Run(name, func(t *testing.T) {
			c, ds := fastLoginScriptCtx(t, fastLoginPickerSrc, tc.keys)
			next, err := runFastLoginWithin(t, c, ds)
			if next != tc.next || !errors.Is(err, tc.err) {
				t.Fatalf("next=%q err=%v, want %q %v", next, err, tc.next, tc.err)
			}
			out := ds.output()
			if !strings.Contains(out, "[FASTLOGN items 1,2,3,4,G,/G shortcuts 1,2,3,4,G,/G]") {
				t.Errorf("the script was not handed the menu: %q", out)
			}
			if strings.Contains(out, builtInFastLogin) {
				t.Error("the built-in screen was drawn as well as the script")
			}
		})
	}
}

// A caller whose flags keep them to the full login is not offered the menu
// keys, and a key they may not use leaves the built-in screen to answer.
func TestFastLoginScriptRespectsCommandAccess(t *testing.T) {
	c, ds := fastLoginScriptCtx(t, fastLoginPickerSrc, "21")
	c.currentUser.Flags = "Q"
	next, err := runFastLoginWithin(t, c, ds)
	if err != nil || next != "" {
		t.Fatalf("next=%q err=%v, want the full login", next, err)
	}
	out := ds.output()
	if !strings.Contains(out, "[FASTLOGN items 1,G,/G shortcuts 1,G,/G]") {
		t.Errorf("the script was offered keys the caller may not use: %q", out)
	}
	if !strings.Contains(out, builtInFastLogin) {
		t.Error("the built-in screen was not drawn after the refused key")
	}
}

// Fast Logon draws its built-in screen when the script cannot run or gives
// an answer that is not one of its commands; a script that answers without
// reading the caller is run once.
func TestFastLoginScriptFallsBackToTheBuiltInScreen(t *testing.T) {
	for name, tc := range map[string]struct {
		script string
		drawn  int
	}{
		"the script file is missing":            {script: ""},
		"the script answers an unknown key":     {script: `v3.console.write("[drawn]"); v3.prompt.answer("Z");`, drawn: 1},
		"the script answers nothing":            {script: `v3.console.write("[drawn]");`, drawn: 1},
		"the script answers with an empty line": {script: `v3.console.write("[drawn]"); v3.prompt.answer("");`, drawn: 1},
	} {
		t.Run(name, func(t *testing.T) {
			c, ds := fastLoginScriptCtx(t, tc.script, "2")
			next, err := runFastLoginWithin(t, c, ds)
			if err != nil || next != "GOTO:MAIN" {
				t.Fatalf("next=%q err=%v, want GOTO:MAIN from the built-in screen", next, err)
			}
			out := ds.output()
			if !strings.Contains(out, builtInFastLogin) {
				t.Errorf("the built-in screen was not drawn: %q", out)
			}
			if n := strings.Count(out, "[drawn]"); n != tc.drawn {
				t.Errorf("script ran %d times, want %d", n, tc.drawn)
			}
		})
	}
}

// A caller who hangs up while the script reads their key ends the login.
func TestFastLoginScriptHangUp(t *testing.T) {
	c, ds := fastLoginScriptCtx(t, fastLoginPickerSrc, "")
	type result struct {
		u    *user.User
		next string
		err  error
	}
	done := make(chan result, 1)
	go func() {
		u, next, err := runFastLogin(c, "")
		done <- result{u, next, err}
	}()
	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(ds.output(), "[FASTLOGN items") {
		if time.Now().After(deadline) {
			t.Fatalf("the script did not start; output:\n%s", ds.output())
		}
		time.Sleep(10 * time.Millisecond)
	}
	ds.eof()
	var r result
	select {
	case r = <-done:
	case <-time.After(10 * time.Second):
		t.Fatalf("Fast Logon still running after the hang-up; output:\n%s", ds.output())
	}
	if r.u != nil || r.next != "LOGOFF" || !errors.Is(r.err, io.EOF) {
		t.Fatalf("user=%v next=%q err=%v, want no user, LOGOFF and io.EOF", r.u, r.next, r.err)
	}
	if strings.Contains(ds.output(), builtInFastLogin) {
		t.Error("the built-in screen was drawn after the caller hung up")
	}
}

// A command Fast Logon does not carry out is not offered to the script.
func TestFastLoginScriptOffersOnlyCommandsItActsOn(t *testing.T) {
	c, ds := fastLoginScriptCtx(t, fastLoginPickerSrc, "2")
	path := filepath.Join(c.e.MenuSetPath, "cfg", "FASTLOGN.CFG")
	var commands []map[string]any
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &commands); err != nil {
		t.Fatal(err)
	}
	commands = append(commands, map[string]any{"KEYS": "X", "CMD": "RUN:LISTUSERS", "ACS": "*"})
	raw, _ = json.Marshal(commands)
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if next, err := runFastLoginWithin(t, c, ds); err != nil || next != "GOTO:MAIN" {
		t.Fatalf("next=%q err=%v", next, err)
	}
	if out := ds.output(); !strings.Contains(out, "[FASTLOGN items 1,2,3,4,G,/G shortcuts 1,2,3,4,G,/G]") {
		t.Errorf("the script was offered a command Fast Logon does not carry out: %q", out)
	}
}

// A caller who goes idle in the script is told so before the login ends.
func TestFastLoginScriptIdleShowsTheNotice(t *testing.T) {
	old := sessionIdleUnit
	sessionIdleUnit = 300 * time.Millisecond
	t.Cleanup(func() { sessionIdleUnit = old })
	c, ds := fastLoginScriptCtx(t, fastLoginPickerSrc, "")
	cfg := c.e.GetServerConfig()
	cfg.SessionIdleTimeoutMinutes = 1 // one sessionIdleUnit
	cfg.CoSysOpLevel, cfg.SysOpLevel = 250, 255
	c.e.SetServerConfig(cfg)
	next, err := runFastLoginWithin(t, c, ds)
	if err != nil || next != "LOGOFF" {
		t.Fatalf("next=%q err=%v, want LOGOFF", next, err)
	}
	out := stripEscapes(ds.output())
	if !strings.Contains(out, "[FASTLOGN items") || !strings.Contains(out, "idle too long") {
		t.Errorf("output lacks the script or the idle notice: %q", out)
	}
}

// Two FASTLOGN.CFG entries with one key are offered as the entry the key
// runs: the first the caller may use.
func TestFastLoginScriptOffersTheEntryAKeyRuns(t *testing.T) {
	script := `v3.console.write("[" + v3.menu.items.map(function (i) { return i.key + "=" + i.label; }).join(",") + "]");
v3.prompt.answer("2");`
	for name, tc := range map[string]struct {
		flags, label, next string
	}{
		"the first entry is the caller's":    {flags: "", label: "2=Main Menu", next: "GOTO:MAIN"},
		"the first entry is refused to them": {flags: "Q", label: "2=Hang Up", next: "LOGOFF"},
	} {
		t.Run(name, func(t *testing.T) {
			c, ds := fastLoginScriptCtx(t, script, "")
			c.currentUser.Flags = tc.flags
			path := filepath.Join(c.e.MenuSetPath, "cfg", "FASTLOGN.CFG")
			commands := []map[string]any{
				{"KEYS": "1", "CMD": "RUN:FULL_LOGIN_SEQUENCE", "ACS": "*", "LABEL": "Full Login"},
				{"KEYS": "2", "CMD": "GOTO:MAIN", "ACS": "!FQ", "LABEL": "Main Menu"},
				{"KEYS": "2", "CMD": "LOGOFF", "ACS": "*", "LABEL": "Hang Up"},
			}
			raw, _ := json.Marshal(commands)
			if err := os.WriteFile(path, raw, 0o644); err != nil {
				t.Fatal(err)
			}
			next, err := runFastLoginWithin(t, c, ds)
			if err != nil || next != tc.next {
				t.Fatalf("next=%q err=%v, want %q", next, err, tc.next)
			}
			if out := ds.output(); !strings.Contains(out, "[1=Full Login,"+tc.label+"]") {
				t.Errorf("items = %q, want key 2 shown once as %q", out, tc.label)
			}
		})
	}
}

// A key whose winning entry changes between showing the script's view and
// the caller's answer is not run as the entry shown: owners differ.
func TestFastLoginOfferOwnersFollowAccess(t *testing.T) {
	commands := []CommandRecord{
		{Keys: "2", Command: "GOTO:MAIN", ACS: "DAYTIME"},
		{Keys: "2 X", Command: "LOGOFF", ACS: "*"},
		{Keys: "L", Command: "RUN:LISTUSERS", ACS: "*"},
	}
	day := func(acs string) bool { return true }
	night := func(acs string) bool { return acs != "DAYTIME" }

	offered, owners := fastLoginOffer(commands, day)
	if len(offered) != 2 || offered[0].Keys != "2" || offered[1].Keys != "X" {
		t.Fatalf("offered by day = %+v", offered)
	}
	if owners["2"] != 0 || owners["X"] != 1 {
		t.Fatalf("owners by day = %v", owners)
	}
	if _, ok := owners["L"]; ok {
		t.Error("a command Fast Logon does not carry out has an owner")
	}
	_, later := fastLoginOffer(commands, night)
	if later["2"] == owners["2"] {
		t.Error("key 2 kept its owner after the first entry's ACS stopped passing")
	}
	if later["X"] != owners["X"] {
		t.Error("key X changed owner with no change to its entry")
	}
}
