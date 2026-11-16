package config

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
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
	// The source is lightbarSelected's, so a theme's highlight isn't
	// reported as built-in, nor overridden by a legacy defColor7.
	if c, src := theme.ColorWithSource(RoleCallerBar); c != 0x1E || src != SourceTheme {
		t.Errorf("callerBar unset = %d from %v", c, src)
	}
	if got := theme.ColorSlotCodes([7]string{6: "|10|B6"})[6]; got != "|14|B4" {
		t.Errorf("legacy defColor7 over the theme's lightbarSelected: %q", got)
	}
	if c, src := ResolveColor(RoleCallerBar, ThemeConfig{}, ThemeColors{LightbarSelected: SetColor(0x4E)}); c != 0x4E || src != SourceBoard {
		t.Errorf("callerBar unset, board lightbarSelected = %d from %v", c, src)
	}
}

// ansi's built-in slot colours, which draw |C1-|C7 before a theme loads,
// are the caller roles' built-in colours, the Bar slot's being the
// built-in lightbarSelected.
func TestColorSlotDefaultsMatchBuiltinRoles(t *testing.T) {
	th := builtinTheme()
	for i, role := range CallerColorRoles {
		if got, want := ansi.ColorSlotDefaults[i], AttrPipeCode(th.Color(role)); got != want {
			t.Errorf("slot %d: ansi has %q, the %s role %q", i+1, got, role, want)
		}
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

// A caller role the theme and board leave unset resolves to the legacy
// defColorN; a set one, and any other role, ignore it.
func TestResolveCallerColor(t *testing.T) {
	theme := ThemeConfig{YesNoHighlightColor: 31, CallerStat: SetColor(14)}
	legacy := [7]string{"|12", "", "", "|10", "", "", "|00|B6"}
	for _, tc := range []struct {
		role ColorRole
		v    int
		src  ColorSource
	}{
		{RoleCallerPrompt, 12, SourceStrings},
		{RoleCallerInput, 15, SourceBuiltin},
		{RoleCallerStat, 14, SourceTheme},
		{RoleCallerBar, 48, SourceStrings},
		{RoleLightbarSelected, 31, SourceBuiltin},
	} {
		if v, src := ResolveCallerColor(tc.role, theme, ThemeColors{}, legacy); v != tc.v || src != tc.src {
			t.Errorf("%s: %d from %v, want %d from %v", tc.role, v, src, tc.v, tc.src)
		}
	}
	if v, src := ResolveCallerColor(RoleCallerPrompt, theme, ThemeColors{CallerPrompt: SetColor(7)}, legacy); v != 7 || src != SourceBoard {
		t.Errorf("board override: %d from %v", v, src)
	}
}

// The editors read a board's slot defaults from its files: the selected
// theme, config.json's theme.colors and strings.json's defColorN. A board
// with none of them gets the built-ins.
func TestBoardColorSlotCodes(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "configs")
	if got := BoardColorSlotCodes(root, configDir, ""); got[0] != "|07" || got[6] != AttrPipeCode(31) {
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
	got := BoardColorSlotCodes(root, configDir, "")
	if want := [7]string{"|12", "|13", "|07", "|10", "|08", "|11", "|14|B4"}; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	// A menu set named outright, as menuedit --menus does, is read in place
	// of the selected one.
	v3 := filepath.Join(root, "menus", "v3")
	if err := os.MkdirAll(v3, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(v3, "theme.json"), []byte(`{"callerStat": 9}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := BoardColorSlotCodes(root, configDir, v3)[3]; got != "|09" {
		t.Errorf("--menus v3 stat = %q, want v3's |09", got)
	}
}

// Reading the slot defaults logs nothing, so an editor can call it with its
// screen up without touching the process's logger.
func TestBoardColorSlotCodesIsQuiet(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "configs")
	if err := os.MkdirAll(filepath.Join(root, "menus", "v3"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "menus", "v3", "theme.json"), []byte(`{not json`), 0o644); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	if got := BoardColorSlotCodes(root, configDir, ""); got[0] != "|07" {
		t.Errorf("a bad theme.json should leave the built-ins: %q", got)
	}
	if buf.Len() != 0 {
		t.Errorf("logged: %s", buf.String())
	}
}

// A slot's pipe code reads back as the attribute a lightbar draws, and
// AttrPipeCode undoes it.
func TestPipeCodeAttr(t *testing.T) {
	for code, want := range map[string]int{
		"|07": 7, "|15|B4": 31, "|00|B6": 48, "|14|B1": 0x4E, "|15|B12": 31, "": 7, "|C1": 7, "|99": 7,
	} {
		if got := PipeCodeAttr(code); got != want {
			t.Errorf("PipeCodeAttr(%q) = %d, want %d", code, got, want)
		}
	}
	for attr := 0; attr < 128; attr++ {
		if got := PipeCodeAttr(AttrPipeCode(attr)); got != attr {
			t.Errorf("attribute %d round-trips to %d", attr, got)
		}
	}
}
