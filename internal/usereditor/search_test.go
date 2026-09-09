package usereditor

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// listing returns a model on the user list, seeded with the given handles.
func listing(t *testing.T, handles ...string) Model {
	t.Helper()
	users := make([]*user.User, len(handles))
	for i, h := range handles {
		users[i] = &user.User{ID: i + 1, Handle: h, AccessLevel: 10}
	}
	m, _ := editorOver(t, users...)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 25})
	return updated.(Model)
}

func typeRunes(t *testing.T, m Model, s string) Model {
	t.Helper()
	for _, r := range s {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = updated.(Model)
	}
	return m
}

// modeSearch had no key that reached it: everything else about search was
// implemented, but nothing ever assigned the mode. "/" now opens it, the same
// key ./strings uses.
func TestSlashOpensSearch(t *testing.T) {
	m := listing(t, "Alice", "Bob")
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = updated.(Model)

	if m.mode != modeSearch {
		t.Fatalf("/ left the editor in mode %v, want modeSearch", m.mode)
	}
	if !m.searchInput.Focused() {
		t.Error("search input is not focused, so typing would go nowhere")
	}
	if got := m.searchInput.Value(); got != "" {
		t.Errorf("search input opened holding %q, want empty", got)
	}
}

func TestSearchMovesTheCursorToTheMatch(t *testing.T) {
	m := listing(t, "Alice", "Bob", "Carol")
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = typeRunes(t, updated.(Model), "car")
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)

	if m.mode != modeSearch && m.mode != modeList {
		t.Fatalf("unexpected mode %v", m.mode)
	}
	if m.mode != modeList {
		t.Fatalf("Enter did not return to the list, got mode %v", m.mode)
	}
	if got := m.users[m.cursor].Handle; got != "Carol" {
		t.Errorf("cursor landed on %q, want Carol", got)
	}
	if !strings.Contains(m.message, "Carol") {
		t.Errorf("message %q does not report the match", m.message)
	}
	if m.searchInput.Focused() {
		t.Error("search input still focused after leaving search mode")
	}
}

// The search is case-insensitive and matches a substring, per updateSearch.
func TestSearchIsCaseInsensitiveSubstring(t *testing.T) {
	m := listing(t, "Alice", "BobbyTables", "Carol")
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = typeRunes(t, updated.(Model), "BYTAB")
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)

	if got := m.users[m.cursor].Handle; got != "BobbyTables" {
		t.Errorf("cursor landed on %q, want BobbyTables", got)
	}
}

// Searching forward from the cursor wraps past the end of the list.
func TestSearchWrapsPastTheEnd(t *testing.T) {
	m := listing(t, "Alice", "Bob", "Carol")
	m.cursor = 2 // on Carol, so Alice is only reachable by wrapping

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = typeRunes(t, updated.(Model), "alice")
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)

	if got := m.users[m.cursor].Handle; got != "Alice" {
		t.Errorf("cursor landed on %q, want Alice", got)
	}
}

// A miss says so. Returning to the list unchanged and silent reads as the key
// not having worked.
func TestSearchReportsAMiss(t *testing.T) {
	m := listing(t, "Alice", "Bob")
	before := m.cursor

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = typeRunes(t, updated.(Model), "zzzz")
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)

	if m.cursor != before {
		t.Errorf("a miss moved the cursor from %d to %d", before, m.cursor)
	}
	if m.message == "" {
		t.Error("a miss produced no message at all")
	}
	if !strings.Contains(m.message, "zzzz") {
		t.Errorf("message %q does not name what was searched for", m.message)
	}
}

// Escape abandons the search and leaves the cursor alone.
func TestEscapeCancelsSearch(t *testing.T) {
	m := listing(t, "Alice", "Bob", "Carol")
	before := m.cursor

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = typeRunes(t, updated.(Model), "carol")
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEscape})
	m = updated.(Model)

	if m.mode != modeList {
		t.Errorf("Escape left mode %v, want modeList", m.mode)
	}
	if m.cursor != before {
		t.Errorf("Escape moved the cursor from %d to %d", before, m.cursor)
	}
	if m.searchInput.Focused() {
		t.Error("search input still focused after cancelling")
	}
}

// The prompt shares its row with the flash message, so a message left over
// from a previous action must not hide it.
func TestSearchPromptIsNotMaskedByAStaleMessage(t *testing.T) {
	m := listing(t, "Alice", "Bob")
	m.message = "Validated: Bob"

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = updated.(Model)

	out := m.View()
	if !strings.Contains(out, "Search:") {
		t.Error("the search prompt is not rendered while a flash message is set")
	}
	if strings.Contains(out, "Validated: Bob") {
		t.Error("the stale flash message is still drawn over the search row")
	}
}

// The help screen advertises the key. Its height is derived from its content,
// so adding the line cannot leave the box off-centre.
func TestHelpScreenAdvertisesSearch(t *testing.T) {
	m := listing(t, "Alice")
	m.mode = modeHelp
	if out := m.View(); !strings.Contains(out, "Search Users by Handle") {
		t.Error("the help screen does not mention the search key")
	}
}
