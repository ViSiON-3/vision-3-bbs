package menueditor

import (
	"encoding/json"
	"os"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// Regression tests for issue #457: edits lost or wrongly kept by the menu
// editor. Every assertion reads the .MNU/.CFG files back from disk.

// diskMenu decodes name's .MNU file as it stands on disk.
func diskMenu(t *testing.T, m Model, name string) MenuData {
	t.Helper()
	path, err := m.set.Resolve("mnu", name+".MNU")
	if err != nil {
		t.Fatalf("resolve %s.MNU: %v", name, err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var d MenuData
	if err := json.Unmarshal(raw, &d); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return d
}

// diskCmds decodes name's .CFG file as it stands on disk; a missing or empty
// file reads as no commands.
func diskCmds(t *testing.T, m Model, name string) []CmdData {
	t.Helper()
	path, err := m.set.Resolve("cfg", name+".CFG")
	if err != nil {
		t.Fatalf("resolve %s.CFG: %v", name, err)
	}
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) || (err == nil && len(raw) == 0) {
		return nil
	}
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var cmds []CmdData
	if err := json.Unmarshal(raw, &cmds); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return cmds
}

// editorWithCommand returns an editor whose ALPHA menu has one saved command.
func editorWithCommand(t *testing.T) Model {
	t.Helper()
	m := newTestEditor(t)
	if err := SaveCommands(m.set, "ALPHA", []CmdData{{Keys: "A", Command: "X"}}); err != nil {
		t.Fatalf("SaveCommands: %v", err)
	}
	return m
}

// editKeys opens the Keystroke(s) field of the command being edited and
// appends s to it.
func editKeys(t *testing.T, m Model, s string) Model {
	t.Helper()
	for m.cmdEditFld > 1 {
		m = press(t, m, tea.KeyMsg{Type: tea.KeyUp})
	}
	for m.cmdEditFld < 1 {
		m = press(t, m, tea.KeyMsg{Type: tea.KeyDown})
	}
	m = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.mode != modeCommandEditField {
		t.Fatalf("mode = %v, want commandEditField", m.mode)
	}
	m = typeText(t, m, s)
	return press(t, m, tea.KeyMsg{Type: tea.KeyEnter})
}

func TestCommandEditF8DiscardsFieldEdits(t *testing.T) {
	m := editorWithCommand(t)
	m = press(t, m, tea.KeyMsg{Type: tea.KeyF10}) // ALPHA command list
	m = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m = editKeys(t, m, "Z")
	if m.cmds[0].Keys != "AZ" {
		t.Fatalf("keys = %q, want AZ before abort", m.cmds[0].Keys)
	}
	m = press(t, m, tea.KeyMsg{Type: tea.KeyF8})
	if m.mode != modeCommandList {
		t.Fatalf("mode = %v, want commandList", m.mode)
	}
	if m.dirtyCmds {
		t.Error("F8 should leave the command set clean")
	}
	m = press(t, m, tea.KeyMsg{Type: tea.KeyEscape}) // leave the list

	cmds := diskCmds(t, m, "ALPHA")
	if len(cmds) != 1 || cmds[0].Keys != "A" {
		t.Errorf("ALPHA.CFG = %+v, want the original single command with keys A", cmds)
	}
}

func TestCommandEditF8DropsAddedCommand(t *testing.T) {
	m := editorWithCommand(t)
	m = press(t, m, tea.KeyMsg{Type: tea.KeyF10})
	m = press(t, m, tea.KeyMsg{Type: tea.KeyF5}) // add from the list
	m = editKeys(t, m, "Q")
	m = press(t, m, tea.KeyMsg{Type: tea.KeyF5}) // and another from the edit screen
	m = press(t, m, tea.KeyMsg{Type: tea.KeyF8})
	if len(m.cmds) != 1 {
		t.Fatalf("cmds after F8 = %+v, want only the original", m.cmds)
	}
	if m.cmdCursor != 0 {
		t.Errorf("cmdCursor = %d, want 0 (clamped to remaining list)", m.cmdCursor)
	}
	m = press(t, m, tea.KeyMsg{Type: tea.KeyEscape})

	cmds := diskCmds(t, m, "ALPHA")
	if len(cmds) != 1 || cmds[0].Keys != "A" {
		t.Errorf("ALPHA.CFG = %+v, want the original single command", cmds)
	}
}

func TestCommandEditF8KeepsEarlierKeptEdits(t *testing.T) {
	m := editorWithCommand(t)
	m = press(t, m, tea.KeyMsg{Type: tea.KeyF10})
	m = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m = editKeys(t, m, "B")                          // kept: A -> AB
	m = press(t, m, tea.KeyMsg{Type: tea.KeyEscape}) // keep, back to list
	m = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m = editKeys(t, m, "C") // discarded below
	m = press(t, m, tea.KeyMsg{Type: tea.KeyF8})
	if !m.dirtyCmds {
		t.Error("F8 must not clear changes kept before the screen opened")
	}
	m = press(t, m, tea.KeyMsg{Type: tea.KeyEscape}) // saves the list

	cmds := diskCmds(t, m, "ALPHA")
	if len(cmds) != 1 || cmds[0].Keys != "AB" {
		t.Errorf("ALPHA.CFG = %+v, want keys AB", cmds)
	}
}

// TestAddMenuKeepsUnsavedMenuEdit is the issue's repro: edit ALPHA's title,
// F5 to add GAMMA straight from the edit screen, then leave and save.
func TestAddMenuKeepsUnsavedMenuEdit(t *testing.T) {
	m := newTestEditor(t)
	m = press(t, m, tea.KeyMsg{Type: tea.KeyEnter}) // edit ALPHA
	m = press(t, m, tea.KeyMsg{Type: tea.KeyEnter}) // Title field
	m = typeText(t, m, "Edited")
	m = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m = press(t, m, tea.KeyMsg{Type: tea.KeyF5})
	m = typeText(t, m, "GAMMA")
	m = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.mode != modeMenuEdit || m.menus[m.menuEditIdx].Name != "GAMMA" {
		t.Fatalf("mode/menu = %v/%q, want menuEdit/GAMMA", m.mode, m.menus[m.menuEditIdx].Name)
	}
	m = press(t, m, tea.KeyMsg{Type: tea.KeyEscape}) // back to list
	m = press(t, m, tea.KeyMsg{Type: tea.KeyEscape}) // exit (confirm if dirty)
	if m.mode == modeExitConfirm {
		m = press(t, m, tea.KeyMsg{Type: tea.KeyEnter}) // Yes: save all
	}

	if got := diskMenu(t, m, "ALPHA").Title; got != "Edited" {
		t.Errorf("ALPHA title on disk = %q, want Edited", got)
	}
}

// TestAddMenuReloadKeepsDirtyMenus covers a menu that is still dirty when
// another is added (e.g. its save failed): the reload must not replace its
// in-memory edit with the disk copy.
func TestAddMenuReloadKeepsDirtyMenus(t *testing.T) {
	m := newTestEditor(t)
	m.menus[1].Data.Title = "Unsaved"
	m.dirtyMenus["BETA"] = true

	m = press(t, m, tea.KeyMsg{Type: tea.KeyF5}) // add from the menu list
	m = typeText(t, m, "GAMMA")
	m = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if len(m.menus) != 3 {
		t.Fatalf("menus = %d, want 3", len(m.menus))
	}
	if !m.dirtyMenus["BETA"] {
		t.Fatal("BETA should still be dirty after the reload")
	}
	m = press(t, m, tea.KeyMsg{Type: tea.KeyEscape}) // GAMMA edit -> list
	m = press(t, m, tea.KeyMsg{Type: tea.KeyEscape}) // exit confirm
	if m.mode != modeExitConfirm {
		t.Fatalf("mode = %v, want exitConfirm", m.mode)
	}
	m = press(t, m, tea.KeyMsg{Type: tea.KeyEnter}) // Yes: save all

	if got := diskMenu(t, m, "BETA").Title; got != "Unsaved" {
		t.Errorf("BETA title on disk = %q, want Unsaved", got)
	}
}

// TestDeleteRevertKeepsDirtyMenus covers the other reload: deleting an
// overridden shipped menu reverts it and re-reads the list, which must keep
// unsaved edits to the other menus.
func TestDeleteRevertKeepsDirtyMenus(t *testing.T) {
	set := newOverlaySet(t)
	if err := SaveMenu(set, "MAIN", MenuData{Title: "Mine"}); err != nil {
		t.Fatal(err)
	}
	m, err := New(set)
	if err != nil {
		t.Fatal(err)
	}
	m.menus[1].Data.Title = "Unsaved" // MSG
	m.dirtyMenus["MSG"] = true

	m = press(t, m, tea.KeyMsg{Type: tea.KeyF2}) // delete MAIN
	m = typeText(t, m, "y")
	if !m.dirtyMenus["MSG"] || m.menus[1].Data.Title != "Unsaved" {
		t.Fatalf("MSG after revert: dirty = %v, title = %q", m.dirtyMenus["MSG"], m.menus[1].Data.Title)
	}
	m = press(t, m, tea.KeyMsg{Type: tea.KeyEscape}) // exit confirm
	m = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})  // Yes: save all

	if got := diskMenu(t, m, "MSG").Title; got != "Unsaved" {
		t.Errorf("MSG title on disk = %q, want Unsaved", got)
	}
}
