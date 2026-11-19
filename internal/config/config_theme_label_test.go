package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// A theme states its name and description in theme.json; the board's
// config.json theme.labels overrides either, field by field, for a theme
// by directory; a theme that states neither is its directory name with no
// description.
func TestThemeLabelFor(t *testing.T) {
	menus := t.TempDir()
	write := func(rel, body string) {
		t.Helper()
		p := filepath.Join(menus, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("menus/neon/theme.json", `{"name": "Neon", "description": "Neon signs: lettering, scripted menus"}`)
	write("menus/plain/theme.json", `{"yesNoHighlightColor": 31}`)
	write("menus/v3/theme.json", `{}`)
	write("menus/glass/theme.json", `{"name": "Glass", "description": "Theme's own words"}`)
	write("menus/odd/theme.json", "{\"name\": \"  Odd\\u001b[2J \", \"description\": \"line\\none\"}")
	write("menus.d/glass/theme.json", `{"description": "Overlay words"}`)

	var sel ThemeSelection
	if err := json.Unmarshal([]byte(`{"name": "neon", "labels": {
		"v3": {"name": "V3-Stock", "description": "Stock ViSiON/3 menus, as shipped"},
		"glass": {"name": "Board Glass"}}}`), &sel); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ dir, name, desc string }{
		{"neon", "Neon", "Neon signs: lettering, scripted menus"},
		{"plain", "plain", ""},
		{"missing", "missing", ""},
		{"v3", "V3-Stock", "Stock ViSiON/3 menus, as shipped"},
		{"glass", "Board Glass", "Overlay words"},
		{"odd", "Odd[2J", "lineone"},
	} {
		got := ThemeLabelFor(filepath.Join(menus, "menus", tc.dir), sel)
		if got.Name != tc.name || got.Description != tc.desc {
			t.Errorf("%s: got %+v, want %q, %q", tc.dir, got, tc.name, tc.desc)
		}
	}

	// config.json round-trips the labels.
	data, err := json.Marshal(sel)
	if err != nil {
		t.Fatal(err)
	}
	var back ThemeSelection
	if err := json.Unmarshal(data, &back); err != nil || back.Labels["v3"].Name != "V3-Stock" {
		t.Errorf("round trip: %s -> %+v (%v)", data, back, err)
	}
}

// Saving the theme's colours keeps its name and description.
func TestSaveThemeConfigKeepsLabel(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "menus", "glass")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "theme.json"), []byte(`{"name": "Glass", "description": "Clear"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	theme, err := LoadThemeConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	theme.ChatUserColor = 12
	if err := SaveThemeConfig(dir, theme); err != nil {
		t.Fatal(err)
	}
	if got := ThemeLabelFor(dir, ThemeSelection{}); got.Name != "Glass" || got.Description != "Clear" {
		t.Errorf("after a save: %+v", got)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(filepath.Dir(dir)), "menus.d", "glass", "theme.json")); err != nil {
		t.Fatalf("overlay not written: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(filepath.Dir(filepath.Dir(dir)), "menus.d", "glass", "theme.json"))
	var doc map[string]any
	if json.Unmarshal(data, &doc) != nil || doc["name"] != nil || doc["description"] != nil {
		t.Errorf("overlay repeats the name or description: %s", data)
	}
}
