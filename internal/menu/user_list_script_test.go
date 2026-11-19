//go:build !windows

package menu

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// userListScriptCtx is scriptedScreenCtx for USERLIST, with user list
// templates that mark the built-in screen's output.
func userListScriptCtx(t *testing.T, script, keys string) (*cmdCtx, *doorcovSession, *user.UserMgr) {
	t.Helper()
	c, ds, um := scriptedScreenCtx(t, "USERLIST", script, keys)
	dir := filepath.Join(c.e.MenuSetPath, "templates")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"USERLIST.TOP": "[builtin top]", "USERLIST.MID": "[row |UH]\r\n", "USERLIST.BOT": "[builtin bot]",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return c, ds, um
}

// runListUsersWithin runs the user list and fails the test if it has not
// returned within ten seconds.
func runListUsersWithin(t *testing.T, c *cmdCtx, ds *doorcovSession) (next string, err error) {
	t.Helper()
	type result struct {
		next string
		err  error
	}
	done := make(chan result, 1)
	go func() {
		_, next, err := runListUsers(c, "")
		done <- result{next, err}
	}()
	select {
	case r := <-done:
		return r.next, r.err
	case <-time.After(10 * time.Second):
		t.Fatalf("user list still running; output:\n%s", ds.output())
		return "", nil
	}
}

func TestUserListScriptDrawsTheList(t *testing.T) {
	script := `var m = v3.menu;
v3.console.write("[" + [m.name, m.title, v3.prompt.kind].join("/") + "]");
v3.console.write("[items " + m.items.map(function (i) { return i.key + "=" + i.label; }).join(",") + "]");
v3.console.write("[users " + v3.users.publicList().filter(function (u) { return u.handle === "Tester"; }).length + "]");
v3.prompt.answer(v3.console.getkey());`
	c, ds, _ := userListScriptCtx(t, script, "q")
	next, err := runListUsersWithin(t, c, ds)
	if err != nil || next != "" {
		t.Fatalf("next=%q err=%v", next, err)
	}
	out := ds.output()
	for _, want := range []string{"[USERLIST/Themed/menu]", "[items Q=Back]", "[users 1]"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q: %q", want, out)
		}
	}
	if strings.Contains(out, "[builtin top]") {
		t.Error("the built-in list was drawn after the script")
	}
}

// The built-in list is drawn when the script cannot run, answers with
// something the list has no use for, or the screen is too small for it.
// A script that answers without reading the caller is run once.
func TestUserListScriptFallsBackToTheBuiltInList(t *testing.T) {
	for name, tc := range map[string]struct {
		script string
		small  bool
		drawn  int
	}{
		"the script file is missing":                {script: ""},
		"the script answers an unknown key at once": {script: `v3.console.write("[drawn]"); v3.prompt.answer("Z");`, drawn: 1},
		"the script answers nothing":                {script: `v3.console.write("[drawn]");`, drawn: 1},
		"the screen is smaller than 80x24":          {script: `v3.console.write("[drawn]"); v3.prompt.answer("Q");`, small: true},
	} {
		t.Run(name, func(t *testing.T) {
			c, ds, _ := userListScriptCtx(t, tc.script, "\r")
			if tc.small {
				c.termWidth = 79
			}
			next, err := runListUsersWithin(t, c, ds)
			if err != nil || next != "" {
				t.Fatalf("next=%q err=%v", next, err)
			}
			out := ds.output()
			if !strings.Contains(out, "[builtin top]") || !strings.Contains(out, "[row Tester") {
				t.Errorf("the built-in list was not drawn: %q", out)
			}
			if n := strings.Count(out, "[drawn]"); n != tc.drawn {
				t.Errorf("script ran %d times, want %d", n, tc.drawn)
			}
		})
	}
}

func TestUserListWithoutAMenuFileIsTheBuiltInList(t *testing.T) {
	c, ds, _ := userListScriptCtx(t, `v3.console.write("[drawn]"); v3.prompt.answer("Q");`, "\r")
	if err := os.Remove(filepath.Join(c.e.MenuSetPath, "mnu", "USERLIST.MNU")); err != nil {
		t.Fatal(err)
	}
	if _, err := runListUsersWithin(t, c, ds); err != nil {
		t.Fatal(err)
	}
	if out := ds.output(); strings.Contains(out, "[drawn]") || !strings.Contains(out, "[builtin top]") {
		t.Errorf("output = %q, want the built-in list only", out)
	}
}
