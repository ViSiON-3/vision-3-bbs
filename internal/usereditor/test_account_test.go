package usereditor

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// The sysop sets and clears the test-account flag in ./ue, and each change
// reaches users.json.
func TestTestAccountFieldIsSaved(t *testing.T) {
	m, path := editBob(t)
	for _, want := range []bool{true, false} {
		answer := 'n'
		if want {
			answer = 'y'
		}
		m = focusField(t, m, "Test Account")
		m = press(t, m, key(tea.KeyEnter), char(answer))
		if m.mode != modeEdit || m.users[m.editIndex].TestAccount != want {
			t.Fatalf("%c: mode=%v testAccount=%v", answer, m.mode, m.users[m.editIndex].TestAccount)
		}
		m = press(t, m, key(tea.KeyEscape), key(tea.KeyEnter))
		if m.mode != modeList {
			t.Fatalf("after saving: mode=%v message=%q", m.mode, m.message)
		}
		if got, _ := reloadUser(t, path, 2); got.TestAccount != want {
			t.Fatalf("TestAccount on disk = %v, want %v", got.TestAccount, want)
		}
		m = press(t, m, key(tea.KeyEnter)) // back into Bob's record
		if m.mode != modeEdit {
			t.Fatalf("reopening the record: mode=%v", m.mode)
		}
	}
}
