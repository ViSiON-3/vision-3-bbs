package menu

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// themeStringsBoard is themeViewBoard with a strings.json in the board's
// theme (v3) and in amber, each with its own login-cancelled message. The
// board's configs/strings.json sets pauseString and konfigTitle.
func themeStringsBoard(t *testing.T) (board *MenuExecutor, amber string) {
	t.Helper()
	board, amber = themeViewBoard(t)
	write := func(dir, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, "strings.json"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(board.MenuSetPath, `{"execLoginCancelled": "v3 bye"}`)
	write(amber, `{"execLoginCancelled": "amber bye", "konfigTitle": "Amber Konfig"}`)
	board.SetStrings(config.StringsConfig{PauseString: "board pause", KonfigTitle: "Board Konfig", ExecLoginCancelled: "board bye"})
	board.ReloadThemeStrings()
	return board, amber
}

// Screens before login run on the board's executor, which draws the
// board's theme's strings over configs/strings.json.
func TestBoardThemeStrings(t *testing.T) {
	board, _ := themeStringsBoard(t)
	s := board.Strings()
	if s.ExecLoginCancelled != "v3 bye" {
		t.Errorf("execLoginCancelled = %q, want v3's", s.ExecLoginCancelled)
	}
	if s.PauseString != "board pause" || s.KonfigTitle != "Board Konfig" {
		t.Errorf("keys v3 leaves out: %q, %q; want the board's", s.PauseString, s.KonfigTitle)
	}
	// A reload of configs/strings.json reaches the keys the theme leaves
	// out, and the theme's still win.
	board.SetStrings(config.StringsConfig{PauseString: "reloaded", ExecLoginCancelled: "board again"})
	if s := board.Strings(); s.PauseString != "reloaded" || s.ExecLoginCancelled != "v3 bye" {
		t.Errorf("after reload: %q, %q", s.PauseString, s.ExecLoginCancelled)
	}
	// So does a change to the theme's own file.
	if err := os.WriteFile(filepath.Join(board.MenuSetPath, "strings.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	board.ReloadThemeStrings()
	if got := board.Strings().ExecLoginCancelled; got != "board again" {
		t.Errorf("after the theme's file emptied: %q, want the board's", got)
	}
}

// A caller on another theme gets that theme's strings, the board's for the
// rest, and the board's reloads.
func TestCallerThemeStrings(t *testing.T) {
	board, _ := themeStringsBoard(t)
	view := board.ForUser(&user.User{Handle: "Amb", Theme: "amber"})
	s := view.Strings()
	if s.ExecLoginCancelled != "amber bye" || s.KonfigTitle != "Amber Konfig" {
		t.Errorf("amber's keys: %q, %q", s.ExecLoginCancelled, s.KonfigTitle)
	}
	if s.PauseString != "board pause" {
		t.Errorf("pauseString = %q, want the board's", s.PauseString)
	}
	if view.Strings() != s {
		t.Error("Strings rebuilt with nothing changed")
	}
	board.SetStrings(config.StringsConfig{PauseString: "reloaded", KonfigTitle: "Board again"})
	if s := view.Strings(); s.PauseString != "reloaded" || s.KonfigTitle != "Amber Konfig" {
		t.Errorf("after the board's reload: %q, %q", s.PauseString, s.KonfigTitle)
	}
	if got := board.Strings().KonfigTitle; got != "Board again" {
		t.Errorf("the view changed the board's strings: %q", got)
	}

	// A caller whose theme can't be used is on the board's theme, strings
	// included.
	fallback := board.ForUser(&user.User{Handle: "Gone", Theme: "gone"})
	if got := fallback.Strings().ExecLoginCancelled; got != "v3 bye" {
		t.Errorf("fallback view: %q, want v3's", got)
	}
}

// A caller who changes theme in Konfig gets the new theme's strings on the
// executor the session switches to, and the board's theme's on going back.
func TestSwitchThemeStrings(t *testing.T) {
	board, _ := themeStringsBoard(t)
	u := &user.User{Handle: "Sw"}
	u.Theme = "amber"
	next, _, ok := board.SwitchTheme(u, "MAIN")
	if !ok || next.Strings().ExecLoginCancelled != "amber bye" {
		t.Fatalf("to amber: %v, %q", ok, next.Strings().ExecLoginCancelled)
	}
	u.Theme = "v3"
	back, _, ok := next.SwitchTheme(u, "MAIN")
	if !ok || back.Strings().ExecLoginCancelled != "v3 bye" {
		t.Errorf("back to v3: %v, %q", ok, back.Strings().ExecLoginCancelled)
	}
}

// The message editor is given the session's strings, theme strings
// included, so it prints what the menus print.
func TestEditorGetsSessionStrings(t *testing.T) {
	board, _ := themeStringsBoard(t)
	view := board.ForUser(&user.User{Handle: "Amb", Theme: "amber"})
	if ctx := withSessionTheme(view, editor.EditorContext{}); ctx.Strings != view.Strings() || ctx.Strings.ExecLoginCancelled != "amber bye" {
		t.Errorf("view: editor strings %p, want the view's", ctx.Strings)
	}
	if ctx := withSessionTheme(board, editor.EditorContext{}); ctx.Strings != board.Strings() || ctx.Strings.ExecLoginCancelled != "v3 bye" {
		t.Errorf("board: editor strings %p, want the board's", ctx.Strings)
	}
}
