package configeditor

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ViSiON-3/vision-3-bbs/internal/conference"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
)

// newTUIModel builds an editor Model over an empty temp config dir and seeds
// it with a conference and message area so record screens have data.
func newTUIModel(t *testing.T) Model {
	t.Helper()
	m, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	m.configs.Conferences = []conference.Conference{
		{ID: 1, Position: 1, Tag: "LOCAL", Name: "Local"},
	}
	m.configs.MsgAreas = []message.MessageArea{
		{ID: 1, Position: 1, Tag: "GENERAL", Name: "General", AreaType: "local", ConferenceID: 1},
	}
	return m
}

// asModel asserts that a tea.Model returned by Update is this package's
// Model, failing the test instead of panicking on a mismatch.
func asModel(t *testing.T, m tea.Model) Model {
	t.Helper()
	got, ok := m.(Model)
	if !ok {
		t.Fatalf("Update returned %T, want Model", m)
	}
	return got
}

// hit sends a key message and returns the updated Model.
func hit(t *testing.T, m Model, msg tea.KeyMsg) Model {
	t.Helper()
	updated, _ := m.Update(msg)
	return asModel(t, updated)
}

func TestTopMenuNavigationAndSelect(t *testing.T) {
	m := newTUIModel(t)
	if m.mode != modeTopMenu {
		t.Fatalf("initial mode = %v, want topMenu", m.mode)
	}

	m = hit(t, m, tea.KeyMsg{Type: tea.KeyDown})
	if m.topCursor != 1 {
		t.Errorf("cursor = %d, want 1", m.topCursor)
	}
	m = hit(t, m, tea.KeyMsg{Type: tea.KeyEnd})
	if m.topCursor != len(m.topItems)-1 {
		t.Errorf("cursor = %d, want last", m.topCursor)
	}
	m = hit(t, m, tea.KeyMsg{Type: tea.KeyHome})
	m = hit(t, m, tea.KeyMsg{Type: tea.KeyUp}) // clamped at 0
	if m.topCursor != 0 {
		t.Errorf("cursor = %d, want 0", m.topCursor)
	}

	// Enter on item 0 opens the system-config submenu.
	m = hit(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.mode != modeSysConfigMenu {
		t.Fatalf("mode = %v, want sysConfigMenu", m.mode)
	}

	// Hotkey selection: "6" jumps straight to the protocols record list.
	m.mode = modeTopMenu
	m = hit(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("6")})
	if m.mode != modeRecordList || m.recordType != "protocol" {
		t.Errorf("mode/type = %v/%q, want recordList/protocol", m.mode, m.recordType)
	}
	// Escape returns to the top menu.
	m = hit(t, m, tea.KeyMsg{Type: tea.KeyEscape})
	if m.mode != modeTopMenu {
		t.Errorf("mode = %v, want topMenu", m.mode)
	}
}

func TestCategoryMenuToRecordEdit(t *testing.T) {
	m := newTUIModel(t)

	// Item 1: Areas and Conferences → category menu.
	m.topCursor = 1
	m = hit(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.mode != modeCategoryMenu || len(m.catMenuItems) != 3 {
		t.Fatalf("mode/items = %v/%d, want categoryMenu/3", m.mode, len(m.catMenuItems))
	}

	// Select "Message Areas" → record list of msgareas.
	m = hit(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.mode != modeRecordList || m.recordType != "msgarea" {
		t.Fatalf("mode/type = %v/%q, want recordList/msgarea", m.mode, m.recordType)
	}

	// Enter opens the record editor with built fields.
	m = hit(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.mode != modeRecordEdit || len(m.recordFields) == 0 {
		t.Fatalf("mode/fields = %v/%d, want recordEdit with fields", m.mode, len(m.recordFields))
	}
}

func TestExitConfirmFlow(t *testing.T) {
	m := newTUIModel(t)

	// Clean model prompts "Exit? Y/N" on Escape (no immediate quit).
	m2 := hit(t, m, tea.KeyMsg{Type: tea.KeyEscape})
	if m2.mode != modeQuitConfirm {
		t.Fatalf("clean exit mode = %v, want quitConfirm", m2.mode)
	}
	// Y confirms and quits.
	updated, cmd := m2.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	asModel(t, updated)
	if cmd == nil || cmd() != (tea.QuitMsg{}) {
		t.Fatalf("Y on quit confirm should return tea.QuitMsg")
	}

	// Dirty model prompts first; Escape on the dialog cancels back to top menu.
	m = newTUIModel(t)
	m.dirty = true
	m = hit(t, m, tea.KeyMsg{Type: tea.KeyEscape})
	if m.mode != modeExitConfirm || !m.confirmYes {
		t.Fatalf("mode/yes = %v/%v, want exitConfirm/true", m.mode, m.confirmYes)
	}
	m = hit(t, m, tea.KeyMsg{Type: tea.KeyLeft}) // toggle to No
	if m.confirmYes {
		t.Error("left/right should toggle confirmYes")
	}
	m = hit(t, m, tea.KeyMsg{Type: tea.KeyEscape})
	if m.mode != modeTopMenu {
		t.Errorf("mode = %v, want topMenu after cancel", m.mode)
	}
}

func TestQuitConfirm_NoStaysInMenu(t *testing.T) {
	m := newTUIModel(t)
	m2 := hit(t, m, tea.KeyMsg{Type: tea.KeyEscape}) // clean -> quit confirm
	if m2.mode != modeQuitConfirm {
		t.Fatalf("mode = %v, want quitConfirm", m2.mode)
	}
	m3 := hit(t, m2, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	if m3.mode != modeTopMenu {
		t.Fatalf("N should return to top menu, got %v", m3.mode)
	}
}

func TestQuitConfirm_DirtyStillUsesSavePrompt(t *testing.T) {
	m := newTUIModel(t)
	m.dirty = true
	m2 := hit(t, m, tea.KeyMsg{Type: tea.KeyEscape})
	if m2.mode != modeExitConfirm {
		t.Fatalf("dirty exit mode = %v, want exitConfirm (unchanged)", m2.mode)
	}
}

func TestQuitConfirm_ViewShowsExitPrompt(t *testing.T) {
	m := newTUIModel(t)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = asModel(t, updated)
	m.mode = modeQuitConfirm
	if !strings.Contains(m.View(), "Exit?") {
		t.Fatal("quit confirm view should show the Exit? prompt")
	}
}

func TestConfigEditorViewSmoke(t *testing.T) {
	m := newTUIModel(t)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 32})
	m = asModel(t, updated)

	// Prepare state for the record screens.
	m.recordType = "msgarea"
	m.recordEditIdx = 0
	m.recordFields = m.buildRecordFields()
	m.sysFields = m.buildSysFields(0)
	m.catMenuTitle = "Areas and Conferences"
	m.catMenuItems = []categoryMenuItem{{Label: "Message Areas", RecordType: "msgarea"}}

	modes := []editorMode{
		modeTopMenu, modeCategoryMenu, modeSysConfigMenu, modeSysConfigEdit,
		modeRecordList, modeRecordEdit, modeExitConfirm, modeDeleteConfirm, modeHelp,
	}
	for _, mode := range modes {
		mm := m
		mm.mode = mode
		if out := mm.View(); out == "" {
			t.Errorf("View() in mode %v returned empty output", mode)
		}
	}
}
