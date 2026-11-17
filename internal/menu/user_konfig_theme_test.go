package menu

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
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
	view := e.ForUser(nil)
	if view.boardThemeName() != "v3" {
		t.Errorf("board theme = %q", view.boardThemeName())
	}
}

// V opens the themes box, the board's marked; picking one saves it and
// leaves Konfig at once, so the calling menu is drawn in it. The keys
// below end without Q: had Konfig stayed open it would have run out of
// input.
func TestKonfigThemePicked(t *testing.T) {
	um, u := newUserConfigTestUser(t)
	e := konfigThemeExecutor(t, "amber", "zed", "nomain")
	screen := runKonfigOn(t, e, um, u, "v"+keyUp+"\r")
	if got := reloadUser(t, um).Theme; got != "amber" {
		t.Fatalf("Theme = %q, want amber", got)
	}
	snap := screen.Snapshot()
	for _, want := range []string{"Themes", "amber", "(board's)", "zed", "Esc done"} {
		if !strings.Contains(snap, want) {
			t.Errorf("box is missing %q:\n%s", want, snap)
		}
	}
	if strings.Contains(snap, "nomain") {
		t.Error("a menu set without a MAIN menu is offered")
	}

	// The value shows the choice; picking the board's own stores none.
	u = reloadUser(t, um)
	runKonfigOn(t, e, um, u, "v"+keyDown+"\r")
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
