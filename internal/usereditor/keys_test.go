package usereditor

import (
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// Key-driving helpers shared by the behavioural tests in this package.
//
// Tests reach every state through Model.Update with the messages Bubble Tea
// would deliver, rather than by assigning m.mode, so a transition that is
// broken in the product cannot be papered over by the test.

// key returns the KeyMsg for a special key such as tea.KeyEnter.
func key(k tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: k} }

// char returns the KeyMsg for a single typed rune.
func char(r rune) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}} }

// send feeds msgs to m.Update in order and returns the resulting model and
// the command produced by the last message.
func send(t *testing.T, m Model, msgs ...tea.Msg) (Model, tea.Cmd) {
	t.Helper()
	var cmd tea.Cmd
	for _, msg := range msgs {
		var updated tea.Model
		updated, cmd = m.Update(msg)
		m = updated.(Model)
	}
	return m, cmd
}

// quits reports whether cmd, when run, asks the program to exit.
func quits(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

// focusField moves the edit cursor onto the field with the given label using
// the arrow keys, failing if it cannot be reached.
func focusField(t *testing.T, m Model, label string) Model {
	t.Helper()
	for _, k := range []tea.KeyType{tea.KeyLeft, tea.KeyRight} {
		m = press(t, m, key(k))
		for i := 0; i < len(m.fields); i++ {
			if m.fields[m.editField].Label == label {
				return m
			}
			m = press(t, m, key(tea.KeyDown))
		}
	}
	t.Fatalf("field %q not reachable by arrow keys", label)
	return m
}

// reloadUser reads users.json back through the BBS's own user manager and
// returns the record with the given ID.
func reloadUser(t *testing.T, path string, id int) (*user.User, bool) {
	t.Helper()
	um, err := user.NewUserManager(filepath.Dir(path))
	if err != nil {
		t.Fatalf("NewUserManager: %v", err)
	}
	return um.GetUserByID(id)
}
