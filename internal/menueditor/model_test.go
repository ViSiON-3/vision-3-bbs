package menueditor

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestMenuListNavigation(t *testing.T) {
	m := newTestEditor(t)
	if m.mode != modeMenuList || m.menuCursor != 0 {
		t.Fatalf("initial mode/cursor = %v/%d", m.mode, m.menuCursor)
	}
	m = press(t, m, tea.KeyMsg{Type: tea.KeyDown})
	if m.menuCursor != 1 {
		t.Errorf("after down: cursor = %d, want 1", m.menuCursor)
	}
	m = press(t, m, tea.KeyMsg{Type: tea.KeyDown}) // clamped at last
	if m.menuCursor != 1 {
		t.Errorf("down at end: cursor = %d, want 1", m.menuCursor)
	}
	m = press(t, m, tea.KeyMsg{Type: tea.KeyHome})
	if m.menuCursor != 0 {
		t.Errorf("after home: cursor = %d, want 0", m.menuCursor)
	}
	m = press(t, m, tea.KeyMsg{Type: tea.KeyEnd})
	if m.menuCursor != 1 {
		t.Errorf("after end: cursor = %d, want 1", m.menuCursor)
	}
	m = press(t, m, tea.KeyMsg{Type: tea.KeyPgUp})
	if m.menuCursor != 0 {
		t.Errorf("after pgup: cursor = %d, want 0", m.menuCursor)
	}
	m = press(t, m, tea.KeyMsg{Type: tea.KeyPgDown})
	if m.menuCursor != 1 {
		t.Errorf("after pgdown: cursor = %d, want 1 (clamped)", m.menuCursor)
	}
}

func TestMenuEditFieldFlow(t *testing.T) {
	m := newTestEditor(t)

	// Enter opens the menu editor on ALPHA.
	m = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.mode != modeMenuEdit || m.menuEditIdx != 0 {
		t.Fatalf("mode/idx = %v/%d, want menuEdit/0", m.mode, m.menuEditIdx)
	}

	// Edit the Title field (field 0).
	m = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.mode != modeMenuEditField {
		t.Fatalf("mode = %v, want menuEditField", m.mode)
	}
	m = typeText(t, m, "Main Menu")
	m = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.mode != modeMenuEdit {
		t.Fatalf("mode = %v, want menuEdit after apply", m.mode)
	}
	if got := m.menus[0].Data.Title; got != "Main Menu" {
		t.Errorf("title = %q, want Main Menu", got)
	}
	if !m.dirtyMenus["ALPHA"] {
		t.Error("ALPHA should be marked dirty")
	}
	if m.menuEditFld != 1 {
		t.Errorf("field cursor = %d, want 1 (advanced)", m.menuEditFld)
	}

	// Field 1 is Clear Screen (Y/N): Enter toggles in place.
	wasCLR := m.menus[0].Data.CLR
	m = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.mode != modeMenuEdit || m.menus[0].Data.CLR == wasCLR {
		t.Errorf("Y/N toggle: mode = %v, CLR = %v (was %v)", m.mode, m.menus[0].Data.CLR, wasCLR)
	}

	// Escape saves the dirty menu to disk and returns to the list.
	m = press(t, m, tea.KeyMsg{Type: tea.KeyEscape})
	if m.mode != modeMenuList {
		t.Fatalf("mode = %v, want menuList", m.mode)
	}
	if m.dirtyMenus["ALPHA"] {
		t.Error("ALPHA should be clean after save-on-escape")
	}
	menus, err := LoadMenus(m.menuBase)
	if err != nil {
		t.Fatalf("LoadMenus: %v", err)
	}
	if menus[0].Data.Title != "Main Menu" {
		t.Errorf("persisted title = %q, want Main Menu", menus[0].Data.Title)
	}
}

func TestMenuEditFieldValidation(t *testing.T) {
	m := newTestEditor(t)
	m = press(t, m, tea.KeyMsg{Type: tea.KeyEnter}) // edit ALPHA

	// Navigate to "Force Hlp Lvl" (field index 9) and enter a non-number.
	for m.menuEditFld < 9 {
		m = press(t, m, tea.KeyMsg{Type: tea.KeyDown})
	}
	m = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.mode != modeMenuEditField {
		t.Fatalf("mode = %v, want menuEditField", m.mode)
	}
	m.textInput.SetValue("abc")
	m = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.mode != modeMenuEditField {
		t.Errorf("invalid value should keep edit mode, got %v", m.mode)
	}
	if m.message == "" {
		t.Error("expected an invalid-value flash message")
	}
	// Escape abandons the field edit.
	m = press(t, m, tea.KeyMsg{Type: tea.KeyEscape})
	if m.mode != modeMenuEdit {
		t.Errorf("mode = %v, want menuEdit", m.mode)
	}
}

func TestAddMenuDialog(t *testing.T) {
	m := newTestEditor(t)

	m = press(t, m, tea.KeyMsg{Type: tea.KeyF5})
	if m.mode != modeAddMenu {
		t.Fatalf("mode = %v, want addMenu", m.mode)
	}
	m = typeText(t, m, "gamma")
	m = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.mode != modeMenuEdit {
		t.Fatalf("mode = %v, want menuEdit after create", m.mode)
	}
	if !MenuExists(m.menuBase, "GAMMA") {
		t.Error("GAMMA should exist on disk")
	}
	if m.menus[m.menuEditIdx].Name != "GAMMA" {
		t.Errorf("editing %q, want GAMMA", m.menus[m.menuEditIdx].Name)
	}

	// Creating a duplicate is rejected with a message.
	m = press(t, m, tea.KeyMsg{Type: tea.KeyF5})
	m = typeText(t, m, "GAMMA")
	m = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.mode != modeMenuList || m.message == "" {
		t.Errorf("duplicate create: mode = %v, message = %q", m.mode, m.message)
	}

	// Invalid characters are rejected.
	m = press(t, m, tea.KeyMsg{Type: tea.KeyF5})
	m = typeText(t, m, "BAD-NAME")
	m = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.mode != modeMenuList || m.message == "" {
		t.Errorf("invalid create: mode = %v, message = %q", m.mode, m.message)
	}
}

func TestDeleteMenuConfirm(t *testing.T) {
	m := newTestEditor(t)

	// F2 then N cancels.
	m = press(t, m, tea.KeyMsg{Type: tea.KeyF2})
	if m.mode != modeDeleteMenuConfirm {
		t.Fatalf("mode = %v, want deleteMenuConfirm", m.mode)
	}
	m = typeText(t, m, "n")
	if m.mode != modeMenuList || len(m.menus) != 2 {
		t.Fatalf("cancel delete: mode = %v, menus = %d", m.mode, len(m.menus))
	}

	// F2 then Y deletes ALPHA from disk and the list.
	m = press(t, m, tea.KeyMsg{Type: tea.KeyF2})
	m = typeText(t, m, "y")
	if len(m.menus) != 1 || m.menus[0].Name != "BETA" {
		t.Fatalf("after delete: menus = %+v, want just BETA", m.menus)
	}
	if MenuExists(m.menuBase, "ALPHA") {
		t.Error("ALPHA should be deleted from disk")
	}
}

func TestCommandListAndEditFlow(t *testing.T) {
	m := newTestEditor(t)

	// F10 opens the (empty) command list for ALPHA.
	m = press(t, m, tea.KeyMsg{Type: tea.KeyF10})
	if m.mode != modeCommandList || len(m.cmds) != 0 {
		t.Fatalf("mode/cmds = %v/%d, want commandList/0", m.mode, len(m.cmds))
	}

	// F5 appends a new command and opens the editor.
	m = press(t, m, tea.KeyMsg{Type: tea.KeyF5})
	if m.mode != modeCommandEdit || len(m.cmds) != 1 {
		t.Fatalf("mode/cmds = %v/%d, want commandEdit/1", m.mode, len(m.cmds))
	}

	// Edit Keystroke(s) (field 1): value is uppercased on apply.
	m = press(t, m, tea.KeyMsg{Type: tea.KeyDown})
	m = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.mode != modeCommandEditField {
		t.Fatalf("mode = %v, want commandEditField", m.mode)
	}
	m = typeText(t, m, "q")
	m = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.cmds[0].Keys != "Q" {
		t.Errorf("keys = %q, want Q (uppercased)", m.cmds[0].Keys)
	}

	// Hidden? (field 4) toggles Y/N in place.
	for m.cmdEditFld < 4 {
		m = press(t, m, tea.KeyMsg{Type: tea.KeyDown})
	}
	m = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if !m.cmds[0].Hidden {
		t.Error("Hidden should toggle to true")
	}

	// Escape back to list, then Escape saves commands and returns to menu edit.
	m = press(t, m, tea.KeyMsg{Type: tea.KeyEscape})
	if m.mode != modeCommandList {
		t.Fatalf("mode = %v, want commandList", m.mode)
	}
	m = press(t, m, tea.KeyMsg{Type: tea.KeyEscape})
	if m.mode != modeMenuEdit {
		t.Fatalf("mode = %v, want menuEdit", m.mode)
	}
	cmds, err := LoadCommands(m.menuBase, "ALPHA")
	if err != nil {
		t.Fatalf("LoadCommands: %v", err)
	}
	if len(cmds) != 1 || cmds[0].Keys != "Q" || !cmds[0].Hidden {
		t.Errorf("persisted cmds = %+v", cmds)
	}
}

func TestDeleteCommandConfirm(t *testing.T) {
	m := newTestEditor(t)
	m = press(t, m, tea.KeyMsg{Type: tea.KeyF10}) // command list
	m = press(t, m, tea.KeyMsg{Type: tea.KeyF5})  // add
	m = press(t, m, tea.KeyMsg{Type: tea.KeyEscape})
	m = press(t, m, tea.KeyMsg{Type: tea.KeyF2}) // delete highlighted
	if m.mode != modeDeleteCmdConfirm {
		t.Fatalf("mode = %v, want deleteCmdConfirm", m.mode)
	}
	m = typeText(t, m, "y")
	if len(m.cmds) != 0 || m.mode != modeCommandList {
		t.Errorf("after delete: cmds = %d, mode = %v", len(m.cmds), m.mode)
	}
}

func TestExitFlow(t *testing.T) {
	// Clean model: Escape quits immediately.
	m := newTestEditor(t)
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEscape})
	asModel(t, updated) // m is not read afterwards; assert the type only
	if cmd == nil {
		t.Error("clean exit should return a Quit command")
	}

	// Dirty model: Escape opens the exit confirm; Enter (Yes) saves all and quits.
	m = newTestEditor(t)
	m = press(t, m, tea.KeyMsg{Type: tea.KeyEnter}) // edit ALPHA
	m = press(t, m, tea.KeyMsg{Type: tea.KeyEnter}) // edit Title
	m = typeText(t, m, "Dirty")
	m = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m = press(t, m, tea.KeyMsg{Type: tea.KeyEscape}) // saves + back to list (clean)
	m = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})  // re-enter edit
	m = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})  // edit Title again
	m = typeText(t, m, "2")
	m = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m.mode = modeMenuList // exit from list with dirty state
	m = press(t, m, tea.KeyMsg{Type: tea.KeyEscape})
	if m.mode != modeExitConfirm || !m.confirmYes {
		t.Fatalf("mode/confirmYes = %v/%v, want exitConfirm/true", m.mode, m.confirmYes)
	}
	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = asModel(t, updated)
	if cmd == nil {
		t.Error("confirmed exit should return a Quit command")
	}
	if len(m.dirtyMenus) != 0 {
		t.Errorf("dirtyMenus = %v, want empty after saveAll", m.dirtyMenus)
	}
}
