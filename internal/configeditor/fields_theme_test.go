package configeditor

import (
	"encoding/json"
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
	m = press(t, m, "C", "5")
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

// The Theme selector lists the directories under menus/, stores the choice
// as config.json's theme.name (the default as an omitted name), and leaves
// the theme the editor opened on as the one its colour screens edit.
func TestThemeSelectorPicksFromMenusDirs(t *testing.T) {
	m, root := newThemeModel(t)
	if err := os.MkdirAll(filepath.Join(root, "menus", "amber"), 0o755); err != nil {
		t.Fatal(err)
	}
	m = press(t, m, "C", "1")
	if m.mode != modeSysConfigEdit || m.sysFields[m.editField].Label != "Theme" {
		t.Fatalf("mode=%v field=%q", m.mode, m.sysFields[m.editField].Label)
	}
	if got := m.sysFields[m.editField].Get(); got != "v3" {
		t.Fatalf("selector shows %q with no theme.name, want v3", got)
	}
	m = press(t, m, "enter")
	var values []string
	for _, it := range m.pickerItems {
		values = append(values, it.Value)
	}
	if got := strings.Join(values, ","); got != "amber,v3" {
		t.Fatalf("picker = %s, want amber,v3", got)
	}
	m = press(t, m, "home", "enter")
	if m.configs.Server.Theme.Name != "amber" || !m.dirty {
		t.Fatalf("theme.name=%q dirty=%v", m.configs.Server.Theme.Name, m.dirty)
	}
	// A theme.json edit made now still belongs to v3, the theme loaded.
	m.configs.Theme.ChatSysopColor = 14
	if !m.saveAll() {
		t.Fatalf("saveAll: %s", m.message)
	}
	configDir := filepath.Join(root, "configs")
	if name, err := config.ThemeNameFromConfigDir(configDir); err != nil || name != "amber" {
		t.Errorf("config.json theme.name = %q, %v", name, err)
	}
	if got, err := config.LoadThemeConfig(filepath.Join(root, "menus", "v3")); err != nil || got.ChatSysopColor != 14 {
		t.Errorf("v3 theme after save: %+v, %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(root, "menus.d", "amber", "theme.json")); !os.IsNotExist(err) {
		t.Errorf("v3's colours were written into amber's overlay (stat err %v)", err)
	}

	// Reopened, the editor is on amber; back to the default stores no name.
	m2, err := New(configDir)
	if err != nil {
		t.Fatal(err)
	}
	if m2.configs.themeSet != filepath.Join(configDir, "..", "menus", "amber") {
		t.Errorf("reopened editor edits %s", m2.configs.themeSet)
	}
	f := themeFieldsSelection(&m2.configs.Server, func() []string { return nil })[0]
	if err := f.Set("v3"); err != nil || m2.configs.Server.Theme.Name != "" {
		t.Errorf("default stored as %q, %v", m2.configs.Server.Theme.Name, err)
	}
	if err := f.Set("../v3"); err == nil {
		t.Error("an invalid theme name was accepted")
	}
}

// A theme.name that selects nothing still opens the editor, on the default
// theme, so the selection can be put right there.
func TestInvalidThemeSelectionStillOpens(t *testing.T) {
	_, root := newThemeModel(t)
	configDir := filepath.Join(root, "configs")
	if err := os.WriteFile(filepath.Join(configDir, "config.json"), []byte(`{"theme": {"name": "neon"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := New(configDir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if m.configs.themeSet != filepath.Join(configDir, "..", "menus", "v3") {
		t.Errorf("editing %s, want the default theme", m.configs.themeSet)
	}
	if got := themeFieldsSelection(&m.configs.Server, func() []string { return nil })[0].Get(); got != "neon" {
		t.Errorf("selector shows %q, want the configured neon", got)
	}
}

// A colour field shows the colour the board draws with and where it comes
// from. Setting it is a board override, which leaves the theme alone;
// choosing the colour already shown is not one; 0 is a colour; and Inherit
// ("Use theme default") takes the override away again.
func TestBoardColorField(t *testing.T) {
	th := config.ThemeConfig{ChatSysopColor: 11, YesNoHighlightColor: 31}
	var board config.ThemeColors
	f := themeFieldsChat(&th, &board, func() []themeChatScreen { return nil })[1]
	if f.Label != "Sysop Color" || f.Type != ftColorAttr || f.Min != 0 || f.Max != 255 {
		t.Fatalf("sysop colour field = %+v", f)
	}
	if f.Get() != "11" || f.Source() != "theme" {
		t.Fatalf("opens as %s from %q, want 11 from the theme", f.Get(), f.Source())
	}
	if err := f.Set("11"); err != nil || board != (config.ThemeColors{}) {
		t.Fatalf("setting the colour shown made an override: %+v, %v", board, err)
	}
	if err := f.Set("14"); err != nil || board.ChatSysopColor != config.SetColor(14) || th.ChatSysopColor != 11 {
		t.Fatalf("set 14: board %+v theme %d err %v", board.ChatSysopColor, th.ChatSysopColor, err)
	}
	if f.Get() != "14" || f.Source() != "board override" {
		t.Errorf("after the override: %s from %q", f.Get(), f.Source())
	}
	if err := f.Set("0"); err != nil || board.ChatSysopColor != config.SetColor(0) || f.Get() != "0" {
		t.Errorf("set 0: %+v, %v", board.ChatSysopColor, err)
	}
	for _, bad := range []string{"256", "-1", "red"} {
		if err := f.Set(bad); err == nil {
			t.Errorf("Set(%q) was accepted", bad)
		}
	}
	if !f.Inherit() || board != (config.ThemeColors{}) || f.Get() != "11" || f.Source() != "theme" {
		t.Errorf("after Inherit: board %+v, shows %s from %q", board, f.Get(), f.Source())
	}
	if f.Inherit() {
		t.Error("Inherit reported an override with none left")
	}

	// A role the theme leaves out comes from the built-in fallback.
	sel := themeFieldsLightbar(&th, &board, nil)[0]
	if sel.Label != "Selected" || sel.Get() != "31" || sel.Source() != "built-in" {
		t.Errorf("lightbar selected: %s from %q", sel.Get(), sel.Source())
	}
	hint := fieldByLabel(t, themeFieldsLightbar(&th, &board, nil), "Hint")
	if hint.Get() != "8" || hint.Source() != "built-in" {
		t.Errorf("lightbar hint: %s from %q", hint.Get(), hint.Source())
	}
}

// A colour edit in the editor is saved to config.json's theme.colors,
// holding only that colour, and never to the theme's overlay theme.json.
// The screen shows each colour's source, and D on a field removes its
// override.
func TestColourEditsGoToConfigJSON(t *testing.T) {
	m, root := newThemeModel(t)
	configJSON := filepath.Join(root, "configs", "config.json")
	overlay := filepath.Join(root, "menus.d", "v3", "theme.json")

	m = press(t, m, "C", "2")
	wantScreen(t, m, "Yes/No Highlight", "theme")
	// Foreground 15 -> 14 through the picker.
	m = press(t, m, "enter", "left", "enter")
	if got := m.configs.Server.Theme.Colors.YesNoHighlightColor; got != config.SetColor(30) || !m.dirty {
		t.Fatalf("override = %+v dirty = %v, want 30", got, m.dirty)
	}
	if m.configs.Theme.YesNoHighlightColor != 31 {
		t.Fatalf("the theme's own colour changed to %d", m.configs.Theme.YesNoHighlightColor)
	}
	wantScreen(t, m, "board override")
	if !m.saveAll() {
		t.Fatalf("saveAll: %s", m.message)
	}
	if _, err := os.Stat(overlay); !os.IsNotExist(err) {
		t.Errorf("a colour edit wrote the overlay theme.json (stat err %v)", err)
	}
	if data, _ := os.ReadFile(filepath.Join(root, "menus", "v3", "theme.json")); string(data) != themeShipped {
		t.Errorf("shipped theme.json changed: %s", data)
	}
	var saved struct {
		Theme struct {
			Colors map[string]int `json:"colors"`
		} `json:"theme"`
	}
	data, err := os.ReadFile(configJSON)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	if len(saved.Theme.Colors) != 1 || saved.Theme.Colors["yesNoHighlightColor"] != 30 {
		t.Errorf("config.json theme.colors = %v, want only yesNoHighlightColor 30", saved.Theme.Colors)
	}
	board, err := config.BoardColorsFromConfigDir(filepath.Join(root, "configs"))
	if err != nil || board.YesNoHighlightColor != config.SetColor(30) {
		t.Errorf("read back: %+v, %v", board.YesNoHighlightColor, err)
	}

	// D: use the theme's default. The key goes from config.json.
	m = press(t, m, "d")
	if m.configs.Server.Theme.Colors != (config.ThemeColors{}) || !m.dirty || !strings.Contains(m.message, "theme's default") {
		t.Fatalf("after D: %+v dirty=%v message=%q", m.configs.Server.Theme.Colors, m.dirty, m.message)
	}
	if got := m.sysFields[m.editField].Get(); got != "31" {
		t.Errorf("field shows %s after D, want the theme's 31", got)
	}
	if !m.saveAll() {
		t.Fatalf("saveAll: %s", m.message)
	}
	if data, _ := os.ReadFile(configJSON); strings.Contains(string(data), "yesNoHighlightColor") {
		t.Errorf("config.json still has the override:\n%s", data)
	}
	// D again has nothing to remove and changes nothing.
	m.dirty = false
	m = press(t, m, "d")
	if m.dirty || !strings.Contains(m.message, "already") {
		t.Errorf("second D: dirty=%v message=%q", m.dirty, m.message)
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
	m = press(t, nm.(Model), "C", "5", "enter")
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

// The Caller Colors page edits the board's default of each |Cn as a board
// override. The Bar slot unset shows the lightbar Selected colour, which it
// falls back to.
func TestCallerColorFields(t *testing.T) {
	th := config.ThemeConfig{YesNoHighlightColor: 31, CallerStat: config.SetColor(14)}
	var board config.ThemeColors
	fields := themeFieldsCaller(&th, &board, [7]string{})
	if len(fields) != len(config.CallerColorRoles) {
		t.Fatalf("%d fields for %d slots", len(fields), len(config.CallerColorRoles))
	}
	bar := fieldByLabel(t, fields, "Bar (|C7)")
	if bar.Get() != "31" || bar.Source() != "built-in" {
		t.Errorf("bar: %s from %q, want the lightbar's 31", bar.Get(), bar.Source())
	}
	stat := fieldByLabel(t, fields, "Stat (|C4)")
	if stat.Get() != "14" || stat.Source() != "theme" {
		t.Errorf("stat: %s from %q", stat.Get(), stat.Source())
	}
	if err := fieldByLabel(t, fields, "Prompt (|C1)").Set("13"); err != nil || board.CallerPrompt != config.SetColor(13) {
		t.Errorf("prompt override: %v, board %+v", err, board.CallerPrompt)
	}
}

// A slot the theme and board leave unset shows an older strings.json's
// defColorN, as the board draws it, and picking the built-in colour there
// is a change that stores an override.
func TestCallerColorFieldsLegacyDefault(t *testing.T) {
	th := config.ThemeConfig{YesNoHighlightColor: 31, CallerStat: config.SetColor(14)}
	var board config.ThemeColors
	legacy := [7]string{"|12", "", "", "|10", "", "", "|00|B6"}
	fields := themeFieldsCaller(&th, &board, legacy)
	prompt := fieldByLabel(t, fields, "Prompt (|C1)")
	if prompt.Get() != "12" || prompt.Source() != "strings.json" {
		t.Errorf("prompt: %s from %q, want strings.json's 12", prompt.Get(), prompt.Source())
	}
	if bar := fieldByLabel(t, fields, "Bar (|C7)"); bar.Get() != "48" || bar.Source() != "strings.json" {
		t.Errorf("bar: %s from %q, want strings.json's black on cyan, 48", bar.Get(), bar.Source())
	}
	// The theme's own value still wins over the legacy one.
	if stat := fieldByLabel(t, fields, "Stat (|C4)"); stat.Get() != "14" || stat.Source() != "theme" {
		t.Errorf("stat: %s from %q", stat.Get(), stat.Source())
	}
	if err := prompt.Set("7"); err != nil || board.CallerPrompt != config.SetColor(7) {
		t.Errorf("picking the built-in 7 over strings.json's 12: %v, board %+v", err, board.CallerPrompt)
	}
	if prompt.Get() != "7" || prompt.Source() != "board override" {
		t.Errorf("after the pick: %s from %q", prompt.Get(), prompt.Source())
	}
}

// Caller Choice is on while theme.callerChoice is omitted, and N stores it
// as false; Y omits it again (#1099).
func TestThemeCallerChoiceField(t *testing.T) {
	var cfg config.ServerConfig
	f := themeFieldsSelection(&cfg, func() []string { return nil })[1]
	if f.Label != "Caller Choice" || f.Get() != "Y" {
		t.Fatalf("%s = %s, want Y", f.Label, f.Get())
	}
	if err := f.Set("N"); err != nil || cfg.Theme.CallerChoice == nil || *cfg.Theme.CallerChoice || f.Get() != "N" {
		t.Errorf("after N: %v, %v", cfg.Theme.CallerChoice, err)
	}
	if err := f.Set("Y"); err != nil || cfg.Theme.CallerChoice != nil || f.Get() != "Y" {
		t.Errorf("after Y: %v, %v", cfg.Theme.CallerChoice, err)
	}
}
