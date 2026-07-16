package menueditor

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// newTestEditor builds a Model over a menu base seeded with menus ALPHA and BETA.
func newTestEditor(t *testing.T) Model {
	t.Helper()
	base := newMenuBase(t)
	for _, name := range []string{"ALPHA", "BETA"} {
		if err := CreateMenu(base, name); err != nil {
			t.Fatalf("CreateMenu(%s): %v", name, err)
		}
	}
	m, err := New(base)
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
