package configeditor

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

// isQuit reports whether cmd, when run, asks the program to exit.
func isQuit(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

// TestSysConfigMenu_Navigation pins cursor movement, digit hotkeys and the
// ways back to the top menu on the System Setup inner menu.
func TestSysConfigMenu_Navigation(t *testing.T) {
	m, _ := newDiskModel(t)
	m = press(t, m, "1")
	if m.mode != modeSysConfigMenu || m.sysMenuTitle != "System Setup" {
		t.Fatalf("mode/title = %v/%q", m.mode, m.sysMenuTitle)
	}
	wantScreen(t, m, "BBS Registration", "Logging")
	m = press(t, m, "end")
	if m.sysMenuCursor != len(m.sysMenuItems)-1 {
		t.Errorf("end: cursor = %d", m.sysMenuCursor)
	}
	m = press(t, m, "down", "home", "up")
	if m.sysMenuCursor != 0 {
		t.Errorf("home/up: cursor = %d", m.sysMenuCursor)
	}
	m = press(t, m, "down", "down")
	if m.sysMenuCursor != 2 {
		t.Errorf("down: cursor = %d", m.sysMenuCursor)
	}
	// Out-of-range digit does nothing; "9" has no item on a six-item menu.
	m = press(t, m, "9")
	if m.mode != modeSysConfigMenu {
		t.Fatalf("mode = %v after out-of-range hotkey", m.mode)
	}
	m = press(t, m, "4")
	if m.mode != modeSysConfigEdit || m.sysSubScreen != 3 {
		t.Fatalf("hotkey 4: mode=%v sub=%d", m.mode, m.sysSubScreen)
	}
	m = press(t, m, "esc", "Q")
	if m.mode != modeTopMenu {
		t.Errorf("Q: mode = %v", m.mode)
	}
	m = press(t, m, "2", "esc")
	if m.mode != modeTopMenu {
		t.Errorf("esc: mode = %v", m.mode)
	}
}

// TestSysConfigEdit_PageBetweenSubScreens pins that PgDn/PgUp step through
// the inner menu's screens and stop at either end.
func TestSysConfigEdit_PageBetweenSubScreens(t *testing.T) {
	m, _ := newDiskModel(t)
	m = press(t, m, "2", "enter") // Access & Security -> Access Levels
	if m.sysSubScreen != 0 {
		t.Fatalf("sub = %d", m.sysSubScreen)
	}
	m = press(t, m, "pgup")
	if m.sysSubScreen != 0 {
		t.Errorf("pgup at start moved to %d", m.sysSubScreen)
	}
	for i := 0; i < 10; i++ {
		m = press(t, m, "pgdown")
	}
	if m.sysSubScreen != len(m.sysMenuItems)-1 {
		t.Errorf("pgdown stops at last screen, got %d", m.sysSubScreen)
	}
	wantScreen(t, m, "New User Voting")
	m = press(t, m, "pgup")
	if m.sysSubScreen != len(m.sysMenuItems)-2 || m.editField != 0 {
		t.Errorf("pgup: sub=%d field=%d", m.sysSubScreen, m.editField)
	}
	// Up from the first field wraps to the last.
	m = press(t, m, "up")
	if m.editField != len(m.sysFields)-1 {
		t.Errorf("up wrap: field = %d, want %d", m.editField, len(m.sysFields)-1)
	}
	m = press(t, m, "down")
	if m.editField != 0 {
		t.Errorf("down wrap: field = %d", m.editField)
	}
}

// TestSysConfigEdit_StringFieldPersists pins that editing Board Name, then
// exiting with Save, writes config.json and quits.
func TestSysConfigEdit_StringFieldPersists(t *testing.T) {
	m, dir := newDiskModel(t)
	m = press(t, m, "1", "1", "enter")
	if m.mode != modeSysConfigField {
		t.Fatalf("mode = %v, want field edit", m.mode)
	}
	m = press(t, replaceText(t, m, "Test Board"), "enter")
	if m.mode != modeSysConfigEdit || m.editField != 1 || !m.dirty {
		t.Fatalf("mode=%v field=%d dirty=%v", m.mode, m.editField, m.dirty)
	}
	// Escape abandons an edit in progress.
	m = press(t, m, "enter")
	m = press(t, replaceText(t, m, "Discarded"), "esc")
	if m.configs.Server.SysOpName == "Discarded" {
		t.Error("escape applied the edit")
	}
	// Up applies and moves back.
	m = press(t, m, "enter")
	m = press(t, replaceText(t, m, "Sysop Sam"), "up")
	if m.editField != 0 || m.configs.Server.SysOpName != "Sysop Sam" {
		t.Errorf("up: field=%d sysop=%q", m.editField, m.configs.Server.SysOpName)
	}
	wantScreen(t, m, "Test Board", "Sysop Sam")

	m = press(t, m, "esc", "esc", "esc")
	if m.mode != modeExitConfirm {
		t.Fatalf("mode = %v, want exit confirm", m.mode)
	}
	wantScreen(t, m, "Unsaved Changes")
	res, cmd := m.Update(keyMsg("y"))
	if !isQuit(cmd) {
		t.Fatalf("save-and-exit did not quit: %q", asModel(t, res).message)
	}
	got, err := config.LoadServerConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.BoardName != "Test Board" || got.SysOpName != "Sysop Sam" {
		t.Errorf("saved board/sysop = %q/%q", got.BoardName, got.SysOpName)
	}
}

// TestSysConfigEdit_IntegerValidation pins that integer fields ignore
// non-digit keys and refuse out-of-range values until corrected.
func TestSysConfigEdit_IntegerValidation(t *testing.T) {
	m, _ := newDiskModel(t)
	m = press(t, m, "1", "3") // Default Settings
	for m.sysFields[m.editField].Label != "Del User Days" {
		m = press(t, m, "down")
	}
	m = press(t, m, "enter")
	m.textInput.SetValue("")
	m = typeText(t, m, "4x2")
	if m.textInput.Value() != "42" {
		t.Errorf("non-digit accepted: %q", m.textInput.Value())
	}
	m = press(t, replaceText(t, m, "99999"), "enter")
	if m.mode != modeSysConfigField || m.message != "Invalid: must be -1-9999" {
		t.Fatalf("mode=%v msg=%q", m.mode, m.message)
	}
	m = press(t, m, "up")
	if m.mode != modeSysConfigField {
		t.Fatal("up applied an invalid value")
	}
	m.textInput.SetValue("-")
	m = press(t, m, "enter")
	if m.message != "Invalid: not a number" {
		t.Errorf("msg = %q", m.message)
	}
	m = press(t, replaceText(t, m, "-1"), "enter")
	if m.mode != modeSysConfigEdit || m.configs.Server.DeletedUserRetentionDays != -1 {
		t.Errorf("mode=%v days=%d", m.mode, m.configs.Server.DeletedUserRetentionDays)
	}
}

// TestSysConfigEdit_YesNoToggle pins that Enter and Space flip a Y/N field
// and mark the config dirty.
func TestSysConfigEdit_YesNoToggle(t *testing.T) {
	m, _ := newDiskModel(t)
	m = press(t, m, "1", "3")
	before := m.configs.Server.AllowNewUsers
	m = press(t, m, "enter")
	if m.configs.Server.AllowNewUsers == before || !m.dirty {
		t.Fatal("enter did not toggle Allow New Users")
	}
	m = press(t, m, "space")
	if m.configs.Server.AllowNewUsers != before {
		t.Error("space did not toggle back")
	}
	// Space on a non-Y/N field does nothing.
	m = press(t, m, "1")
	m.mode = modeSysConfigMenu
	m = press(t, m, "1", "space")
	if m.mode != modeSysConfigEdit {
		t.Errorf("mode = %v", m.mode)
	}
}

// TestSysConfigEdit_LookupPickerSetsValue pins the Timezone picker: Escape
// leaves the value alone, Enter stores the highlighted choice.
func TestSysConfigEdit_LookupPickerSetsValue(t *testing.T) {
	m, _ := newDiskModel(t)
	m.configs.Server.Timezone = "Mars/Olympus"
	m = press(t, m, "1", "1", "down", "down", "down")
	if m.sysFields[m.editField].Label != "Timezone" {
		t.Fatalf("field = %q", m.sysFields[m.editField].Label)
	}
	m = press(t, m, "enter")
	if m.mode != modeLookupPicker || m.pickerReturnMode != modeSysConfigEdit {
		t.Fatalf("mode = %v, want picker", m.mode)
	}
	// An unknown current zone is appended and preselected.
	last := m.pickerItems[len(m.pickerItems)-1]
	if last.Value != "Mars/Olympus" || m.pickerCursor != len(m.pickerItems)-1 {
		t.Errorf("current zone item=%+v cursor=%d", last, m.pickerCursor)
	}
	wantScreen(t, m, "Mars/Olympus (current)")
	m = press(t, m, "esc")
	if m.mode != modeSysConfigEdit || m.configs.Server.Timezone != "Mars/Olympus" || m.dirty {
		t.Fatalf("esc changed value: mode=%v tz=%q", m.mode, m.configs.Server.Timezone)
	}
	m = press(t, m, "enter", "home", "down", "pgdown", "pgup", "end", "up")
	want := m.pickerItems[m.pickerCursor].Value
	m = press(t, m, "enter")
	if m.configs.Server.Timezone != want || !m.dirty || m.mode != modeSysConfigEdit {
		t.Errorf("tz=%q want %q dirty=%v mode=%v", m.configs.Server.Timezone, want, m.dirty, m.mode)
	}
}

// TestLookupPicker_EmptyListOnlyEscapes pins that an empty picker ignores
// everything but Escape.
func TestLookupPicker_EmptyListOnlyEscapes(t *testing.T) {
	m, _ := newDiskModel(t)
	m.mode = modeLookupPicker
	m.pickerReturnMode = modeSysConfigEdit
	m = press(t, m, "enter", "down")
	if m.mode != modeLookupPicker {
		t.Fatalf("mode = %v", m.mode)
	}
	m = press(t, m, "esc")
	if m.mode != modeSysConfigEdit {
		t.Errorf("mode = %v", m.mode)
	}
}

// TestLookupPicker_ScrollKeepsCursorVisible pins picker scrolling over a list
// longer than the popup.
func TestLookupPicker_ScrollKeepsCursorVisible(t *testing.T) {
	m, _ := newDiskModel(t)
	for i := 0; i < 25; i++ {
		m.pickerItems = append(m.pickerItems, LookupItem{Value: string(rune('a' + i)), Display: string(rune('A' + i))})
	}
	m.mode = modeLookupPicker
	m.pickerReturnMode = modeSysConfigEdit
	m = press(t, m, "pgdown", "pgdown")
	if m.pickerCursor != 20 || m.pickerScroll != 11 {
		t.Errorf("cursor=%d scroll=%d, want 20/11", m.pickerCursor, m.pickerScroll)
	}
	m = press(t, m, "pgdown", "end")
	if m.pickerCursor != 24 || m.pickerScroll != 15 {
		t.Errorf("cursor=%d scroll=%d, want 24/15", m.pickerCursor, m.pickerScroll)
	}
	m = press(t, m, "pgup", "pgup", "pgup")
	if m.pickerCursor != 0 || m.pickerScroll != 0 {
		t.Errorf("cursor=%d scroll=%d, want 0/0", m.pickerCursor, m.pickerScroll)
	}
}

// TestSysConfigEdit_EmptyScreenEscapes pins that a sub-screen with no fields
// only responds to Escape.
func TestSysConfigEdit_EmptyScreenEscapes(t *testing.T) {
	m, _ := newDiskModel(t)
	m.mode = modeSysConfigEdit
	m.sysFields = nil
	m = press(t, m, "enter")
	if m.mode != modeSysConfigEdit {
		t.Fatalf("mode = %v", m.mode)
	}
	m = press(t, m, "esc")
	if m.mode != modeSysConfigMenu {
		t.Errorf("mode = %v", m.mode)
	}
}

// TestSysConfigEdit_DisplayFieldSkips pins that Enter on a read-only field
// moves to the next field rather than opening an editor.
func TestSysConfigEdit_DisplayFieldSkips(t *testing.T) {
	m, _ := newDiskModel(t)
	m.mode = modeSysConfigEdit
	m.sysFields = []fieldDef{
		{Label: "Info", Type: ftDisplay, Row: 1, Get: func() string { return "x" }},
		{Label: "Name", Type: ftString, Row: 2, Width: 10, Get: func() string { return "" }, Set: func(string) error { return nil }},
	}
	m = press(t, m, "enter")
	if m.mode != modeSysConfigEdit || m.editField != 1 {
		t.Errorf("mode=%v field=%d", m.mode, m.editField)
	}
}
