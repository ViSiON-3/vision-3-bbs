package menu

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor/testterm"
	"golang.org/x/term"
)

// konfigThemeExecutor is a Konfig executor on a board running menus/v3,
// with the other menu sets named in themes beside it. Each has a MAIN
// menu, except one named "nomain".
func konfigThemeExecutor(t *testing.T, themes ...string) *MenuExecutor {
	t.Helper()
	menus := filepath.Join(t.TempDir(), "menus")
	for _, name := range append([]string{"v3"}, themes...) {
		dir := filepath.Join(menus, name, "mnu")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if name == "nomain" {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, "MAIN.MNU"), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	e := &MenuExecutor{MenuSetPath: filepath.Join(menus, "v3")}
	e.SetStrings(konfigTestStrings(t))
	return e
}

// The themes a caller can choose are the menu sets beside the board's
// with a MAIN menu, the board's own included.
func TestInstalledThemes(t *testing.T) {
	e := konfigThemeExecutor(t, "zed", "amber", "nomain", "bad name")
	if got := e.installedThemes(); !slices.Equal(got, []string{"amber", "v3", "zed"}) {
		t.Errorf("installed = %v", got)
	}
	// A theme installed as a symlink to its directory counts; a stray file
	// doesn't, nor a theme whose theme.json doesn't load.
	menus := filepath.Dir(e.MenuSetPath)
	writeBroken(t, menus, "broken")
	if err := os.Symlink(filepath.Join(menus, "amber"), filepath.Join(menus, "linked")); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	if err := os.WriteFile(filepath.Join(menus, "README"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := e.installedThemes(); !slices.Equal(got, []string{"amber", "linked", "v3", "zed"}) {
		t.Errorf("with a symlinked theme: %v", got)
	}
	view := e.ForUser(nil)
	if view.boardThemeName() != "v3" {
		t.Errorf("board theme = %q", view.boardThemeName())
	}
}

// V opens the themes box, centred on the form's 80 by 24 screen, the
// board's theme marked as the default, and themes without a MAIN menu
// left out.
func TestKonfigThemeBox(t *testing.T) {
	um, u := newUserConfigTestUser(t)
	e := konfigThemeExecutor(t, "amber", "zed", "nomain")
	screen := testterm.New(80, 24)
	sess := &teeSession{Session: testterm.NewSession(screen, "v"+keyEsc+"q"), out: &bytes.Buffer{}}
	t.Cleanup(func() { resetSessionIH(sess); sessionTermSizes.Delete(sess) })
	c := &cmdCtx{e: e, s: sess, terminal: term.NewTerminal(sess, ""), userManager: um, currentUser: u,
		nodeNumber: 1, outputMode: ansi.OutputModeUTF8, termWidth: 80, termHeight: 24}
	if _, _, err := runUserKonfig(c, ""); err != nil {
		t.Fatal(err)
	}
	raw := sess.out.String()
	// Three themes: a box of 7 rows and 36 columns, centred.
	if !strings.Contains(raw, moveTo(9, 23)+pc(9)+"┌─") {
		t.Errorf("box not centred at row 9, column 23")
	}
	for _, want := range []string{"Themes", "amber", "v3", "(default)", "zed", "Esc done"} {
		if !strings.Contains(raw, want) {
			t.Errorf("box is missing %q", want)
		}
	}
	if strings.Contains(raw, "nomain") {
		t.Error("a menu set without a MAIN menu is offered")
	}
	if got := reloadUser(t, um).Theme; got != "" {
		t.Errorf("Theme = %q after Esc", got)
	}
}

// Picking a theme saves it and keeps Konfig open, redrawn in the new
// theme straight away: its art, with the choice confirmed. Picking the
// board's own stores none.
func TestKonfigThemePicked(t *testing.T) {
	um, u := newUserConfigTestUser(t)
	e := konfigThemeExecutor(t, "amber", "zed")
	menus := filepath.Dir(e.MenuSetPath)
	for name, art := range map[string]string{"v3": "V3-KONFIG-ART", "amber": "AMBER-KONFIG-ART"} {
		dir := filepath.Join(menus, name, "ansi")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "KONFIG.ANS"), []byte(art+"\r\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	screen := runKonfigOn(t, e, um, u, "v"+keyUp+"\rq")
	if got := reloadUser(t, um).Theme; got != "amber" {
		t.Fatalf("Theme = %q, want amber", got)
	}
	snap := screen.Snapshot()
	if !strings.Contains(snap, "AMBER-KONFIG-ART") || strings.Contains(snap, "V3-KONFIG-ART") {
		t.Errorf("Konfig not redrawn in amber:\n%s", snap)
	}
	if !strings.Contains(snap, "[V] Theme           amber") || !strings.Contains(screen.Row(konfigEditRow), "Saved") {
		t.Errorf("choice not shown as saved: status %q\n%s", screen.Row(konfigEditRow), snap)
	}

	runKonfigOn(t, e, um, reloadUser(t, um), "v"+keyDown+"\rq")
	if got := reloadUser(t, um).Theme; got != "" {
		t.Errorf("Theme = %q after picking the board's, want none", got)
	}
}

// The form shows the caller's theme, or the board's as the default.
func TestKonfigThemeValue(t *testing.T) {
	um, u := newUserConfigTestUser(t)
	e := konfigThemeExecutor(t, "amber")
	screen := runKonfigOn(t, e, um, u, "q")
	if !strings.Contains(screen.Snapshot(), "[V] Theme           v3 (default)") {
		t.Errorf("no choice:\n%s", screen.Snapshot())
	}
	u.Theme = "amber"
	if err := um.UpdateUser(u); err != nil {
		t.Fatal(err)
	}
	screen = runKonfigOn(t, e, um, reloadUser(t, um), "q")
	if !strings.Contains(screen.Snapshot(), "[V] Theme           amber") {
		t.Errorf("amber chosen:\n%s", screen.Snapshot())
	}
}

// Esc leaves the box with nothing saved, and Konfig stays open.
func TestKonfigThemeBoxCancelled(t *testing.T) {
	um, u := newUserConfigTestUser(t)
	e := konfigThemeExecutor(t, "amber")
	runKonfigOn(t, e, um, u, "v"+keyUp+keyEsc+"q")
	if got := reloadUser(t, um).Theme; got != "" {
		t.Errorf("Theme = %q after Esc", got)
	}
}

// With only the board's theme there is nothing to choose: Theme says so
// and Konfig stays open.
func TestKonfigThemeOnlyOne(t *testing.T) {
	um, u := newUserConfigTestUser(t)
	e := konfigThemeExecutor(t, "nomain")
	screen := runKonfigOn(t, e, um, u, "vq")
	if !strings.Contains(screen.Row(konfigEditRow), "has one theme, v3") {
		t.Errorf("status row = %q", screen.Row(konfigEditRow))
	}
	if got := reloadUser(t, um).Theme; got != "" {
		t.Errorf("Theme = %q", got)
	}
}

// With caller choice turned off, Konfig has no Theme item.
func TestKonfigThemeOff(t *testing.T) {
	um, u := newUserConfigTestUser(t)
	e := konfigThemeExecutor(t, "amber")
	off := false
	e.SetServerConfig(config.ServerConfig{Theme: config.ThemeSelection{CallerChoice: &off}})
	screen := runKonfigOn(t, e, um, u, "vq")
	if strings.Contains(screen.Snapshot(), "Theme") {
		t.Errorf("Theme offered with the choice off:\n%s", screen.Snapshot())
	}
	if got := reloadUser(t, um).Theme; got != "" {
		t.Errorf("Theme = %q", got)
	}
}

// On a taller screen the help, status and legend rows move to its foot;
// the form stays at the top.
func TestKonfigFooterAtTheFootOfTheScreen(t *testing.T) {
	um, u := newUserConfigTestUser(t)
	e := konfigThemeExecutor(t)
	screen := testterm.New(80, 40)
	sess := testterm.NewSession(screen, "q")
	t.Cleanup(func() { resetSessionIH(sess); sessionTermSizes.Delete(sess) })
	c := &cmdCtx{e: e, s: sess, terminal: term.NewTerminal(sess, ""), userManager: um, currentUser: u,
		nodeNumber: 1, outputMode: ansi.OutputModeUTF8, termWidth: 80, termHeight: 40}
	if _, _, err := runUserKonfig(c, ""); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(screen.Row(36), "────") {
		t.Errorf("rule row 36 = %q", screen.Row(36))
	}
	if !strings.Contains(screen.Row(37), "Columns your terminal shows") {
		t.Errorf("help row 37 = %q", screen.Row(37))
	}
	if !strings.Contains(screen.Row(konfigTopRow), "Terminal") {
		t.Errorf("form's first row %d = %q", konfigTopRow, screen.Row(konfigTopRow))
	}
}

// A screen height changed in Konfig moves the footer at once: the form is
// drawn again on the new size.
func TestKonfigFooterFollowsAHeightChange(t *testing.T) {
	um, u := newUserConfigTestUser(t)
	e := konfigThemeExecutor(t)
	screen := testterm.New(80, 40)
	sess := testterm.NewSession(screen, "b"+keyClear+"40\rq")
	t.Cleanup(func() { resetSessionIH(sess); sessionTermSizes.Delete(sess) })
	c := &cmdCtx{e: e, s: sess, terminal: term.NewTerminal(sess, ""), userManager: um, currentUser: u,
		nodeNumber: 1, outputMode: ansi.OutputModeUTF8, termWidth: 80, termHeight: 24}
	if _, _, err := runUserKonfig(c, ""); err != nil {
		t.Fatal(err)
	}
	if got := reloadUser(t, um).ScreenHeight; got != 40 {
		t.Fatalf("ScreenHeight = %d", got)
	}
	if !strings.Contains(screen.Row(37), "Rows your terminal shows") || !strings.Contains(screen.Row(36), "────") {
		t.Errorf("footer not at the foot of 40 rows: rule %q, help %q", screen.Row(36), screen.Row(37))
	}
	if strings.Contains(screen.Row(20), "────") || strings.Contains(screen.Row(21), "Rows your") {
		t.Errorf("old footer left on rows 20-21: %q / %q", screen.Row(20), screen.Row(21))
	}
}

// After a theme change, Header Style is named from the new theme's
// MSGHDR.BAR, not the one read before.
func TestKonfigThemeChangeRenamesHeaderStyle(t *testing.T) {
	um, u := newUserConfigTestUser(t)
	e := konfigThemeExecutor(t, "amber")
	menus := filepath.Dir(e.MenuSetPath)
	for name, label := range map[string]string{"v3": "Classic", "amber": "Ambered"} {
		dir := filepath.Join(menus, name, "bar")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "MSGHDR.BAR"), []byte("0,0,31,7,1,1,"+label+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	u.MsgHdr = 1
	if err := um.UpdateUser(u); err != nil {
		t.Fatal(err)
	}
	screen := runKonfigOn(t, e, um, reloadUser(t, um), "v"+keyUp+"\rq")
	if snap := screen.Snapshot(); !strings.Contains(snap, "Ambered") || strings.Contains(snap, "Classic") {
		t.Errorf("Header Style not renamed from amber's MSGHDR.BAR:\n%s", snap)
	}
}

// writeBroken puts a theme with a MAIN menu but a theme.json that doesn't
// load beside the others.
func writeBroken(t *testing.T, menus, name string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(menus, name, "mnu"), 0o755); err != nil {
		t.Fatal(err)
	}
	for rel, body := range map[string]string{"mnu/MAIN.MNU": "{}", "theme.json": "{"} {
		if err := os.WriteFile(filepath.Join(menus, name, rel), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// A caller whose theme was removed sees the board's, so Theme shows the
// board's as the default; their choice is kept for when it comes back.
// Picking the board's in the box clears it. A theme whose theme.json
// doesn't load is the same: the session shows the board's.
func TestKonfigThemeRemoved(t *testing.T) {
	um, u := newUserConfigTestUser(t)
	e := konfigThemeExecutor(t, "amber")
	writeBroken(t, filepath.Dir(e.MenuSetPath), "broken")
	for _, gone := range []string{"test", "broken"} {
		u.Theme = gone
		if err := um.UpdateUser(u); err != nil {
			t.Fatal(err)
		}
		screen := runKonfigOn(t, e, um, reloadUser(t, um), "q")
		if snap := screen.Snapshot(); !strings.Contains(snap, "[V] Theme           v3 (default)") || strings.Contains(snap, gone) {
			t.Errorf("%s shown:\n%s", gone, snap)
		}
		if view := e.ForUser(reloadUser(t, um)); view.Menus().Base != e.Menus().Base {
			t.Errorf("%s: the session runs on %q, not the board's", gone, view.Menus().Base)
		}
	}
	u.Theme = "test"
	if err := um.UpdateUser(u); err != nil {
		t.Fatal(err)
	}
	screen := runKonfigOn(t, e, um, reloadUser(t, um), "q")
	if snap := screen.Snapshot(); !strings.Contains(snap, "[V] Theme           v3 (default)") || strings.Contains(snap, "test") {
		t.Errorf("removed theme shown:\n%s", snap)
	}
	if got := reloadUser(t, um).Theme; got != "test" {
		t.Errorf("the choice was dropped: %q", got)
	}
	// The box opens on the board's theme; Enter there clears the choice.
	runKonfigOn(t, e, um, reloadUser(t, um), "v\rq")
	if got := reloadUser(t, um).Theme; got != "" {
		t.Errorf("Theme = %q after picking the board's, want none", got)
	}
}
