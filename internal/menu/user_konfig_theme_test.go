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
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
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

// A theme copied into menus/ while the board runs is offered in Konfig at
// once: the list is read from menus/ each time the box opens, with no
// restart and no reload of the executor.
func TestKonfigThemeCopiedInWhileRunning(t *testing.T) {
	um, u := newUserConfigTestUser(t)
	e := konfigThemeExecutor(t)
	screen := runKonfigOn(t, e, um, u, "vq")
	if !strings.Contains(screen.Row(konfigEditRow), "has one theme, v3") {
		t.Fatalf("before the copy, status row = %q", screen.Row(konfigEditRow))
	}

	// The sysop prepares the theme elsewhere and copies it in.
	src := filepath.Join(t.TempDir(), "glass")
	for rel, body := range map[string]string{
		"mnu/MAIN.MNU":     "{}",
		"theme.json":       "{}",
		"ansi/KONFIG.ANS":  "GLASS-KONFIG-ART\r\n",
		"scripts/menu.js":  "",
		"data/picture.png": "",
	} {
		path := filepath.Join(src, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// As the guide says: copy it in under a dot name, which is never
	// offered, then rename it into place.
	menus := filepath.Dir(e.MenuSetPath)
	if err := os.CopyFS(filepath.Join(menus, ".glass.new"), os.DirFS(src)); err != nil {
		t.Fatal(err)
	}
	if got := e.installedThemes(); !slices.Equal(got, []string{"v3"}) {
		t.Fatalf("a theme being copied in under a dot name is offered: %v", got)
	}
	if err := os.Rename(filepath.Join(menus, ".glass.new"), filepath.Join(menus, "glass")); err != nil {
		t.Fatal(err)
	}

	if got := e.installedThemes(); !slices.Equal(got, []string{"glass", "v3"}) {
		t.Fatalf("installed after the copy = %v", got)
	}
	screen = runKonfigOn(t, e, um, reloadUser(t, um), "v"+keyUp+"\rq")
	if got := reloadUser(t, um).Theme; got != "glass" {
		t.Fatalf("Theme = %q, want glass", got)
	}
	if snap := screen.Snapshot(); !strings.Contains(snap, "GLASS-KONFIG-ART") {
		t.Errorf("Konfig not redrawn in the copied theme:\n%s", snap)
	}
}

// Konfig shows each theme by its name: the list, the description of the
// highlighted theme, the Theme value and the Saved line. The choice saved
// is the theme's directory.
func TestKonfigThemeNames(t *testing.T) {
	um, u := newUserConfigTestUser(t)
	e := konfigThemeExecutor(t, "amber", "zed")
	menus := filepath.Dir(e.MenuSetPath)
	if err := os.WriteFile(filepath.Join(menus, "amber", "theme.json"),
		[]byte(`{"name": "Amber Glow", "description": "Warm light on black"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	e.SetServerConfig(config.ServerConfig{Theme: config.ThemeSelection{Labels: map[string]config.ThemeLabel{
		"v3": {Name: "V3-Stock", Description: "Stock ViSiON/3 menus, as shipped"},
	}}})

	screen := runKonfigOn(t, e, um, u, "q")
	if !strings.Contains(screen.Snapshot(), "[V] Theme           V3-Stock (default)") {
		t.Errorf("Theme value is not the board theme's name:\n%s", screen.Snapshot())
	}

	// The box opens on V3-Stock, the board's, with its description; Up
	// highlights Amber Glow and shows its description; Enter picks it.
	sess, screen := konfigSessionOn(t, e, um, u, "v"+keyUp+"\rq")
	raw := sess.out.String()
	for _, want := range []string{"Amber Glow", "V3-Stock", "zed", "Stock ViSiON/3 menus, as shipped", "Warm light on black"} {
		if !strings.Contains(raw, want) {
			t.Errorf("box lacks %q", want)
		}
	}
	if strings.Contains(raw, " amber ") {
		t.Errorf("box shows the directory name amber")
	}
	if got := reloadUser(t, um).Theme; got != "amber" {
		t.Fatalf("Theme = %q, want the directory amber", got)
	}
	if row := screen.Row(konfigEditRow); !strings.Contains(row, "Theme is now Amber Glow") {
		t.Errorf("Saved line = %q", row)
	}
	if !strings.Contains(screen.Snapshot(), "[V] Theme           Amber Glow") {
		t.Errorf("Theme value after the pick:\n%s", screen.Snapshot())
	}

	// Renaming the theme keeps the caller's choice: it is the directory.
	if err := os.WriteFile(filepath.Join(menus, "amber", "theme.json"), []byte(`{"name": "Ember"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	screen = runKonfigOn(t, e, um, reloadUser(t, um), "q")
	if !strings.Contains(screen.Snapshot(), "[V] Theme           Ember") {
		t.Errorf("after a rename:\n%s", screen.Snapshot())
	}
}

// A description wider than the 36-column box widens it, up to the 60
// columns a description is shown in, still centred on the 80-column form.
func TestKonfigThemeBoxFitsDescription(t *testing.T) {
	um, u := newUserConfigTestUser(t)
	e := konfigThemeExecutor(t, "amber")
	long := strings.Repeat("d", 70)
	if err := os.WriteFile(filepath.Join(filepath.Dir(e.MenuSetPath), "amber", "theme.json"),
		[]byte(`{"description": "`+long+`"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	sess, _ := konfigSessionOn(t, e, um, u, "v"+keyUp+keyEsc+"q")
	raw := sess.out.String()
	// 64 columns wide and 6 rows high: column (80-64)/2+1 = 9, row
	// (24-6)/2+1 = 10.
	if !strings.Contains(raw, moveTo(10, 9)+pc(9)+"┌─") {
		t.Errorf("box not widened to 64 columns at column 9")
	}
	// Cut to 60 columns, its last two "..".
	if !strings.Contains(raw, strings.Repeat("d", 58)+"..") || strings.Contains(raw, strings.Repeat("d", 59)) {
		t.Errorf("description not cut to 60 columns")
	}
}

// A name with a pipe code prints as typed in the list, the Theme value and
// the Saved line, and on the only-one line; it never colours them.
func TestKonfigThemeNamePipesPrintAsTyped(t *testing.T) {
	um, u := newUserConfigTestUser(t)
	e := konfigThemeExecutor(t, "amber")
	if err := os.WriteFile(filepath.Join(filepath.Dir(e.MenuSetPath), "amber", "theme.json"),
		[]byte(`{"name": "|C1Glass"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	sess, screen := konfigSessionOn(t, e, um, u, "v"+keyUp+"\rq")
	if !strings.Contains(sess.out.String(), " |C1Glass ") {
		t.Errorf("the themes box does not show |C1Glass as typed")
	}
	if row := screen.Row(konfigEditRow); !strings.Contains(row, "Theme is now |C1Glass") {
		t.Errorf("Saved line = %q", row)
	}
	if !strings.Contains(screen.Snapshot(), "[V] Theme           |C1Glass") {
		t.Errorf("Theme value:\n%s", screen.Snapshot())
	}

	one := konfigThemeExecutor(t)
	one.SetServerConfig(config.ServerConfig{Theme: config.ThemeSelection{Labels: map[string]config.ThemeLabel{"v3": {Name: "|C1Stock"}}}})
	_, screen = konfigSessionOn(t, one, um, reloadUser(t, um), "vq")
	if row := screen.Row(konfigEditRow); !strings.Contains(row, "has one theme, |C1Stock") {
		t.Errorf("only-one line = %q", row)
	}
}

// konfigSessionOn runs Konfig on an 80 by 24 screen and returns the session,
// with everything written to it, and the screen.
func konfigSessionOn(t *testing.T, e *MenuExecutor, um *user.UserMgr, u *user.User, keys string) (*teeSession, *testterm.Term) {
	t.Helper()
	screen := testterm.New(80, 24)
	sess := &teeSession{Session: testterm.NewSession(screen, keys), out: &bytes.Buffer{}}
	t.Cleanup(func() { resetSessionIH(sess); sessionTermSizes.Delete(sess) })
	c := &cmdCtx{e: e.ForUser(u), s: sess, terminal: term.NewTerminal(sess, ""), userManager: um, currentUser: u,
		nodeNumber: 1, outputMode: ansi.OutputModeUTF8, termWidth: 80, termHeight: 24}
	if _, _, err := runUserKonfig(c, ""); err != nil {
		t.Fatal(err)
	}
	return sess, screen
}
