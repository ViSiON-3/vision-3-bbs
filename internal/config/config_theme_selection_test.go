package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/menuset"
)

// themeBoard is a BBS root with menus/v3 and menus/amber, and a config.json
// holding configJSON ("" for no file). It returns the root and configs dir.
func themeBoard(t *testing.T, configJSON string) (root, configDir string) {
	t.Helper()
	root = t.TempDir()
	configDir = filepath.Join(root, "configs")
	for _, d := range []string{configDir, filepath.Join(root, "menus", "v3"), filepath.Join(root, "menus", "amber")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if configJSON != "" {
		if err := os.WriteFile(filepath.Join(configDir, "config.json"), []byte(configJSON), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root, configDir
}

// An omitted theme.name, in every way it can be omitted, selects v3.
func TestThemeNameDefaultsToV3(t *testing.T) {
	for name, js := range map[string]string{
		"no config.json":   "",
		"no theme section": `{"boardName": "X"}`,
		"empty section":    `{"theme": {}}`,
		"empty name":       `{"theme": {"name": ""}}`,
	} {
		t.Run(name, func(t *testing.T) {
			root, configDir := themeBoard(t, js)
			dir, theme, err := SelectedMenuSet(root, configDir)
			if err != nil || theme != "v3" || dir != filepath.Join(root, "menus", "v3") {
				t.Errorf("SelectedMenuSet = %q, %q, %v", dir, theme, err)
			}
			cfg, err := LoadServerConfig(configDir)
			if err != nil || cfg.ThemeName() != menuset.DefaultTheme {
				t.Errorf("LoadServerConfig: ThemeName %q, %v", cfg.ThemeName(), err)
			}
		})
	}
}

// A second theme resolves its own directory, and its overlay is
// menus.d/<name>: a file there wins over the shipped one, and v3's overlay
// is not consulted.
func TestSelectedMenuSetAlternateTheme(t *testing.T) {
	root, configDir := themeBoard(t, `{"theme": {"name": "amber"}}`)
	write := func(rel, content string) {
		t.Helper()
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("menus/amber/ansi/MAIN.ANS", "amber shipped")
	write("menus/amber/ansi/ONLY.ANS", "amber only")
	write("menus.d/amber/ansi/MAIN.ANS", "amber overlay")
	write("menus/v3/ansi/MAIN.ANS", "v3 shipped")
	write("menus.d/v3/ansi/ONLY.ANS", "v3 overlay")
	write("menus.d/amber/theme.json", `{"yesNoHighlightColor": 42}`)

	dir, theme, err := SelectedMenuSet(root, configDir)
	if err != nil || theme != "amber" || dir != filepath.Join(root, "menus", "amber") {
		t.Fatalf("SelectedMenuSet = %q, %q, %v", dir, theme, err)
	}
	cfg, err := LoadServerConfig(configDir)
	if err != nil || cfg.ThemeName() != "amber" {
		t.Fatalf("LoadServerConfig: ThemeName %q, %v", cfg.ThemeName(), err)
	}

	set := menuset.FromPath(dir)
	for file, want := range map[string]string{"MAIN.ANS": "amber overlay", "ONLY.ANS": "amber only"} {
		path, err := set.Resolve("ansi", file)
		if err != nil {
			t.Fatal(err)
		}
		if got, _ := os.ReadFile(path); string(got) != want {
			t.Errorf("%s = %q, want %q", file, got, want)
		}
	}
	th, err := LoadThemeConfig(dir)
	if err != nil || th.YesNoHighlightColor != 42 {
		t.Errorf("theme.json from the overlay: %d, %v", th.YesNoHighlightColor, err)
	}
}

// A selection that can't be used is an error that names the setting and the
// problem; nothing falls back to v3.
func TestSelectedMenuSetInvalid(t *testing.T) {
	for name, tc := range map[string]struct{ js, want string }{
		"no such theme":  {`{"theme": {"name": "neon"}}`, `theme "neon" not found`},
		"path traversal": {`{"theme": {"name": "../v3"}}`, "starts with a dot"},
		"separator":      {`{"theme": {"name": "v3/ansi"}}`, "may hold only"},
		"broken json":    {`{"theme": `, "config.json"},
	} {
		t.Run(name, func(t *testing.T) {
			root, configDir := themeBoard(t, tc.js)
			dir, _, err := SelectedMenuSet(root, configDir)
			if err == nil || dir != "" || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("SelectedMenuSet = %q, %v; want an error holding %q", dir, err, tc.want)
			}
			if name != "broken json" && !strings.Contains(err.Error(), "theme.name") {
				t.Errorf("error doesn't name the setting: %v", err)
			}
		})
	}
}
