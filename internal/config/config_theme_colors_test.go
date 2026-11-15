package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeThemeFile writes theme.json into a new menu set directory, with a
// parent so the set has an overlay, and returns the set.
func writeThemeFile(t *testing.T, content string) string {
	t.Helper()
	set := filepath.Join(t.TempDir(), "menus", "v3")
	if err := os.MkdirAll(set, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(set, "theme.json"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return set
}

// A theme.json from before the roles: selected and normal take its Yes/No
// colours, and the rest take the colours the screens used to hard-code.
func TestLegacyThemeFileRoles(t *testing.T) {
	set := writeThemeFile(t, `{"yesNoHighlightColor": 79, "yesNoRegularColor": 3, "chatSysopColor": 11, "chatUserColor": 10}`)
	th, err := LoadThemeConfig(set)
	if err != nil {
		t.Fatal(err)
	}
	want := map[ColorRole]int{
		RoleLightbarSelected: 79, RoleLightbarNormal: 3, RoleLightbarCommand: 9,
		RoleLightbarHotkey: 15, RoleLightbarHint: 8, RoleLightbarBorder: 8, RoleLightbarDestructive: 4,
	}
	for _, role := range LightbarRoles {
		got, src := th.ColorWithSource(role)
		if got != want[role] || src != SourceBuiltin {
			t.Errorf("%s = %d from source %d, want %d from the built-in fallback", role, got, src, want[role])
		}
	}
	if len(want) != len(LightbarRoles) {
		t.Fatalf("test covers %d roles of %d", len(want), len(LightbarRoles))
	}
	// The existing colours are roles too, read from their own fields.
	if th.Color(RoleYesNoHighlight) != 79 || th.Color(RoleYesNoRegular) != 3 || th.Color(RoleChatSysop) != 11 {
		t.Errorf("Yes/No and chat roles: %d %d %d", th.Color(RoleYesNoHighlight), th.Color(RoleYesNoRegular), th.Color(RoleChatSysop))
	}
}

// A role set to 0 is colour 0; a role left out, or null, inherits. The two
// must not be confused: 0 is a valid attribute.
func TestRoleZeroIsNotMissing(t *testing.T) {
	set := writeThemeFile(t, `{"yesNoHighlightColor": 31, "yesNoRegularColor": 13, "lightbarSelected": 0, "lightbarHint": 0, "lightbarNormal": null}`)
	th, err := LoadThemeConfig(set)
	if err != nil {
		t.Fatal(err)
	}
	for _, role := range []ColorRole{RoleLightbarSelected, RoleLightbarHint} {
		if got, src := th.ColorWithSource(role); got != 0 || src != SourceTheme {
			t.Errorf("%s = %d from source %d, want 0 from the theme", role, got, src)
		}
	}
	if got, src := th.ColorWithSource(RoleLightbarNormal); got != 13 || src != SourceBuiltin {
		t.Errorf("a null lightbarNormal = %d from source %d, want the fallback 13", got, src)
	}
	if got := th.Color(RoleLightbarBorder); got != 8 {
		t.Errorf("an omitted lightbarBorder = %d, want 8", got)
	}

	// Written back, the 0 stays and the unset roles stay out of the file.
	out, err := json.Marshal(th)
	if err != nil {
		t.Fatal(err)
	}
	if s := string(out); !strings.Contains(s, `"lightbarSelected":0`) || strings.Contains(s, "lightbarNormal") || strings.Contains(s, "lightbarBorder") {
		t.Errorf("marshalled theme = %s", s)
	}
	if err := SaveThemeConfig(set, th); err != nil {
		t.Fatal(err)
	}
	again, err := LoadThemeConfig(set)
	if err != nil || again != th {
		t.Errorf("theme after a save and reload = %+v, %v; want %+v", again, err, th)
	}
}

// A role that isn't a colour makes the file unusable, like any other bad
// value, so it is noticed and not silently drawn as something else.
func TestRoleRejectsNonColours(t *testing.T) {
	for _, bad := range []string{`256`, `-1`, `"red"`, `1.5`, `true`} {
		set := writeThemeFile(t, `{"lightbarSelected": `+bad+`}`)
		if _, err := LoadThemeConfig(set); err == nil {
			t.Errorf("lightbarSelected %s was accepted", bad)
		}
	}
	var c OptColor
	if err := json.Unmarshal([]byte(`255`), &c); err != nil || c != SetColor(255) {
		t.Errorf("255: %+v, %v", c, err)
	}
}

// The stock theme.json sets every lightbar role, to the colour a theme
// without it would draw, so shipping the roles changes nothing on screen.
func TestStockThemeSetsEveryRole(t *testing.T) {
	th, err := LoadThemeConfig(filepath.Join("..", "..", "menus", "v3"))
	if err != nil {
		t.Fatal(err)
	}
	unset := th
	unset.LightbarSelected, unset.LightbarNormal, unset.LightbarCommand = OptColor{}, OptColor{}, OptColor{}
	unset.LightbarHotkey, unset.LightbarHint, unset.LightbarBorder, unset.LightbarDestructive = OptColor{}, OptColor{}, OptColor{}, OptColor{}
	for _, role := range LightbarRoles {
		got, src := th.ColorWithSource(role)
		if src != SourceTheme {
			t.Errorf("stock theme.json has no %s", role)
		}
		if want := unset.Color(role); got != want {
			t.Errorf("stock %s = %d, want %d, the colour without it", role, got, want)
		}
	}
}
