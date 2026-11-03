package stringeditor

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// TestPickerInsertsAtCursor opens the picker from edit mode, chooses a colour
// and checks the codes land at the cursor with the cursor after them.
func TestPickerInsertsAtCursor(t *testing.T) {
	m := newTestModel(t)
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m = key(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("ab")})
	m = key(t, m, tea.KeyMsg{Type: tea.KeyLeft}) // cursor between a and b
	m = key(t, m, tea.KeyMsg{Type: tea.KeyF2})
	if m.mode != modePicker {
		t.Fatalf("after F2 mode = %v, want modePicker", m.mode)
	}
	// Default choice is white with no background.
	if got := m.pickerCode(); got != "|15" {
		t.Fatalf("initial code = %q, want |15", got)
	}
	m = key(t, m, tea.KeyMsg{Type: tea.KeyLeft}) // |14
	m = key(t, m, tea.KeyMsg{Type: tea.KeyDown}) // background row
	m = key(t, m, tea.KeyMsg{Type: tea.KeyRight})
	m = key(t, m, tea.KeyMsg{Type: tea.KeyRight}) // -- -> B0 -> B1
	if got := m.pickerCode(); got != "|B1|14" {
		t.Fatalf("code = %q, want |B1|14", got)
	}
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.mode != modeEdit {
		t.Fatalf("after Enter mode = %v, want modeEdit", m.mode)
	}
	if got := m.textInput.Value(); got != "a|B1|14b" {
		t.Errorf("value = %q, want a|B1|14b", got)
	}
	if got := m.textInput.Position(); got != len("a|B1|14") {
		t.Errorf("cursor = %d, want %d", got, len("a|B1|14"))
	}
	// Accepting the edit stores the codes unchanged.
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if got := m.values[m.entries[m.cursor].Key]; got != "a|B1|14b" {
		t.Errorf("stored = %q", got)
	}
}

// TestPickerCancelKeepsValue checks Esc returns to the input unchanged, and
// that the choice made is remembered for the next opening.
func TestPickerCancelKeepsValue(t *testing.T) {
	m := newTestModel(t)
	m = key(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	m = key(t, m, tea.KeyMsg{Type: tea.KeyF2})
	m = key(t, m, tea.KeyMsg{Type: tea.KeyRight}) // |00
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEscape})
	if m.mode != modeEdit || m.textInput.Value() != "x" {
		t.Fatalf("after Esc: mode %v value %q", m.mode, m.textInput.Value())
	}
	m = key(t, m, tea.KeyMsg{Type: tea.KeyF2})
	if got := m.pickerCode(); got != "|00" {
		t.Errorf("choice not kept: %q", got)
	}
}

// TestPickerWraps checks the rows wrap at both ends, including the
// no-background cell.
func TestPickerWraps(t *testing.T) {
	m := newTestModel(t)
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m = key(t, m, tea.KeyMsg{Type: tea.KeyF2})
	m = key(t, m, tea.KeyMsg{Type: tea.KeyRight}) // 15 -> 0
	if m.pickFG != 0 {
		t.Errorf("fg after wrap = %d, want 0", m.pickFG)
	}
	m = key(t, m, tea.KeyMsg{Type: tea.KeyTab})
	m = key(t, m, tea.KeyMsg{Type: tea.KeyLeft}) // none -> B7
	if m.pickBG != 7 {
		t.Errorf("bg after wrap = %d, want 7", m.pickBG)
	}
	m = key(t, m, tea.KeyMsg{Type: tea.KeyRight}) // B7 -> none
	if m.pickBG != pickerNoBG {
		t.Errorf("bg after wrap = %d, want none", m.pickBG)
	}
}

// TestPickerView checks the dialog is drawn over the edit screen with the
// sample in the chosen codes, and that the edit row is still shown under it.
func TestPickerView(t *testing.T) {
	m := newTestModel(t)
	m = resize(t, m, 80, 25)
	m = key(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v")})
	m = key(t, m, tea.KeyMsg{Type: tea.KeyF2})
	view := m.View()
	for _, want := range []string{"Insert Colour", "|15 ", pickerSample, "Enter insert", helpTextEdit} {
		if !strings.Contains(view, want) {
			t.Errorf("view lacks %q", want)
		}
	}
	for _, line := range strings.Split(view, "\n") {
		if w := visualLen(line); w != 80 {
			t.Errorf("row width %d, want 80: %q", w, line)
		}
	}
}
