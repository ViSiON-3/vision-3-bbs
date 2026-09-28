package menueditor

import (
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/menuset"

	tea "github.com/charmbracelet/bubbletea"
)

// newTestEditor builds a Model over a menu base seeded with menus ALPHA and BETA.
func newTestEditor(t *testing.T) Model {
	t.Helper()
	base := newMenuBase(t)
	for _, name := range []string{"ALPHA", "BETA"} {
		if err := CreateMenu(menuset.Bare(base), name); err != nil {
			t.Fatalf("CreateMenu(%s): %v", name, err)
		}
	}
	m, err := New(menuset.Bare(base))
	if err != nil {
		t.Fatalf("New: %v", err)
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

// press sends a key message and returns the updated Model.
func press(t *testing.T, m Model, msg tea.KeyMsg) Model {
	t.Helper()
	updated, _ := m.Update(msg)
	return asModel(t, updated)
}

// typeText sends s as a runes key message and returns the updated Model.
func typeText(t *testing.T, m Model, s string) Model {
	t.Helper()
	return press(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)})
}

// keys presses each special key in order and returns the updated Model.
func keys(t *testing.T, m Model, ks ...tea.KeyType) Model {
	t.Helper()
	for _, k := range ks {
		m = press(t, m, tea.KeyMsg{Type: k})
	}
	return m
}

// sendKey presses one special key and returns the updated Model together with
// the command Update produced.
func sendKey(t *testing.T, m Model, k tea.KeyType) (Model, tea.Cmd) {
	t.Helper()
	updated, cmd := m.Update(tea.KeyMsg{Type: k})
	return asModel(t, updated), cmd
}

// quits reports whether cmd, when run, asks the program to exit.
func quits(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

// retype opens the focused field with Enter, erases its current value and
// types s, leaving the input open. It works on both edit screens.
func retype(t *testing.T, m Model, s string) Model {
	t.Helper()
	m = keys(t, m, tea.KeyEnter)
	if m.mode != modeMenuEditField && m.mode != modeCommandEditField {
		t.Fatalf("Enter did not open a field, mode = %v", m.mode)
	}
	for range m.textInput.Value() {
		m = keys(t, m, tea.KeyBackspace)
	}
	if s == "" {
		return m
	}
	return typeText(t, m, s)
}
