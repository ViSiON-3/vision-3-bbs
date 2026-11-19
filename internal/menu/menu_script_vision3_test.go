//go:build !windows

package menu

import (
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// vision3TextLogo is a run of the ViSiON/3 lettering a terminal without
// sixel is shown in place of the picture.
const vision3TextLogo = `\   \ /   /|__|/   _____/`

// runVision3Menu runs rec as menu name on the ViSiON/3 theme at theme, with
// the caller's sixel setting forced to sixel, on a width x height screen,
// and returns whether the script answered, its answer and the raw output.
func runVision3Menu(t *testing.T, theme, name string, rec *MenuRecord, commands []CommandRecord,
	input, sixel string, width, height int) (handled bool, line, raw string) {
	t.Helper()
	f := newStringHookFixture(t, input)
	f.e.MenuSetPath = theme
	f.bbs.User.PreferredSixel = sixel
	terminal := newDoorcovTerminal(f.s.doorcovSession)
	defer resetSessionIH(f.s)
	st := &runLoopState{e: f.e, s: f.s, terminal: terminal, nodeNumber: 3, sessionStartTime: time.Now(),
		outputMode: ansi.OutputModeUTF8, termWidth: width, termHeight: height,
		currentMenuName: name, currentUser: f.bbs.User}
	type result struct {
		line    string
		err     error
		handled bool
	}
	done := make(chan result, 1)
	go func() {
		l, _, err, h := st.runMenuScript(rec, commands)
		done <- result{l, err, h}
	}()
	select {
	case r := <-done:
		if r.err != nil {
			t.Fatalf("%s: %v", name, r.err)
		}
		return r.handled, r.line, f.term()
	case <-time.After(handlerWait):
		t.Fatalf("%s: script still running after %v; output:\n%s", name, handlerWait, f.term())
		return false, "", ""
	}
}

// TestVision3MainMenu runs the ViSiON/3 main menu as MAIN.MNU names it, a
// theme script of a copy of the theme: it answers keys and shortcuts, draws
// the logo as sixel when v3.gfx.sixel says the terminal draws it and as
// text when it does not, and leaves a small screen to the menu's own.
func TestVision3MainMenu(t *testing.T) {
	if vision3ThemeDir(t) == "" {
		t.Fatal("no menus/vision3 in this tree")
	}
	theme := vision3ThemeCopy(t)
	rec := themeMenuRecords(t, theme)["MAIN"]
	if rec == nil || rec.Script != "mainmenu.js" {
		t.Fatalf("MAIN.MNU = %+v, want SCRIPT mainmenu.js", rec)
	}
	commands := []CommandRecord{
		{Keys: "M", Command: "GOTO:MSGMENU", Label: "Messages"},
		{Keys: "E", Command: "GOTO:EMAILM", Label: "Email"},
		{Keys: "G", Command: "RUN:MAINLOGOFF", Label: "Log off"},
		{Keys: "/G", Command: "RUN:IMMEDIATELOGOFF"},
		{Keys: "X", Command: "GOTO:ADMIN", ACS: "S255", Label: "Sysop"},
	}
	for name, tc := range map[string]struct{ input, sixel, want string }{
		"a hot key answers with that key":          {"e", user.SixelOff, "E"},
		"an unlisted two-key shortcut answers":     {"/g", user.SixelOff, "/G"},
		"enter answers with the highlighted item":  {"\x1b[B\r", user.SixelOff, "E"},
		"a key the caller may not use is no match": {"xm", user.SixelOff, "M"},
		"a sixel terminal answers the same":        {"e", user.SixelOn, "E"},
	} {
		t.Run(name, func(t *testing.T) {
			handled, line, raw := runVision3Menu(t, theme, "MAIN", rec, commands, tc.input, tc.sixel, 80, 24)
			out := stripEscapes(raw)
			if !handled || line != tc.want {
				t.Fatalf("handled=%v line=%q, want %q; output:\n%q", handled, line, tc.want, out)
			}
			for _, want := range []string{"Messages", "Email", "Log off"} {
				if !strings.Contains(out, want) {
					t.Errorf("output lacks %q", want)
				}
			}
			if strings.Contains(out, "Sysop") {
				t.Error("output shows a command the caller may not use")
			}
			picture := strings.Contains(raw, "\x1bP")
			text := strings.Contains(out, vision3TextLogo)
			if wantPicture := tc.sixel == user.SixelOn; picture != wantPicture || text == wantPicture {
				t.Errorf("sixel=%s: sixel drawn=%v, text logo drawn=%v", tc.sixel, picture, text)
			}
		})
	}

	t.Run("a small screen is left to the menu's own", func(t *testing.T) {
		if handled, _, _ := runVision3Menu(t, theme, "MAIN", rec, commands, "e", user.SixelOff, 60, 20); handled {
			t.Error("the script answered on a 60x20 screen")
		}
	})
}

// TestVision3SubmenusRunTheirScript runs each ViSiON/3 menu that names
// submenu.js on a copy of the theme, with no door for it: the theme's script
// draws the menu's lettering and items and answers, a terminal without sixel
// gets no sixel, and on a copy without scripts/submenu.js the menu is left
// to show its own screen.
func TestVision3SubmenusRunTheirScript(t *testing.T) {
	if vision3ThemeDir(t) == "" {
		t.Fatal("no menus/vision3 in this tree")
	}
	withScript := vision3ThemeCopy(t)
	withoutScript := vision3ThemeCopy(t, "scripts/submenu.js")
	commands := []CommandRecord{
		{Keys: "R", Command: "RUN:X", Label: "Read"},
		{Keys: "/G", Command: "RUN:IMMEDIATELOGOFF", Hidden: true},
		{Keys: "Z", Command: "GOTO:ADMIN", ACS: "S200", Label: "Sysop"},
		{Keys: "Q", Command: "GOTO:MAIN", Label: "Quit"},
	}
	recs := themeMenuRecords(t, withScript)
	subs := vision3Submenus(t, withScript)
	for _, name := range subs {
		rec := recs[name]
		if rec == nil {
			t.Errorf("no %s menu", name)
			continue
		}
		t.Run(name, func(t *testing.T) {
			for input, want := range map[string]string{"q": "Q", "\r": "R", "/g": "/G", "zq": "Q"} {
				handled, line, raw := runVision3Menu(t, withScript, name, rec, commands, input, user.SixelOff, 80, 24)
				out := stripEscapes(raw)
				if !handled || line != want {
					t.Fatalf("input %q: handled=%v line=%q, want %q; output:\n%s", input, handled, line, want, out)
				}
				if !strings.Contains(out, "____") {
					t.Error("the menu's lettering is not drawn")
				}
				for _, w := range []string{"Read", "Quit"} {
					if !strings.Contains(out, w) {
						t.Errorf("output lacks %q", w)
					}
				}
				if strings.Contains(out, "Sysop") {
					t.Error("output shows a command the caller may not use")
				}
				if strings.Contains(raw, "\x1bP") {
					t.Error("sixel sent to a terminal without it")
				}
			}
			if handled, _, _ := runVision3Menu(t, withScript, name, rec, commands, "q", user.SixelOff, 60, 20); handled {
				t.Error("the script answered on a 60x20 screen")
			}
			if handled, _, _ := runVision3Menu(t, withoutScript, name, rec, commands, "q", user.SixelOff, 80, 24); handled {
				t.Error("with the script file gone the menu did not fall back to its screen")
			}
		})
	}
	if len(subs) < 10 {
		t.Errorf("%d menus reachable from Main, want at least the 10 stock ones: %v", len(subs), subs)
	}

	// Files has E twice: the sysop's review queue, then Edit Upload. A key
	// runs the first command with it the caller may use, so a sysop is shown
	// the review queue and anyone else Edit Upload.
	files := []CommandRecord{
		{Keys: "E", Command: "RUN:EDITFILERECORD", ACS: "S40", Label: "Review Files"},
		{Keys: "E", Command: "RUN:EDITUPLOAD", Label: "Edit Upload"},
		{Keys: "Q", Command: "GOTO:MAIN", Label: "Main Menu"},
	}
	for level, want := range map[string]string{"S40": "Review Files", "S200": "Edit Upload"} {
		files[0].ACS = level
		_, line, raw := runVision3Menu(t, withScript, "FILEM", recs["FILEM"], files, "e", user.SixelOff, 80, 24)
		out := stripEscapes(raw)
		other := map[string]string{"Review Files": "Edit Upload", "Edit Upload": "Review Files"}[want]
		if line != "E" || !strings.Contains(out, want) || strings.Contains(out, other) {
			t.Errorf("ACS %s: line=%q, shows %q=%v and %q=%v; want only %q",
				level, line, want, strings.Contains(out, want), other, strings.Contains(out, other), want)
		}
	}
}

// TestVision3MenusKeys checks Ctrl+P answers ^P, the board's go-back, on the
// main menu and a submenu, and that Left and Right keep to the selected row,
// wrapping past a column with no item on it.
func TestVision3MenusKeys(t *testing.T) {
	if vision3ThemeDir(t) == "" {
		t.Fatal("no menus/vision3 in this tree")
	}
	theme := vision3ThemeCopy(t)
	recs := themeMenuRecords(t, theme)
	// Seven items: three rows, the last column holding only G on row 1.
	commands := []CommandRecord{
		{Keys: "A", Command: "RUN:X", Label: "Alpha"},
		{Keys: "B", Command: "RUN:X", Label: "Bravo"},
		{Keys: "C", Command: "RUN:X", Label: "Charlie"},
		{Keys: "D", Command: "RUN:X", Label: "Delta"},
		{Keys: "E", Command: "RUN:X", Label: "Echo"},
		{Keys: "F", Command: "RUN:X", Label: "Foxtrot"},
		{Keys: "G", Command: "RUN:X", Label: "Golf"},
		{Keys: "/S", Command: "RUN:X", Hidden: true},
	}
	const down, right, left = "\x1b[B", "\x1b[C", "\x1b[D"
	for _, menu := range []string{"MAIN", "EMAILM"} {
		for name, tc := range map[string]struct{ input, want string }{
			"Ctrl+P goes back":              {"\x10", "^P"},
			"Ctrl+P goes back mid-shortcut": {"/\x10", "^P"},
			"Right keeps to the row":        {down + right + right + "\r", "B"},
			"Right reaches the last column": {right + right + "\r", "G"},
			"Left wraps on the row":         {down + left + "\r", "E"},
		} {
			t.Run(menu+"/"+name, func(t *testing.T) {
				handled, line, raw := runVision3Menu(t, theme, menu, recs[menu], commands, tc.input, user.SixelOff, 80, 24)
				if !handled || line != tc.want {
					t.Fatalf("handled=%v line=%q, want %q; output:\n%s", handled, line, tc.want, stripEscapes(raw))
				}
			})
		}
	}
}
