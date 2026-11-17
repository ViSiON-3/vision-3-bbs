package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// theme.colors overrides only the colours it names. Every other colour is
// the theme's, and follows the theme when its defaults change.
func TestBoardColorsOverrideOnlyNamedKeys(t *testing.T) {
	theme := ThemeConfig{
		YesNoHighlightColor: 31, YesNoRegularColor: 13, ChatSysopColor: 11, ChatUserColor: 10,
		AutocorrectHighlightColor: 15, ToastFadeColors: "8,7,15",
		LightbarSelected: SetColor(31), LightbarHint: SetColor(8),
	}
	cfg, err := ParseServerConfig([]byte(`{"theme": {"name": "v3", "colors": {"lightbarSelected": 78, "chatUserColor": 14}}}`))
	if err != nil {
		t.Fatal(err)
	}
	board := cfg.Theme.Colors

	want := map[ColorRole]struct {
		v   int
		src ColorSource
	}{
		RoleLightbarSelected: {78, SourceBoard},
		RoleChatUser:         {14, SourceBoard},
		RoleLightbarHint:     {8, SourceTheme},
		RoleLightbarNormal:   {13, SourceBuiltin}, // falls back to the Yes/No regular colour
		RoleLightbarBorder:   {8, SourceBuiltin},
		RoleYesNoHighlight:   {31, SourceTheme},
		RoleChatSysop:        {11, SourceTheme},
	}
	eff := theme.WithBoardColors(board)
	for role, w := range want {
		if v, src := ResolveColor(role, theme, board); v != w.v || src != w.src {
			t.Errorf("%s = %d from %q, want %d from %q", role, v, src, w.v, w.src)
		}
		if got := eff.Color(role); got != w.v {
			t.Errorf("effective theme %s = %d, want %d", role, got, w.v)
		}
	}
	if eff.ToastFadeColors != "8,7,15" {
		t.Errorf("fade ramp = %q with no override", eff.ToastFadeColors)
	}

	// The theme's defaults change (an upgrade, or another theme): the two
	// overrides hold, and everything else follows the theme.
	theme.LightbarHint, theme.YesNoRegularColor, theme.ChatSysopColor, theme.LightbarSelected = SetColor(3), 5, 9, SetColor(47)
	eff = theme.WithBoardColors(board)
	for role, v := range map[ColorRole]int{
		RoleLightbarSelected: 78, RoleChatUser: 14,
		RoleLightbarHint: 3, RoleLightbarNormal: 5, RoleChatSysop: 9,
	} {
		if got := eff.Color(role); got != v {
			t.Errorf("after the theme changed, %s = %d, want %d", role, got, v)
		}
	}
}

// An override of 0 is colour 0; a colour left out, or null, inherits.
func TestBoardColorZeroIsNotMissing(t *testing.T) {
	theme := ThemeConfig{YesNoHighlightColor: 31, LightbarHint: SetColor(8)}
	cfg, err := ParseServerConfig([]byte(`{"theme": {"colors": {"lightbarHint": 0, "yesNoHighlightColor": 0, "lightbarBorder": null}}}`))
	if err != nil {
		t.Fatal(err)
	}
	board := cfg.Theme.Colors
	for _, role := range []ColorRole{RoleLightbarHint, RoleYesNoHighlight} {
		if v, src := ResolveColor(role, theme, board); v != 0 || src != SourceBoard {
			t.Errorf("%s = %d from %q, want the board's 0", role, v, src)
		}
	}
	if v, src := ResolveColor(RoleLightbarBorder, theme, board); v != 8 || src != SourceBuiltin {
		t.Errorf("a null lightbarBorder = %d from %q", v, src)
	}

	// Only what is overridden is written, and the 0s survive a round trip.
	out, err := json.Marshal(cfg.Theme)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(out), `{"colors":{"yesNoHighlightColor":0,"lightbarHint":0}}`; got != want {
		t.Errorf("theme section = %s, want %s", got, want)
	}
	// No overrides and the default theme: no theme section at all.
	out, err = json.Marshal(struct {
		Theme ThemeSelection `json:"theme,omitzero"`
	}{})
	if err != nil || string(out) != `{}` {
		t.Errorf("empty theme section = %s, %v", out, err)
	}

	// Use theme default: the key goes.
	if !board.Inherit(RoleLightbarHint) || board.Inherit(RoleLightbarHint) || board.LightbarHint.Set {
		t.Errorf("Inherit: %+v", board.LightbarHint)
	}
	if v, src := ResolveColor(RoleLightbarHint, theme, board); v != 8 || src != SourceTheme {
		t.Errorf("after Inherit lightbarHint = %d from %q", v, src)
	}
}

// The fade ramp is overridden the same way, and inherited when left out.
func TestBoardFadeRamp(t *testing.T) {
	theme := ThemeConfig{ToastFadeColors: "8,7,15"}
	if got := theme.WithBoardColors(ThemeColors{ToastFadeColors: "1,9"}).FadeColors(); len(got) != 2 || got[0] != 1 || got[1] != 9 {
		t.Errorf("overridden ramp = %v", got)
	}
	if got := theme.WithBoardColors(ThemeColors{}).FadeColors(); len(got) != 3 {
		t.Errorf("inherited ramp = %v", got)
	}
}

// A theme.colors value that isn't a colour fails the load, so a reload
// keeps the configuration that was running.
func TestBoardColorsInvalid(t *testing.T) {
	for name, js := range map[string]string{
		"out of range":  `{"theme": {"colors": {"lightbarSelected": 300}}}`,
		"negative":      `{"theme": {"colors": {"lightbarHint": -1}}}`,
		"not a number":  `{"theme": {"colors": {"chatUserColor": "green"}}}`,
		"bad fade ramp": `{"theme": {"colors": {"toastFadeColors": "8,99"}}}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseServerConfig([]byte(js)); err == nil {
				t.Error("accepted")
			}
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(js), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadServerConfig(dir); err == nil {
				t.Error("LoadServerConfig accepted it")
			}
			if _, err := BoardColorsFromConfigDir(dir); err == nil {
				t.Error("BoardColorsFromConfigDir accepted it")
			}
		})
	}
}

// BoardColorsFromConfigDir reads the overrides alone, and none from a
// board that has no config.json or no theme section.
func TestBoardColorsFromConfigDir(t *testing.T) {
	dir := t.TempDir()
	if c, err := BoardColorsFromConfigDir(dir); err != nil || c != (ThemeColors{}) {
		t.Errorf("no config.json: %+v, %v", c, err)
	}
	js := `{"boardName": "X", "theme": {"colors": {"autocorrectHighlightColor": 12, "toastFadeColors": "1,2"}}}`
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(js), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := BoardColorsFromConfigDir(dir)
	if err != nil || c.AutocorrectHighlightColor != SetColor(12) || c.ToastFadeColors != "1,2" {
		t.Errorf("read %+v, %v", c, err)
	}
	if !strings.Contains(js, "theme") {
		t.Fatal("fixture lost its theme section")
	}
}

// Every role theme.colors has a field for can be set and cleared.
func TestBoardColorRolesAllHaveFields(t *testing.T) {
	var c ThemeColors
	for i, role := range BoardColorRoles {
		c.Override(role, i)
		if got := c.Get(role); got != SetColor(i) {
			t.Errorf("%s: Get after Override = %+v", role, got)
		}
	}
	for _, role := range BoardColorRoles {
		if !c.Inherit(role) {
			t.Errorf("%s: nothing to inherit", role)
		}
	}
	if c != (ThemeColors{}) {
		t.Errorf("after clearing every role: %+v", c)
	}
	if len(BoardColorRoles) != len(LightbarRoles)+len(CallerColorRoles)+5 {
		t.Errorf("%d board roles, want the %d lightbar roles, the %d caller roles and 5 others", len(BoardColorRoles), len(LightbarRoles), len(CallerColorRoles))
	}
}
