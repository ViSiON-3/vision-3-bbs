package stringeditor

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// editValue drives an F1 edit to completion with the given escaped text.
func editValue(t *testing.T, m Model, key, escaped string) Model {
	t.Helper()
	m.cursor = indexOfKey(t, m, key)
	m = key2(t, m, tea.KeyMsg{Type: tea.KeyF1})
	m.textInput.SetValue(escaped)
	return key2(t, m, tea.KeyMsg{Type: tea.KeyEnter})
}

// TestEditWarnsOnFormatMismatch covers issue #237: dropping a verb from a
// formatted string is accepted but reported, because Sprintf would otherwise
// print %!d(MISSING) into the middle of the message a caller sees.
func TestEditWarnsOnFormatMismatch(t *testing.T) {
	m := newShippedModel(t)

	// pageNodeListEntry ships as " |15Node %d|07: %s\r\n" — two arguments.
	m = editValue(t, m, "pageNodeListEntry", `|15Node|07: %s\r\n`)

	if m.mode != modeNavigate {
		t.Fatalf("mode = %v, want the edit to be accepted", m.mode)
	}
	if !strings.HasPrefix(m.message, "WARNING") {
		t.Errorf("message = %q, want a format warning", m.message)
	}
	if got := m.values["pageNodeListEntry"]; got != "|15Node|07: %s\r\n" {
		t.Errorf("the edit was not kept: %q", got)
	}
}

// TestEditAcceptsCosmeticChanges checks that padding and reordering raise no
// warning, so a sysop tidying a prompt is not nagged.
func TestEditAcceptsCosmeticChanges(t *testing.T) {
	for _, tt := range []struct{ name, key, value string }{
		{"padding added", "pageNodeListEntry", `|15Node %-4d|07: %20s\r\n`},
		{"reordered by index", "pageNodeListEntry", `|15%[2]s|07 on node %[1]d\r\n`},
		{"literal percent added", "pageNodeListEntry", `|15Node %d|07: %s 100%%\r\n`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m := editValue(t, newShippedModel(t), tt.key, tt.value)
			if m.message != "" {
				t.Errorf("message = %q, want no warning for a cosmetic change", m.message)
			}
		})
	}
}

// TestEditIgnoresUnformattedStrings checks that a percent sign in a string
// nothing formats raises no warning. badUDRatio legitimately ends "%|09".
func TestEditIgnoresUnformattedStrings(t *testing.T) {
	m := newShippedModel(t)
	m = editValue(t, m, "badUDRatio", `|09Bad ratio (|15|RA%|09)`)

	if m.message != "" {
		t.Errorf("message = %q, want no warning for an unformatted string", m.message)
	}
}

// TestSaveWarnsThenSaves covers the save-time check: a mismatch is reported
// once, and pressing F10 again saves anyway rather than trapping every
// unrelated edit behind one bad string.
func TestSaveWarnsThenSaves(t *testing.T) {
	m := newShippedModel(t)
	m.values["pageNodeListEntry"] = "|15Node|07\r\n" // both verbs dropped

	m = key2(t, m, tea.KeyMsg{Type: tea.KeyF10})
	if m.message == "" {
		t.Fatal("first F10 gave no warning")
	}
	if !strings.Contains(m.message, "pageNodeListEntry") {
		t.Errorf("warning %q does not name the offending string", m.message)
	}

	m2 := key2(t, m, tea.KeyMsg{Type: tea.KeyF10})
	reloaded, err := LoadStrings(m2.filePath, nil)
	if err != nil {
		t.Fatalf("LoadStrings: %v", err)
	}
	if reloaded["pageNodeListEntry"] != "|15Node|07\r\n" {
		t.Errorf("second F10 did not save: got %q", reloaded["pageNodeListEntry"])
	}
}

// TestSaveIsSilentWhenEverythingMatches checks the shipped defaults save with
// no warning at all.
func TestSaveIsSilentWhenEverythingMatches(t *testing.T) {
	m := newShippedModel(t)
	if problems := m.formatProblems(); len(problems) > 0 {
		t.Errorf("shipped defaults report %d format problem(s): %v", len(problems), problems)
	}
}
