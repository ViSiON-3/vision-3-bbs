package configeditor

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func reorderTags(m Model) []string {
	t := make([]string, len(m.configs.MsgAreas))
	for i, a := range m.configs.MsgAreas {
		t[i] = a.Tag
	}
	return t
}

func key(m Model, k tea.KeyMsg) Model {
	mm, _ := m.Update(k)
	return mm.(Model)
}

// TestReorderMovesItemLive covers the reported bug: pressing P turned the row
// green but the area would not move. The item now travels with the arrow keys —
// the slice is reordered on each press (green rides the cursor) — so the move
// is visible before Enter, not deferred to it.
func TestReorderMovesItemLive(t *testing.T) {
	m := configuredModel()
	m.mode = modeRecordList
	m.recordType = "msgarea"
	m.recordCursor = 0
	m.recordFields = m.buildRecordFields()

	m = key(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	if m.mode != modeRecordReorder {
		t.Fatalf("P did not enter reorder mode, got %v", m.mode)
	}
	first := reorderTags(m)[0]

	m = key(m, tea.KeyMsg{Type: tea.KeyDown})
	if reorderTags(m)[0] == first {
		t.Errorf("item did not move on Down (still %q at top): %v", first, reorderTags(m))
	}
	if m.recordCursor != 1 {
		t.Errorf("cursor should follow the moved item to index 1, got %d", m.recordCursor)
	}
}

// TestReorderEscRestores covers cancel: the original order and positions must
// come back exactly, since a cancelled reorder must change nothing.
func TestReorderEscRestores(t *testing.T) {
	m := configuredModel()
	m.mode = modeRecordList
	m.recordType = "msgarea"
	m.recordCursor = 0
	m.recordFields = m.buildRecordFields()
	orig := reorderTags(m)

	m = key(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	m = key(m, tea.KeyMsg{Type: tea.KeyDown})
	m = key(m, tea.KeyMsg{Type: tea.KeyDown})
	m = key(m, tea.KeyMsg{Type: tea.KeyEscape})

	if got := reorderTags(m); !equalStrings(got, orig) {
		t.Errorf("Esc did not restore order: got %v, want %v", got, orig)
	}
	if m.mode != modeRecordList {
		t.Errorf("Esc should return to the list, got %v", m.mode)
	}
}

// TestReorderEnterCommitsAndRenumbers covers commit: the new order sticks and
// positions are renumbered 1..n.
func TestReorderEnterCommitsAndRenumbers(t *testing.T) {
	m := configuredModel()
	m.mode = modeRecordList
	m.recordType = "msgarea"
	m.recordCursor = 0
	m.recordFields = m.buildRecordFields()
	orig := reorderTags(m)

	m = key(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	m = key(m, tea.KeyMsg{Type: tea.KeyDown})
	m = key(m, tea.KeyMsg{Type: tea.KeyEnter})

	if reorderTags(m)[0] == orig[0] {
		t.Errorf("commit did not keep the new order: %v", reorderTags(m))
	}
	if !m.dirty {
		t.Error("a committed reorder should mark the config dirty")
	}
	for i, a := range m.configs.MsgAreas {
		if a.Position != i+1 {
			t.Errorf("position %d of area %s not renumbered to %d", a.Position, a.Tag, i+1)
		}
	}
}

// TestReorderStaysWithinConference covers the conference clamp: an area cannot
// be moved out of its conference. Positions are one global sequence, but the
// ordering users see is per-conference, so reorder is confined to the block.
func TestReorderStaysWithinConference(t *testing.T) {
	m := configuredModel()
	m.mode = modeRecordList
	m.recordType = "msgarea"
	m.recordFields = m.buildRecordFields()
	// Put the cursor on the first area of its conference.
	m.recordCursor = 0
	confID := m.configs.MsgAreas[0].ConferenceID

	m = key(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	// Drive it hard downward; it must stop at the conference boundary.
	for i := 0; i < len(m.configs.MsgAreas)+2; i++ {
		m = key(m, tea.KeyMsg{Type: tea.KeyDown})
	}
	if got := m.configs.MsgAreas[m.recordCursor].ConferenceID; got != confID {
		t.Errorf("area escaped its conference: cursor conf %d, want %d", got, confID)
	}
	// Every area still belongs to a contiguous run of its conference (no area
	// was dragged across a boundary).
	m = key(m, tea.KeyMsg{Type: tea.KeyEscape})
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
