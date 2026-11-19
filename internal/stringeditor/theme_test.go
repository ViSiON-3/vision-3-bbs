package stringeditor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// A theme's strings.json opened in the editor: a key it leaves out shows
// what the board prints, not the shipped text; a save leaves out values
// that print the same as the board's and keeps the theme's own, the
// shipped wording among them where the board has its own; and the file
// is written into a directory that didn't exist yet.
func TestEditThemeStrings(t *testing.T) {
	root := t.TempDir()
	board := filepath.Join(root, "configs", "strings.json")
	if err := os.MkdirAll(filepath.Dir(board), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(board, []byte(`{"pauseString": "board pause", "defPrompt": "board prompt"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	shipped := map[string]string{"pauseString": "factory pause", "defPrompt": "factory prompt", "konfigTitle": "Konfig"}

	inherited, err := InheritedStrings(board)
	if err != nil {
		t.Fatal(err)
	}
	theme := filepath.Join(root, "menus.d", "amber", "strings.json")
	m, err := NewForTheme(theme, shipped, inherited)
	if err != nil {
		t.Fatal(err)
	}
	if got := m.getValue("pauseString"); got != "board pause" {
		t.Errorf("pauseString shows %q, want the board's", got)
	}
	if got := m.getValue("konfigTitle"); got != "Konfig" {
		t.Errorf("konfigTitle shows %q, want the shipped text the board leaves", got)
	}

	m.values["pauseString"] = "board pause"  // same as the board's: not written
	m.values["defPrompt"] = "factory prompt" // shipped text over the board's own: written
	m.values["konfigTitle"] = "Amber Konfig" // the theme's own: written
	if err := m.save(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(theme)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	if _, ok := doc["pauseString"]; ok {
		t.Errorf("a value the board prints was written: %s", data)
	}
	for _, k := range []string{"defPrompt", "konfigTitle"} {
		if _, ok := doc[k]; !ok {
			t.Errorf("%s missing: %s", k, data)
		}
	}

	// Opened again, the shipped wording the theme holds is still its own.
	m, err = NewForTheme(theme, shipped, inherited)
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := m.values["defPrompt"]; !ok || v != "factory prompt" {
		t.Errorf("defPrompt on reopening = %q, %v; want the theme's shipped wording", v, ok)
	}
}

// InheritedStrings layers each file over the one before it, by the key
// each spelling sets.
func TestInheritedStringsLayers(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.json")
	b := filepath.Join(dir, "b.json")
	if err := os.WriteFile(a, []byte(`{"pauseString": "a", "defPrompt": "a"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte(`{"PauseString": "b"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := InheritedStrings(a, b, filepath.Join(dir, "missing.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got["pauseString"] != "b" || got["defPrompt"] != "a" {
		t.Errorf("inherited = %v", got)
	}
}

// A board file holding shipped text is the default and New drops it; a
// theme's file holding the same text keeps it, as the theme may use it to
// put the stock wording back over the board's.
func TestThemeKeepsShippedText(t *testing.T) {
	path := filepath.Join(t.TempDir(), "strings.json")
	if err := os.WriteFile(path, []byte(`{"konfigTitle": "User Konfig"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	board, err := New(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := board.values["konfigTitle"]; ok {
		t.Error("New kept shipped text; the fixture no longer tests the theme case")
	}
	theme, err := NewForTheme(path, nil, map[string]string{"konfigTitle": "Board Konfig"})
	if err != nil {
		t.Fatal(err)
	}
	if v := theme.values["konfigTitle"]; v != "User Konfig" {
		t.Errorf("NewForTheme: konfigTitle = %q, want the theme's shipped text", v)
	}
}

// A null in an inherited file is an unset key, so the shipped text shows,
// as the BBS prints it.
func TestInheritedStringsNullIsUnset(t *testing.T) {
	board := filepath.Join(t.TempDir(), "strings.json")
	if err := os.WriteFile(board, []byte(`{"pauseString": null}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := InheritedStrings(board)
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := got["pauseString"]; ok {
		t.Errorf("pauseString inherited as %q; want it unset", v)
	}
}

// A null in the file being edited is an unset key, as the BBS reads it:
// the editor shows the inherited value, and a save writes the null back.
func TestEditedFileNullIsUnset(t *testing.T) {
	path := filepath.Join(t.TempDir(), "strings.json")
	if err := os.WriteFile(path, []byte(`{"pauseString": null}`), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := NewForTheme(path, nil, map[string]string{"pauseString": "board pause"})
	if err != nil {
		t.Fatal(err)
	}
	if got := m.getValue("pauseString"); got != "board pause" {
		t.Errorf("pauseString shows %q, want the inherited value", got)
	}
	if err := m.save(); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	if string(doc["pauseString"]) != "null" {
		t.Errorf("saved pauseString = %s, want null kept: %s", doc["pauseString"], data)
	}
}

// A value in an inherited file that doesn't fit its call site is skipped,
// as the BBS skips it, and the file beneath shows.
func TestInheritedStringsSkipsFormatMismatch(t *testing.T) {
	dir := t.TempDir()
	board := filepath.Join(dir, "board.json")
	theme := filepath.Join(dir, "theme.json")
	if err := os.WriteFile(board, []byte(`{"addedToBatchFormat": "board %s"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(theme, []byte(`{"addedToBatchFormat": "%d %d"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := InheritedStrings(board, theme)
	if err != nil {
		t.Fatal(err)
	}
	if got["addedToBatchFormat"] != "board %s" {
		t.Errorf("addedToBatchFormat = %q, want the board's", got["addedToBatchFormat"])
	}
}

// Two other spellings of one key resolve as the BBS resolves them for a
// theme's file: the one that sorts first, every time.
func TestKeySpellingsResolveLikeTheBBS(t *testing.T) {
	path := filepath.Join(t.TempDir(), "strings.json")
	if err := os.WriteFile(path, []byte(`{"PauseString": "second", "PAUSESTRING": "first"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		m, err := NewForTheme(path, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if got := m.values["pauseString"]; got != "first" {
			t.Fatalf("open %d: pauseString = %q, want the PAUSESTRING spelling's", i, got)
		}
	}
}

// A key with no shipped default that the board sets by hand is a default
// for a theme's file: F4 restores the board's text rather than refusing.
func TestThemeF4RestoresBoardOnlyKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "strings.json")
	if err := os.WriteFile(path, []byte(`{"uploadMsgStr": "theme text"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := NewForTheme(path, map[string]string{}, map[string]string{"uploadMsgStr": "board text"})
	if err != nil {
		t.Fatal(err)
	}
	if def, ok := m.defaultFor("uploadMsgStr"); !ok || def != "board text" {
		t.Fatalf("defaultFor = %q, %v; want the board's text", def, ok)
	}
	m.cursor = indexOfKey(t, m, "uploadMsgStr")
	m = key2(t, m, tea.KeyMsg{Type: tea.KeyF4})
	m = key2(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	if got := m.getValue("uploadMsgStr"); got != "board text" {
		t.Errorf("after F4 uploadMsgStr = %q, want the board's text", got)
	}
}
