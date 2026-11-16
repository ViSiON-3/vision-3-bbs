package config

import (
	"os"
	"path/filepath"
	"testing"
)

// A DOS attribute becomes the pipe codes that draw it: |B codes are in ANSI
// order, so the DOS blue background (1) is |B4 and cyan (3) is |B6.
func TestAttrPipeCode(t *testing.T) {
	for attr, want := range map[int]string{
		7: "|07", 15: "|15", 0: "|00", 31: "|15|B4", 48: "|00|B6", 0x4E: "|14|B1", 0x9F: "|15|B4", -1: "|07", 256: "|07",
	} {
		if got := AttrPipeCode(attr); got != want {
			t.Errorf("AttrPipeCode(%d) = %q, want %q", attr, got, want)
		}
	}
}

// Each slot is the board's override, else the theme's role, else the legacy
// defColorN, else the built-in. The Bar slot's built-in is the theme's
// lightbar highlight, so a lightbar that takes it looks as it did.
func TestColorSlotCodes(t *testing.T) {
	theme := ThemeConfig{YesNoHighlightColor: 31, CallerStat: SetColor(14)}
	if got, want := theme.ColorSlotCodes([7]string{}), [7]string{"|07", "|15", "|07", "|14", "|08", "|11", "|15|B4"}; got != want {
		t.Errorf("defaults = %q, want %q", got, want)
	}
	legacy := [7]string{0: "|12", 3: "|12", 6: "|10|B6"}
	got := theme.WithBoardColors(ThemeColors{CallerPrompt: SetColor(13)}).ColorSlotCodes(legacy)
	if want := [7]string{"|13", "|15", "|07", "|14", "|08", "|11", "|10|B6"}; got != want {
		t.Errorf("with board and legacy = %q, want %q", got, want)
	}
	theme.LightbarSelected = SetColor(0x1E)
	if got := theme.ColorSlotCodes([7]string{})[6]; got != "|14|B4" {
		t.Errorf("bar follows lightbarSelected: %q", got)
	}
	if c, src := theme.ColorWithSource(RoleCallerBar); c != 0x1E || src != SourceBuiltin {
		t.Errorf("callerBar unset = %d from %v", c, src)
	}
}

// The caller colour roles read from theme.json and config.json's
// theme.colors like the other roles.
func TestCallerColorRolesParse(t *testing.T) {
	cfg, err := ParseServerConfig([]byte(`{"theme": {"colors": {"callerBar": 78, "callerPrompt": 0}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Theme.Colors.Get(RoleCallerBar); got != SetColor(78) {
		t.Errorf("callerBar = %+v", got)
	}
	if got := cfg.Theme.Colors.Get(RoleCallerPrompt); got != SetColor(0) {
		t.Errorf("callerPrompt 0 is black, set: %+v", got)
	}
	th := ThemeConfig{}.WithBoardColors(cfg.Theme.Colors)
	if c, src := th.ColorWithSource(RoleCallerBar); c != 78 || src != SourceTheme {
		t.Errorf("board callerBar = %d from %v", c, src)
	}
}

// The editors read a board's slot defaults from its files: the selected
// theme, config.json's theme.colors and strings.json's defColorN. A board
// with none of them gets the built-ins.
func TestBoardColorSlotCodes(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "configs")
	if got := BoardColorSlotCodes(root, configDir); got[0] != "|07" || got[6] != AttrPipeCode(31) {
		t.Errorf("empty board = %q", got)
	}
	for name, body := range map[string]string{
		filepath.Join(configDir, "strings.json"):            `{"defColor1": 12, "defColor2": 12}`,
		filepath.Join(configDir, "config.json"):             `{"theme": {"name": "amber", "colors": {"callerInput": 13}}}`,
		filepath.Join(root, "menus", "amber", "theme.json"): `{"callerStat": 10, "lightbarSelected": 30}`,
	} {
		if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(name, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got := BoardColorSlotCodes(root, configDir)
	if want := [7]string{"|12", "|13", "|07", "|10", "|08", "|11", "|14|B4"}; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
