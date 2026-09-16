package wfcui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ViSiON-3/vision-3-bbs/internal/admin"
)

func isQuit(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

func TestQuitKeys(t *testing.T) {
	for _, k := range []tea.KeyMsg{keyRune('q'), keyRune('Q'), {Type: tea.KeyCtrlC}} {
		m, _ := newTestModel(nil, Options{})
		_, cmd := update(t, m, k)
		if !isQuit(cmd) {
			t.Errorf("%v must quit", k)
		}
	}
	// Ctrl+C quits even inside the confirm prompt; q there only cancels.
	m, _ := newTestModel(newFakeClient(), Options{})
	m = withNodes(m)
	m, _ = update(t, m, keyRune('k'))
	if _, cmd := update(t, m, tea.KeyMsg{Type: tea.KeyCtrlC}); !isQuit(cmd) {
		t.Error("Ctrl+C in confirm prompt must quit")
	}
	m, cmd := update(t, m, keyRune('q'))
	if isQuit(cmd) || m.mode != modeList {
		t.Error("q in confirm prompt must cancel, not quit")
	}
}

func TestUpDownHomeEndBounds(t *testing.T) {
	m, _ := newTestModel(nil, Options{})
	m, _ = update(t, m, tea.KeyMsg{Type: tea.KeyUp})
	m, _ = update(t, m, tea.KeyMsg{Type: tea.KeyDown})
	if m.selected != 0 {
		t.Fatalf("no nodes: selected = %d", m.selected)
	}
	m.snapshot = &admin.SystemSnapshot{Nodes: []admin.NodeState{{NodeID: 1, Handle: "A"}, {NodeID: 2, Handle: "B"}, {NodeID: 3, Handle: "C"}}}
	m, _ = update(t, m, tea.KeyMsg{Type: tea.KeyEnd})
	if m.selected != 2 {
		t.Fatalf("End: selected = %d", m.selected)
	}
	m, _ = update(t, m, tea.KeyMsg{Type: tea.KeyDown})
	if m.selected != 2 {
		t.Fatalf("Down at end: selected = %d", m.selected)
	}
	m, _ = update(t, m, tea.KeyMsg{Type: tea.KeyHome})
	if m.selected != 0 {
		t.Fatalf("Home: selected = %d", m.selected)
	}
	m, _ = update(t, m, tea.KeyMsg{Type: tea.KeyUp})
	if m.selected != 0 {
		t.Fatalf("Up at start: selected = %d", m.selected)
	}
}

func TestEnterNeedsACaller(t *testing.T) {
	m, _ := newTestModel(nil, Options{})
	m, _ = update(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.mode != modeList {
		t.Fatal("Enter with nil snapshot must stay in list")
	}
	m.snapshot = &admin.SystemSnapshot{}
	m, _ = update(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.mode != modeList {
		t.Fatal("Enter with no nodes must stay in list")
	}
}

func TestDetailsArrowsMoveSelection(t *testing.T) {
	m, _ := newTestModel(nil, Options{})
	m = withNodes(m)
	m.mode = modeDetails
	m, _ = update(t, m, tea.KeyMsg{Type: tea.KeyDown})
	if m.selected != 1 || m.mode != modeDetails {
		t.Fatalf("selected=%d mode=%v", m.selected, m.mode)
	}
	m, _ = update(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.mode != modeList {
		t.Fatal("Enter in details must return to the list")
	}
}
