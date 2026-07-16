package stringeditor

import (
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// newTestModel builds a Model over a temp strings.json.
func newTestModel(t *testing.T) Model {
	t.Helper()
	path := filepath.Join(t.TempDir(), "strings.json")
	m, err := New(path, map[string]string{"defPrompt": "factory"})
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

// key sends a key message and returns the updated Model.
func key(t *testing.T, m Model, msg tea.KeyMsg) Model {
	t.Helper()
	updated, _ := m.Update(msg)
	return asModel(t, updated)
}

// TestModelNavigation exercises cursor and page movement keys.
func TestModelNavigation(t *testing.T) {
	m := newTestModel(t)
	if m.cursor != 0 || m.page != 0 {
		t.Fatalf("initial cursor/page = %d/%d, want 0/0", m.cursor, m.page)
	}

	m = key(t, m, tea.KeyMsg{Type: tea.KeyDown})
	if m.cursor != 1 {
		t.Errorf("after down: cursor = %d, want 1", m.cursor)
	}
	m = key(t, m, tea.KeyMsg{Type: tea.KeyUp})
	if m.cursor != 0 {
		t.Errorf("after up: cursor = %d, want 0", m.cursor)
	}
	// Up at top is a no-op.
	m = key(t, m, tea.KeyMsg{Type: tea.KeyUp})
	if m.cursor != 0 {
		t.Errorf("up at top: cursor = %d, want 0", m.cursor)
	}

	m = key(t, m, tea.KeyMsg{Type: tea.KeyPgDown})
	if m.page != 1 || m.cursor != itemsPerPage {
		t.Errorf("after pgdown: page/cursor = %d/%d, want 1/%d", m.page, m.cursor, itemsPerPage)
	}
	m = key(t, m, tea.KeyMsg{Type: tea.KeyPgUp})
	if m.page != 0 || m.cursor != 0 {
		t.Errorf("after pgup: page/cursor = %d/%d, want 0/0", m.page, m.cursor)
	}

	m = key(t, m, tea.KeyMsg{Type: tea.KeyEnd})
	if m.cursor != len(m.entries)-1 || m.page != m.numPages-1 {
		t.Errorf("after end: cursor/page = %d/%d, want %d/%d",
			m.cursor, m.page, len(m.entries)-1, m.numPages-1)
	}
	m = key(t, m, tea.KeyMsg{Type: tea.KeyHome})
	if m.cursor != 0 || m.page != 0 {
		t.Errorf("after home: cursor/page = %d/%d, want 0/0", m.cursor, m.page)
	}
}

// TestModelEditFlow covers entering, confirming, and cancelling an edit.
func TestModelEditFlow(t *testing.T) {
	m := newTestModel(t)
	keyName := m.entries[0].Key

	// Enter starts editing the first entry.
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.mode != modeEdit || m.editKey != keyName {
		t.Fatalf("mode/editKey = %v/%q, want edit/%q", m.mode, m.editKey, keyName)
	}

	// Type a value and confirm.
	m = key(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("hi")})
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.mode != modeNavigate {
		t.Fatalf("mode = %v, want navigate", m.mode)
	}
	if m.values[keyName] != "hi" {
		t.Errorf("value = %q, want hi", m.values[keyName])
	}
	if !m.dirty {
		t.Error("model should be dirty after an edit")
	}

	// Escape cancels an edit without changing the value.
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m = key(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("junk")})
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEscape})
	if m.values[keyName] != "hi" {
		t.Errorf("cancelled edit changed value to %q", m.values[keyName])
	}

	// Typing a printable char in navigate mode starts an edit prefilled with it.
	m = key(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	if m.mode != modeEdit || m.textInput.Value() != "x" {
		t.Errorf("mode/prefill = %v/%q, want edit/x", m.mode, m.textInput.Value())
	}
}

// TestModelAbortAndRevertConfirm covers the abort and revert dialogs.
func TestModelAbortAndRevertConfirm(t *testing.T) {
	m := newTestModel(t)
	keyName := m.entries[0].Key

	// Escape opens the abort dialog; N returns to navigation.
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEscape})
	if m.mode != modeAbortConfirm {
		t.Fatalf("mode = %v, want abort confirm", m.mode)
	}
	m = key(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	if m.mode != modeNavigate {
		t.Fatalf("mode = %v, want navigate after N", m.mode)
	}

	// Edit a value, then F3 + Y reverts it to the on-disk original.
	orig := m.origValues[keyName]
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m = key(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("changed")})
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m = key(t, m, tea.KeyMsg{Type: tea.KeyF3})
	if m.mode != modeRevertConfirm {
		t.Fatalf("mode = %v, want revert confirm", m.mode)
	}
	m = key(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	if m.values[keyName] != orig {
		t.Errorf("value = %q, want reverted %q", m.values[keyName], orig)
	}
	if m.dirty {
		t.Error("dirty should be false after reverting the only change")
	}
}

// TestModelRestoreDefault covers F4 restoring a key's shipped default value.
func TestModelRestoreDefault(t *testing.T) {
	m := newTestModel(t)

	// Find the entry index for the key with a shipped default.
	idx := -1
	for i, e := range m.entries {
		if e.Key == "defPrompt" {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatal("defPrompt entry not found in metadata")
	}
	m.cursor = idx
	m.page = idx / itemsPerPage

	m = key(t, m, tea.KeyMsg{Type: tea.KeyF4})
	if m.mode != modeDefaultConfirm {
		t.Fatalf("mode = %v, want default confirm", m.mode)
	}
	m = key(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	if m.values["defPrompt"] != "factory" {
		t.Errorf("value = %q, want shipped default", m.values["defPrompt"])
	}
}

// TestModelSearch covers entering search mode, matching a key, and escaping.
func TestModelSearch(t *testing.T) {
	m := newTestModel(t)

	m = key(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	if m.mode != modeSearch {
		t.Fatalf("mode = %v, want search", m.mode)
	}
	m = key(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("pause")})
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.mode != modeNavigate {
		t.Fatalf("mode = %v, want navigate after search", m.mode)
	}
	if m.entries[m.cursor].Key != "pauseString" {
		t.Errorf("cursor on %q, want pauseString", m.entries[m.cursor].Key)
	}
	// Escape leaves search mode.
	m = key(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEscape})
	if m.mode != modeNavigate {
		t.Errorf("mode = %v, want navigate after escape", m.mode)
	}
}

// TestModelViewSmoke checks that View renders non-empty output.
func TestModelViewSmoke(t *testing.T) {
	m := newTestModel(t)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = asModel(t, updated)
	if out := m.View(); out == "" {
		t.Error("View() returned empty output")
	}
	// Dialog views render too.
	m.mode = modeAbortConfirm
	if out := m.View(); out == "" {
		t.Error("View() in confirm mode returned empty output")
	}
}
