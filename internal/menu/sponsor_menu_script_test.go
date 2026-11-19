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
	"golang.org/x/term"
)

// sponsorScriptCtx is sponsorNewEnv's caller on a menu set whose
// SPONSORM.MNU names the PICK door, a v3_script door running script. keys are
// the caller's keystrokes; the session stays open after them, as a script
// door needs.
func sponsorScriptCtx(t *testing.T, script, keys string) (*cmdCtx, *doorcovSession, *menuEnv) {
	t.Helper()
	env := sponsorNewEnv(t)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "mnu"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "picker.js"), []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(MenuRecord{Title: "Sponsor", Script: "PICK"})
	if err := os.WriteFile(filepath.Join(root, "mnu", "SPONSORM.MNU"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	env.e.MenuSetPath = root
	doors := env.e.DoorRegistry()
	if doors == nil {
		doors = map[string]config.DoorConfig{}
	}
	doors["PICK"] = config.DoorConfig{Type: "v3_script", Script: "picker.js", WorkingDirectory: root}
	env.e.SetDoorRegistry(doors)
	ds := newDoorcovSession()
	ds.send(keys)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	s := hookSession{doorcovSession: ds, ctx: &hookCtx{Context: ctx}}
	t.Cleanup(func() { resetSessionIH(s); ds.eof() })
	return &cmdCtx{e: env.e, s: s, terminal: term.NewTerminal(s, ""),
		userManager: env.um, currentUser: env.caller, nodeNumber: 1, sessionStartTime: time.Now(),
		outputMode: ansi.OutputModeUTF8, termWidth: 80, termHeight: 24}, ds, env
}

// sponsorRun runs the sponsor menu and fails the test if it does not return.
func sponsorRun(t *testing.T, c *cmdCtx, ds *doorcovSession) {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		_, _, err := runSponsorMenu(c, "")
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(handlerWait):
		t.Fatalf("sponsor menu still running after %v; output:\n%s", handlerWait, ds.output())
	}
}

// TestSponsorMenuScript has SPONSORM.MNU's script draw the sponsor menu.
func TestSponsorMenuScript(t *testing.T) {
	t.Run("the script sees the commands and its answer runs one", func(t *testing.T) {
		// ] moves from GENERAL to THIRD, the caller's other area; Q leaves.
		c, ds, env := sponsorScriptCtx(t, `var m = v3.menu;
v3.console.write("[" + m.name + " " + m.items.map(function (i) { return i.key + "=" + i.label; }).join(",") + "]");
v3.prompt.answer(v3.console.getkey());`, "]q")
		sponsorRun(t, c, ds)
		out := ds.output()
		for _, want := range []string{"[SPONSORM E=Edit GENERAL,P=Positions,[=Prev Area,]=Next Area,Q=Back]", "E=Edit THIRD"} {
			if !strings.Contains(out, want) {
				t.Errorf("output lacks %q: %q", want, out)
			}
		}
		if env.caller.CurrentMessageAreaID != 3 {
			t.Errorf("caller is in area %d, want 3", env.caller.CurrentMessageAreaID)
		}
	})

	t.Run("an answer that is not a command leaves the built-in menu", func(t *testing.T) {
		c, ds, _ := sponsorScriptCtx(t, `v3.console.write("[drawn]"); v3.prompt.answer("Z");`, "q\r")
		sponsorRun(t, c, ds)
		if n := strings.Count(ds.output(), "[drawn]"); n != 1 {
			t.Errorf("script ran %d times, want 1", n)
		}
	})
}
