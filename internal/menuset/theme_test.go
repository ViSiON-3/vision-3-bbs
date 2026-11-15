package menuset

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestValidThemeName(t *testing.T) {
	for _, name := range []string{"v3", "amber", "My_Theme-2", "v3.1"} {
		if err := ValidThemeName(name); err != nil {
			t.Errorf("ValidThemeName(%q) = %v, want nil", name, err)
		}
	}
	for _, name := range []string{"", ".", "..", ".hidden", "a/b", `a\b`, "../v3", "with space", "café"} {
		if err := ValidThemeName(name); err == nil {
			t.Errorf("ValidThemeName(%q) = nil, want an error", name)
		}
	}
}

// themeRoot is a BBS root with the named themes under menus/.
func themeRoot(t *testing.T, themes ...string) string {
	t.Helper()
	root := t.TempDir()
	for _, name := range themes {
		if err := os.MkdirAll(filepath.Join(root, ThemesDir, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestCheckTheme(t *testing.T) {
	root := themeRoot(t, "v3", "amber")
	// A file under menus/ is not a theme.
	if err := os.WriteFile(filepath.Join(root, ThemesDir, "README"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	dir, err := CheckTheme(root, "amber")
	if err != nil || dir != filepath.Join(root, "menus", "amber") {
		t.Errorf("CheckTheme(amber) = %q, %v", dir, err)
	}
	// The overlay of a theme is menus.d/<name>, by the usual pairing.
	if got, want := OverlayFor(dir), filepath.Join(root, "menus.d", "amber"); got != want {
		t.Errorf("overlay = %q, want %q", got, want)
	}

	_, err = CheckTheme(root, "missing")
	if err == nil || !strings.Contains(err.Error(), `theme "missing" not found`) || !strings.Contains(err.Error(), "amber, v3") {
		t.Errorf("missing theme: %v, want it named with the themes that exist", err)
	}
	if _, err = CheckTheme(root, "README"); err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Errorf("a file: %v", err)
	}
	if _, err = CheckTheme(root, "../v3"); err == nil {
		t.Error("a name with a path separator was accepted")
	}
}

func TestListThemes(t *testing.T) {
	root := themeRoot(t, "v3", "amber", ".git", "bad name")
	if got, want := ListThemes(root), []string{"amber", "v3"}; !reflect.DeepEqual(got, want) {
		t.Errorf("ListThemes = %v, want %v", got, want)
	}
	if got := ListThemes(t.TempDir()); len(got) != 0 {
		t.Errorf("no menus directory: %v", got)
	}
}

func TestRunning(t *testing.T) {
	t.Cleanup(func() { SetRunning("") })
	if Running() != "" {
		t.Fatalf("Running() = %q before SetRunning", Running())
	}
	SetRunning("/bbs/menus/amber")
	if Running() != "/bbs/menus/amber" {
		t.Errorf("Running() = %q", Running())
	}
}
