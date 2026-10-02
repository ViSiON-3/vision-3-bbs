package usereditor

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// The WFC read-only flag is a Y/N field that reaches users.json.
func TestWFCReadOnlyFieldIsSaved(t *testing.T) {
	m, path := editBob(t)
	m = focusField(t, m, "WFC Read Only")
	m = press(t, m, key(tea.KeyEnter), char('y'))
	if m.mode != modeEdit || !m.users[m.editIndex].WFCReadOnly {
		t.Fatalf("y: mode=%v readOnly=%v", m.mode, m.users[m.editIndex].WFCReadOnly)
	}
	m = press(t, m, key(tea.KeyEscape), key(tea.KeyEnter))
	if m.mode != modeList {
		t.Fatalf("after saving: mode=%v message=%q", m.mode, m.message)
	}
	if got, _ := reloadUser(t, path, 2); !got.WFCReadOnly {
		t.Fatal("read-only flag did not reach disk")
	}
}
