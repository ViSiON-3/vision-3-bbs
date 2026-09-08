package stringeditor

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// search opens search mode, types query, and presses Enter.
func search(t *testing.T, m Model, query string) Model {
	t.Helper()
	m = key2(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	if m.mode != modeSearch {
		t.Fatalf("/ left the editor in mode %v, want modeSearch", m.mode)
	}
	for _, r := range query {
		m = key2(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return key2(t, m, tea.KeyMsg{Type: tea.KeyEnter})
}

// A search that matches nothing used to return to the list silently, leaving
// the sysop with no way to tell a miss from a key that did not register. The
// user editor had the identical gap; both now report it.
func TestSearchReportsAMiss(t *testing.T) {
	m := newTestModel(t)
	before := m.cursor

	m = search(t, m, "zzzznotanentryzzzz")

	if m.mode != modeNavigate {
		t.Fatalf("Enter left mode %v, want modeNavigate", m.mode)
	}
	if m.cursor != before {
		t.Errorf("a miss moved the cursor from %d to %d", before, m.cursor)
	}
	if m.message == "" {
		t.Fatal("a miss produced no message at all")
	}
	if !strings.Contains(m.message, "zzzznotanentryzzzz") {
		t.Errorf("message %q does not name what was searched for", m.message)
	}
}

// The miss message must not clobber a genuine hit: a match still reports the
// entry it found.
func TestSearchStillReportsAHit(t *testing.T) {
	m := newTestModel(t)
	if len(m.entries) == 0 {
		t.Skip("no entries to search")
	}
	// Search for an entry other than the one the cursor starts on, since the
	// scan runs forward from the cursor and examines its own row last.
	target := m.entries[len(m.entries)/2]

	m = search(t, m, strings.ToLower(target.Key))

	if !strings.HasPrefix(m.message, "Found: ") {
		t.Errorf("message %q does not report a hit", m.message)
	}
	if strings.Contains(m.message, "No entry matching") {
		t.Errorf("a hit was reported as a miss: %q", m.message)
	}
	if got := m.entries[m.cursor]; got.Key != target.Key {
		t.Errorf("cursor landed on %q, want %q", got.Key, target.Key)
	}
}
