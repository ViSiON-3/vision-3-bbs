package config

import (
	"os"
	"path/filepath"
	"testing"
)

// writeStringsFixture writes body to root/rel, making its directories.
func writeStringsFixture(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A theme's strings.json sets the strings it holds, over the board's; the
// rest stay the board's. The overlay's keys win over the theme's own file,
// key by key.
func TestThemeStringsOverTheBoards(t *testing.T) {
	root := t.TempDir()
	writeStringsFixture(t, root, "configs/strings.json", `{"pauseString": "board pause", "konfigTitle": "Board Konfig"}`)
	writeStringsFixture(t, root, "menus/amber/strings.json", `{"pauseString": "amber pause", "execLoginCancelled": "amber bye", "_defaults": {}}`)
	writeStringsFixture(t, root, "menus.d/amber/strings.json", `{"ExecLoginCancelled": "overlay bye"}`)

	board, err := LoadStrings(filepath.Join(root, "configs"))
	if err != nil {
		t.Fatal(err)
	}
	ts, err := LoadThemeStrings(filepath.Join(root, "menus", "amber"))
	if err != nil {
		t.Fatal(err)
	}
	if ts.Empty() {
		t.Fatal("theme strings are empty")
	}
	got := board.WithTheme(ts)
	if got.PauseString != "amber pause" {
		t.Errorf("pauseString = %q, want the theme's", got.PauseString)
	}
	if got.ExecLoginCancelled != "overlay bye" {
		t.Errorf("execLoginCancelled = %q, want the overlay's", got.ExecLoginCancelled)
	}
	if got.KonfigTitle != "Board Konfig" {
		t.Errorf("konfigTitle = %q, want the board's", got.KonfigTitle)
	}
	if board.PauseString != "board pause" {
		t.Errorf("WithTheme changed the board's strings: %q", board.PauseString)
	}
}

// A theme with no strings.json, in either layer, has no strings of its own.
func TestThemeStringsNone(t *testing.T) {
	ts, err := LoadThemeStrings(filepath.Join(t.TempDir(), "menus", "v3"))
	if err != nil {
		t.Fatal(err)
	}
	if !ts.Empty() {
		t.Error("a theme without strings.json has strings")
	}
	board := StringsConfig{PauseString: "board"}
	if got := board.WithTheme(ts); got.PauseString != "board" {
		t.Errorf("pauseString = %q, want the board's", got.PauseString)
	}
	if got := board.WithTheme(nil); got.PauseString != "board" {
		t.Errorf("nil theme strings: pauseString = %q, want the board's", got.PauseString)
	}
}

// A theme string whose format directives don't match its call site is
// left out, so the board's applies; a file that doesn't parse is an error.
func TestThemeStringsFormatMismatchAndBadFile(t *testing.T) {
	root := t.TempDir()
	writeStringsFixture(t, root, "menus/amber/strings.json", `{"addedToBatchFormat": "%d %d", "pauseString": "ok"}`)
	ts, err := LoadThemeStrings(filepath.Join(root, "menus", "amber"))
	if err != nil {
		t.Fatal(err)
	}
	got := StringsConfig{AddedToBatchFormat: "board %s"}.WithTheme(ts)
	if got.AddedToBatchFormat != "board %s" {
		t.Errorf("addedToBatchFormat = %q, want the board's", got.AddedToBatchFormat)
	}
	if got.PauseString != "ok" {
		t.Errorf("pauseString = %q, want the theme's", got.PauseString)
	}

	writeStringsFixture(t, root, "menus/broken/strings.json", `{`)
	if _, err := LoadThemeStrings(filepath.Join(root, "menus", "broken")); err == nil {
		t.Error("a strings.json that doesn't parse loaded")
	}
}

// Keys spelt in other capitals resolve the same way every time: the exact
// spelling wins, else the one that sorts first ("KONFIGTITLE" before
// "KonfigTitle"). An entry that isn't a
// string doesn't count as the exact spelling.
func TestThemeStringsKeySpellings(t *testing.T) {
	root := t.TempDir()
	writeStringsFixture(t, root, "menus/amber/strings.json",
		`{"pauseString": 42, "PauseString": "amber pause", "KonfigTitle": "first", "KONFIGTITLE": "second", "execLoginCancelled": "exact", "ExecLoginCancelled": "other"}`)
	for i := 0; i < 20; i++ {
		ts, err := LoadThemeStrings(filepath.Join(root, "menus", "amber"))
		if err != nil {
			t.Fatal(err)
		}
		got := StringsConfig{}.WithTheme(ts)
		if got.PauseString != "amber pause" || got.KonfigTitle != "second" || got.ExecLoginCancelled != "exact" {
			t.Fatalf("run %d: pauseString %q, konfigTitle %q, execLoginCancelled %q", i, got.PauseString, got.KonfigTitle, got.ExecLoginCancelled)
		}
	}
}

// A null in a theme's file is an unset key, as in configs/strings.json:
// the value beneath it applies, not an empty string.
func TestThemeStringsNullIsUnset(t *testing.T) {
	root := t.TempDir()
	writeStringsFixture(t, root, "menus/amber/strings.json", `{"pauseString": null, "konfigTitle": "Amber"}`)
	ts, err := LoadThemeStrings(filepath.Join(root, "menus", "amber"))
	if err != nil {
		t.Fatal(err)
	}
	got := StringsConfig{PauseString: "board pause"}.WithTheme(ts)
	if got.PauseString != "board pause" || got.KonfigTitle != "Amber" {
		t.Errorf("pauseString %q, konfigTitle %q; want the board's pause and amber's title", got.PauseString, got.KonfigTitle)
	}
}

// An overlay value that doesn't fit its call site is skipped, and the
// theme's own value beneath it applies.
func TestThemeStringsBadOverlayValue(t *testing.T) {
	root := t.TempDir()
	writeStringsFixture(t, root, "menus/amber/strings.json", `{"addedToBatchFormat": "amber %s"}`)
	writeStringsFixture(t, root, "menus.d/amber/strings.json", `{"addedToBatchFormat": "%d %d"}`)
	ts, err := LoadThemeStrings(filepath.Join(root, "menus", "amber"))
	if err != nil {
		t.Fatal(err)
	}
	if got := (StringsConfig{AddedToBatchFormat: "board %s"}).WithTheme(ts).AddedToBatchFormat; got != "amber %s" {
		t.Errorf("addedToBatchFormat = %q, want the theme's own", got)
	}
}
