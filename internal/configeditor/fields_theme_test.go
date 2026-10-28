package configeditor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

const themeShipped = `{"yesNoHighlightColor": 31, "yesNoRegularColor": 13, "chatSysopColor": 11, "chatUserColor": 10, "sysopChatScreen": "CHAT1"}`

// sauceRecord is a 128-byte SAUCE record with the given title, author and
// group.
func sauceRecord(title, author, group string) string {
	rec := make([]byte, 128)
	copy(rec, "SAUCE00")
	copy(rec[7:42], title)
	copy(rec[42:62], author)
	copy(rec[62:82], group)
	return string(rec)
}

// newThemeModel builds an editor over <tmp>/configs beside a shipped menu
// set <tmp>/menus/v3 with theme.json and two chat screens, one of them only
// in the overlay. It returns the model and the BBS root.
func newThemeModel(t *testing.T) (Model, string) {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"menus/v3/theme.json":         themeShipped,
		"menus/v3/cfg/CHAT1.json":     "{}",
		"menus/v3/cfg/MAIN.CFG":       "[]",
		"menus.d/v3/cfg/V2CHAT1.json": `{"art": "SKULL.ANS"}`,
		"menus.d/v3/ansi/SKULL.ANS":   "\x1b[0mart\x1a" + sauceRecord("ViSiON/2 Chat #1", "Posyden", "GRiM"),
		"menus/v3/ansi/CHAT1.ANS":     "\x1b[0mart, no SAUCE",
	}
	for rel, body := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, sub := range []string{"configs", "data"} {
		if err := os.MkdirAll(filepath.Join(root, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	m, err := New(filepath.Join(root, "configs"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	m.configs.V3Net.KeystorePath = filepath.Join(root, "data", "v3net.key")
	return m, root
}

// The sysop picks the chat screen from the menu set's screens, built-in or
// random, and the save goes to the overlay theme.json, not the shipped one.
func TestThemeChatScreenPickedAndSavedToOverlay(t *testing.T) {
	m, root := newThemeModel(t)
	m = press(t, m, "C", "2")
	if m.mode != modeSysConfigEdit || m.sysFields[m.editField].Label != "Chat Screen" {
		t.Fatalf("mode=%v field=%q", m.mode, m.sysFields[m.editField].Label)
	}
	wantScreen(t, m, "CHAT1")
	m = press(t, m, "enter")
	if m.mode != modeLookupPicker {
		t.Fatalf("mode = %v, want picker", m.mode)
	}
	var values []string
	for _, it := range m.pickerItems {
		values = append(values, it.Value)
	}
	if got := strings.Join(values, ","); got != "(built-in),random,CHAT1,V2CHAT1" {
		t.Fatalf("picker = %s", got)
	}
	if got := m.pickerItems[3].Display; got != "V2CHAT1 - ViSiON/2 Chat #1 by Posyden" {
		t.Errorf("titled screen shown as %q", got)
	}
	if got := m.pickerItems[2].Display; got != "CHAT1" {
		t.Errorf("untitled screen shown as %q", got)
	}
	if m.pickerItems[m.pickerCursor].Value != "CHAT1" {
		t.Fatalf("picker opened on %q, want the current CHAT1", m.pickerItems[m.pickerCursor].Value)
	}
	m = press(t, m, "up", "enter")
	if m.configs.Theme.SysopChatScreen != "random" || !m.dirty {
		t.Fatalf("screen=%q dirty=%v", m.configs.Theme.SysopChatScreen, m.dirty)
	}
	if !m.saveAll() {
		t.Fatalf("saveAll: %s", m.message)
	}
	if data, _ := os.ReadFile(filepath.Join(root, "menus", "v3", "theme.json")); string(data) != themeShipped {
		t.Errorf("shipped theme.json changed: %s", data)
	}
	got, err := config.LoadThemeConfig(filepath.Join(root, "menus", "v3"))
	if err != nil {
		t.Fatal(err)
	}
	if got.SysopChatScreen != "random" || got.YesNoHighlightColor != 31 {
		t.Errorf("theme after save = %+v", got)
	}

	// The built-in layout is stored as "".
	m = press(t, m, "enter", "home", "enter")
	if m.configs.Theme.SysopChatScreen != "" {
		t.Errorf("built-in stored as %q", m.configs.Theme.SysopChatScreen)
	}
	wantScreen(t, m, "(built-in)")
}

// Colours are DOS codes 0-255.
func TestThemeColorFieldRange(t *testing.T) {
	th := config.ThemeConfig{}
	f := themeFieldsChat(&th, func() []themeChatScreen { return nil })[1]
	if f.Type != ftInteger || f.Min != 0 || f.Max != 255 {
		t.Fatalf("sysop colour field = %+v", f)
	}
	if err := f.Set("14"); err != nil || th.ChatSysopColor != 14 || f.Get() != "14" {
		t.Fatalf("set 14: err=%v colour=%d", err, th.ChatSysopColor)
	}
}

// An untouched theme is never written, so a board gains no overlay file
// from saving something else.
func TestSaveAllLeavesUntouchedTheme(t *testing.T) {
	m, root := newThemeModel(t)
	m.dirty = true
	if !m.saveAll() {
		t.Fatalf("saveAll: %s", m.message)
	}
	if _, err := os.Stat(filepath.Join(root, "menus.d", "v3", "theme.json")); !os.IsNotExist(err) {
		t.Fatalf("overlay theme.json written for an untouched theme (stat err %v)", err)
	}
}

// A theme.json that does not parse stops the load, so a save cannot replace
// the sysop's file with defaults.
func TestBadThemeStopsLoad(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "menus", "v3"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "menus", "v3", "theme.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadAllConfigs(filepath.Join(root, "configs")); err == nil || !strings.Contains(err.Error(), "theme") {
		t.Fatalf("err = %v, want a theme load error", err)
	}
}

// The picker widens for an entry longer than its usual 50 columns, so a
// screen's full title and author show on an 80-column terminal.
func TestThemeChatScreenPickerShowsLongEntries(t *testing.T) {
	m, root := newThemeModel(t)
	for rel, body := range map[string]string{
		"menus/v3/cfg/V3CHAT1.json": "{}",
		"menus/v3/ansi/V3CHAT1.ANS": "\x1b[0mart\x1a" + sauceRecord("ViSiON/3 Framed Chat", "ViSiON/3 Dev Team", "ViSiON/3"),
	} {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(rel)), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 25})
	m = press(t, nm.(Model), "C", "2", "enter")
	wantScreen(t, m, "V3CHAT1 - ViSiON/3 Framed Chat by ViSiON/3 Dev Team")
}

// A theme field changed on disk after the editor loaded survives the
// editor's save of another field; a field both changed takes the editor's
// value and is reported.
func TestSaveThemeMergesChangesMadeElsewhere(t *testing.T) {
	m, root := newThemeModel(t)
	menus := filepath.Join(root, "menus", "v3")
	elsewhere := m.configs.Theme
	elsewhere.SysopChatScreen = "V2CHAT1"
	elsewhere.ChatUserColor = 14
	if err := config.SaveThemeConfig(menus, elsewhere); err != nil {
		t.Fatal(err)
	}

	m.configs.Theme.YesNoHighlightColor = 47
	m.configs.Theme.ChatUserColor = 12
	m.dirty = true
	if !m.saveAll() {
		t.Fatalf("saveAll: %s", m.message)
	}
	got, err := config.LoadThemeConfig(menus)
	if err != nil {
		t.Fatal(err)
	}
	if got.SysopChatScreen != "V2CHAT1" || got.YesNoHighlightColor != 47 || got.ChatUserColor != 12 {
		t.Errorf("saved theme = %+v; want V2CHAT1 kept, 47 and 12 from the editor", got)
	}
	if m.configs.Theme != got {
		t.Errorf("editor shows %+v after the save, disk has %+v", m.configs.Theme, got)
	}
	if !strings.Contains(m.message, "theme.json") {
		t.Errorf("status = %q; want the chatUserColor conflict reported", m.message)
	}
}
