package config

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// themeSet writes a shipped theme.json and, if overlay is not empty, an
// overlay one, and returns the shipped menu set path.
func themeSet(t *testing.T, shipped, overlay string) string {
	t.Helper()
	root := t.TempDir()
	base := filepath.Join(root, "menus", "v3")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	if shipped != "" {
		if err := os.WriteFile(filepath.Join(base, "theme.json"), []byte(shipped), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if overlay != "" {
		dir := filepath.Join(root, "menus.d", "v3")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "theme.json"), []byte(overlay), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return base
}

func overlayPath(base string) string {
	return filepath.Join(filepath.Dir(filepath.Dir(base)), "menus.d", "v3", "theme.json")
}

func readOverlay(t *testing.T, base string) map[string]json.RawMessage {
	t.Helper()
	data, err := os.ReadFile(overlayPath(base))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	doc := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

// An overlay key replaces the shipped one, and a key the overlay leaves
// out, or sets to null, comes from the shipped theme.json, not the built-in
// defaults (#1100).
func TestThemeOverlayMergesOverShipped(t *testing.T) {
	base := themeSet(t,
		`{"yesNoHighlightColor": 30, "chatUserColor": 2, "lightbarHint": 5, "toastFadeColors": "1,2"}`,
		`{"chatUserColor": 12, "lightbarHint": null}`)
	got, err := LoadThemeConfig(base)
	if err != nil {
		t.Fatal(err)
	}
	if got.ChatUserColor != 12 {
		t.Errorf("chatUserColor = %d; want the overlay's 12", got.ChatUserColor)
	}
	if got.YesNoHighlightColor != 30 || got.ToastFadeColors != "1,2" {
		t.Errorf("keys the overlay leaves out = %d, %q; want the shipped 30, \"1,2\"", got.YesNoHighlightColor, got.ToastFadeColors)
	}
	if got.LightbarHint != (OptColor{Set: true, Value: 5}) {
		t.Errorf("lightbarHint null in the overlay = %+v; want the shipped 5", got.LightbarHint)
	}
	if got.ChatSysopColor != builtinTheme().ChatSysopColor {
		t.Errorf("a key neither file sets = %d; want the built-in %d", got.ChatSysopColor, builtinTheme().ChatSysopColor)
	}
}

// An overlay alone, with no shipped theme.json, applies over the built-in
// defaults.
func TestThemeOverlayWithoutShipped(t *testing.T) {
	base := themeSet(t, "", `{"chatUserColor": 12}`)
	got, err := LoadThemeConfig(base)
	if err != nil {
		t.Fatal(err)
	}
	want := builtinTheme()
	want.ChatUserColor = 12
	if got != want {
		t.Errorf("loaded %+v; want %+v", got, want)
	}
}

// An unreadable overlay is an error, never a quiet fall back to the shipped
// file.
func TestThemeOverlayBadJSON(t *testing.T) {
	base := themeSet(t, `{"chatUserColor": 2}`, `{"chatUserColor": `)
	got, err := LoadThemeConfig(base)
	if err == nil {
		t.Fatal("no error for a broken overlay")
	}
	if got != builtinTheme() {
		t.Errorf("loaded %+v; want the built-in defaults", got)
	}
}

// Saving writes only what differs from the shipped theme, keeps overlay
// keys ThemeConfig does not know, and removes the overlay once it would be
// empty.
func TestSaveThemeConfigWritesOnlyDifferences(t *testing.T) {
	base := themeSet(t,
		`{"yesNoHighlightColor": 31, "sysopChatScreen": "V3CHAT1", "futureKey": [1, 2]}`,
		`{"yesNoHighlightColor": 31, "localKey": {"a": 1}, "futureKey": [1,2]}`)
	theme, err := LoadThemeConfig(base)
	if err != nil {
		t.Fatal(err)
	}
	theme.SysopChatScreen = "" // the built-in layout: a real value, not "unset"
	if err := SaveThemeConfig(base, theme); err != nil {
		t.Fatal(err)
	}
	doc := readOverlay(t, base)
	var keys []string
	for k := range doc {
		keys = append(keys, k)
	}
	if len(doc) != 2 || string(doc["sysopChatScreen"]) != `""` || doc["localKey"] == nil {
		t.Errorf("overlay keys = %v (%s); want sysopChatScreen \"\" and localKey", keys, doc["sysopChatScreen"])
	}
	if got, _ := LoadThemeConfig(base); got != theme {
		t.Errorf("loaded %+v; want %+v", got, theme)
	}

	// Back to the shipped value: only localKey is left.
	theme.SysopChatScreen = "V3CHAT1"
	if err := SaveThemeConfig(base, theme); err != nil {
		t.Fatal(err)
	}
	if doc := readOverlay(t, base); len(doc) != 1 || doc["localKey"] == nil {
		t.Errorf("overlay = %v; want only localKey", doc)
	}
}

func TestSaveThemeConfigRemovesEmptyOverlay(t *testing.T) {
	base := themeSet(t, `{"chatUserColor": 2}`, `{"chatUserColor": 9}`)
	theme, err := LoadThemeConfig(base)
	if err != nil {
		t.Fatal(err)
	}
	theme.ChatUserColor = 2
	if err := SaveThemeConfig(base, theme); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(overlayPath(base)); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("overlay still there (%v); want it removed", err)
	}
	// And with nothing to write, none is created.
	if err := SaveThemeConfig(base, theme); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(overlayPath(base)); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("overlay created (%v) for a theme equal to the shipped one", err)
	}
}

// Clearing a role that only the overlay set takes the override out.
func TestSaveThemeConfigClearsUnsetRole(t *testing.T) {
	base := themeSet(t, `{"chatUserColor": 2}`, `{"lightbarHint": 5, "chatUserColor": 9}`)
	theme, err := LoadThemeConfig(base)
	if err != nil {
		t.Fatal(err)
	}
	theme.LightbarHint = OptColor{}
	if err := SaveThemeConfig(base, theme); err != nil {
		t.Fatal(err)
	}
	if doc := readOverlay(t, base); doc["lightbarHint"] != nil || string(doc["chatUserColor"]) != "9" {
		t.Errorf("overlay = %v; want chatUserColor 9 and no lightbarHint", doc)
	}
}

// A set with no overlay (a path with no parent) is written in place, in
// full.
func TestSaveThemeConfigNoOverlay(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("theme.json", []byte(`{"futureKey": 1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	theme := builtinTheme()
	theme.ChatUserColor = 3
	if err := SaveThemeConfig(".", theme); err != nil {
		t.Fatal(err)
	}
	got, err := LoadThemeConfig(".")
	if err != nil || got != theme {
		t.Errorf("loaded %+v, %v; want %+v", got, err, theme)
	}
	data, _ := os.ReadFile("theme.json")
	doc := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &doc); err != nil || doc["futureKey"] == nil || doc["yesNoHighlightColor"] == nil {
		t.Errorf("theme.json = %s; want every key, futureKey included", data)
	}
}

func TestRedundantThemeOverlayKeysAndTrim(t *testing.T) {
	base := themeSet(t,
		`{"yesNoHighlightColor": 31, "chatUserColor": 2, "futureKey": [1, 2]}`,
		`{"yesNoHighlightColor": 31, "chatUserColor": 9, "chatSysopColor": 11, "lightbarHint": null, "futureKey": [1,2], "localKey": 1}`)
	before, err := LoadThemeConfig(base)
	if err != nil {
		t.Fatal(err)
	}
	keys, err := RedundantThemeOverlayKeys(base)
	if err != nil {
		t.Fatal(err)
	}
	// chatSysopColor 11 repeats the built-in default the shipped file leaves
	// in place.
	want := []string{"chatSysopColor", "futureKey", "lightbarHint", "yesNoHighlightColor"}
	if !reflect.DeepEqual(keys, want) {
		t.Fatalf("redundant = %v; want %v", keys, want)
	}
	if err := TrimThemeOverlay(base, keys); err != nil {
		t.Fatal(err)
	}
	if doc := readOverlay(t, base); len(doc) != 2 || doc["chatUserColor"] == nil || doc["localKey"] == nil {
		t.Errorf("overlay after trim = %v; want chatUserColor and localKey", doc)
	}
	if after, _ := LoadThemeConfig(base); after != before {
		t.Errorf("trim changed the theme: %+v; was %+v", after, before)
	}

	// Trimming the rest removes the file.
	if err := TrimThemeOverlay(base, []string{"chatUserColor", "localKey"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(overlayPath(base)); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("overlay still there (%v)", err)
	}
	if keys, err := RedundantThemeOverlayKeys(base); err != nil || keys != nil {
		t.Errorf("no overlay: %v, %v; want nothing", keys, err)
	}
}
