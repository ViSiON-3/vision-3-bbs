package wfcui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ViSiON-3/vision-3-bbs/internal/admin"
)

// TestHandleKeyQuitReturnsQuitCmd verifies that pressing q returns a quit command.
func TestHandleKeyQuitReturnsQuitCmd(t *testing.T) {
	m := New(nil, Options{MaxEvents: 10})
	m.width, m.height = 100, 30

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd == nil {
		t.Fatal("expected a non-nil Cmd for q, got nil")
	}
	// Execute the command and verify it produces a QuitMsg.
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Fatalf("expected tea.QuitMsg from q, got %T", msg)
	}
}

// TestHandleKeyQUppercaseQuit verifies uppercase Q also quits.
func TestHandleKeyQUppercaseQuit(t *testing.T) {
	m := New(nil, Options{MaxEvents: 10})
	m.width, m.height = 100, 30

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'Q'}})
	if cmd == nil {
		t.Fatal("expected a non-nil Cmd for Q, got nil")
	}
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Fatalf("expected tea.QuitMsg from Q, got %T", msg)
	}
}

// TestHandleKeyCtrlCQuit verifies Ctrl+C quits.
func TestHandleKeyCtrlCQuit(t *testing.T) {
	m := New(nil, Options{MaxEvents: 10})
	m.width, m.height = 100, 30

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("expected a non-nil Cmd for Ctrl+C, got nil")
	}
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Fatalf("expected tea.QuitMsg from Ctrl+C, got %T", msg)
	}
}

// TestDetailsViewShowsNodeFields verifies the details view renders key node fields.
func TestDetailsViewShowsNodeFields(t *testing.T) {
	m := makeModel(Options{NoColor: true, ASCII: true}, 100, 30)
	m.snapshot = &admin.SystemSnapshot{
		SystemName: "TestBBS",
		Time:       time.Now(),
		Nodes: []admin.NodeState{
			{
				NodeID:       3,
				Handle:       "SysopJoe",
				Status:       admin.StatusOnline,
				RemoteAddr:   "192.168.1.42:2323",
				CurrentMenu:  "MAIN",
				Activity:     "reading messages",
				ConnectedAt:  time.Now().Add(-30 * time.Minute),
				LastActivity: time.Now().Add(-5 * time.Minute),
				TimeLeftMins: 60,
			},
		},
	}
	m.selected = 0
	m.mode = modeDetails

	got := m.View()

	checks := []string{
		"SysopJoe",
		"192.168.1.42:2323",
		"MAIN",
		"reading messages",
		"3", // NodeID
		"0", // AccessLevel (default zero value)
	}
	for _, want := range checks {
		if !strings.Contains(got, want) {
			t.Errorf("detailsView missing %q; got:\n%s", want, got)
		}
	}
	// Must include hint to go back.
	if !strings.Contains(got, "Esc") && !strings.Contains(got, "ESC") && !strings.Contains(got, "esc") {
		t.Errorf("detailsView missing Esc hint; got:\n%s", got)
	}
}

// TestHandleKeyLTogglesShowLogs verifies L toggles the showLogs field.
// showLogs defaults to true (event feed is visible on startup).
func TestHandleKeyLTogglesShowLogs(t *testing.T) {
	m := New(nil, Options{MaxEvents: 10})
	m.width, m.height = 100, 30

	if !m.showLogs {
		t.Fatal("showLogs should start true (event feed visible by default)")
	}

	mi, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	m = mi.(Model)
	if m.showLogs {
		t.Fatal("showLogs should be false after pressing l")
	}

	mi, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	m = mi.(Model)
	if !m.showLogs {
		t.Fatal("showLogs should be true after pressing l again")
	}
}

// TestShowLogsTogglesEventFeedInView verifies the event feed appears/disappears in rendered output.
func TestShowLogsTogglesEventFeedInView(t *testing.T) {
	m := makeModel(Options{NoColor: true, ASCII: true}, 100, 30)
	m.mode = modeList
	m.snapshot = &admin.SystemSnapshot{
		SystemName: "TestBBS",
		Time:       time.Now(),
		Nodes:      []admin.NodeState{},
	}
	m.events = []admin.Event{
		{
			Time:    time.Now(),
			Type:    admin.EventCallerConnected,
			NodeID:  1,
			Handle:  "TestUser",
			Message: "unique-event-marker",
		},
	}

	// showLogs defaults true — event text must appear.
	m.showLogs = true
	gotWithLogs := m.View()
	if !strings.Contains(gotWithLogs, "unique-event-marker") {
		t.Errorf("showLogs=true: event text missing from view; got:\n%s", gotWithLogs)
	}

	// Toggle off — event text must NOT appear.
	m.showLogs = false
	gotWithoutLogs := m.View()
	if strings.Contains(gotWithoutLogs, "unique-event-marker") {
		t.Errorf("showLogs=false: event text should be hidden; got:\n%s", gotWithoutLogs)
	}
}

// TestEnterWithNilSnapshotStaysInList verifies Enter does not enter details when snapshot is nil.
func TestEnterWithNilSnapshotStaysInList(t *testing.T) {
	m := New(nil, Options{MaxEvents: 10})
	m.width, m.height = 100, 30
	m.mode = modeList
	m.snapshot = nil

	mi, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mi.(Model)
	if m.mode != modeList {
		t.Fatalf("expected modeList after Enter with nil snapshot, got %v", m.mode)
	}
}

// TestEnterWithEmptyNodesStaysInList verifies Enter does not enter details when there are no nodes.
func TestEnterWithEmptyNodesStaysInList(t *testing.T) {
	m := New(nil, Options{MaxEvents: 10})
	m.width, m.height = 100, 30
	m.mode = modeList
	m.snapshot = &admin.SystemSnapshot{
		SystemName: "TestBBS",
		Time:       time.Now(),
		Nodes:      []admin.NodeState{},
	}

	mi, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mi.(Model)
	if m.mode != modeList {
		t.Fatalf("expected modeList after Enter with zero nodes, got %v", m.mode)
	}
}

// TestHandleKeyRInListNilClientStaysInList verifies R in list mode with no client stays in list.
func TestHandleKeyRInListNilClientStaysInList(t *testing.T) {
	m := New(nil, Options{MaxEvents: 10})
	m.width, m.height = 100, 30
	m.mode = modeList

	mi, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	m = mi.(Model)
	if m.mode != modeList {
		t.Fatalf("expected modeList after r with nil client, got %v", m.mode)
	}
	if cmd != nil {
		t.Fatal("expected nil cmd for r with nil client")
	}
}

// TestDetailsViewNilSnapshotNoPanic verifies detailsView doesn't panic when snapshot is nil.
func TestDetailsViewNilSnapshotNoPanic(t *testing.T) {
	m := makeModel(Options{NoColor: true, ASCII: true}, 100, 30)
	m.snapshot = nil
	m.mode = modeDetails

	// Should not panic.
	got := m.detailsView()
	if got == "" {
		t.Error("detailsView returned empty string with nil snapshot")
	}
}

// TestDetailsViewOutOfBoundsSelectedNoPanic verifies detailsView doesn't panic when selected is out of range.
func TestDetailsViewOutOfBoundsSelectedNoPanic(t *testing.T) {
	m := makeModel(Options{NoColor: true, ASCII: true}, 100, 30)
	m.snapshot = &admin.SystemSnapshot{
		SystemName: "TestBBS",
		Time:       time.Now(),
		Nodes:      []admin.NodeState{},
	}
	m.selected = 5 // out of bounds
	m.mode = modeDetails

	// Should not panic.
	got := m.detailsView()
	if got == "" {
		t.Error("detailsView returned empty string")
	}
}
