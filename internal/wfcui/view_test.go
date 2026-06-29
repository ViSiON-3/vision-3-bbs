package wfcui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/admin"
)

// makeModel builds a Model with explicit dimensions (bypassing WindowSizeMsg)
// so tests are independent of terminal size.
func makeModel(opts Options, w, h int) Model {
	m := New(nil, opts)
	m.width, m.height = w, h
	return m
}

// TestViewTooSmall verifies the too-small guard renders the expected text.
func TestViewTooSmall(t *testing.T) {
	m := makeModel(Options{NoColor: true, ASCII: true}, minWidth-1, minHeight-1)
	m.mode = modeTooSmall
	got := m.View()
	if !strings.Contains(got, "Terminal too small") {
		t.Errorf("too-small view missing expected text; got:\n%s", got)
	}
}

// TestViewDisconnected verifies the disconnected banner contains the error.
func TestViewDisconnected(t *testing.T) {
	m := makeModel(Options{NoColor: true, ASCII: true}, 100, 30)
	m.mode = modeDisconnected
	m.lastErr = errors.New("connection refused")
	got := m.View()
	if !strings.Contains(got, "Disconnected") {
		t.Errorf("disconnected view missing 'Disconnected'; got:\n%s", got)
	}
	if !strings.Contains(got, "connection refused") {
		t.Errorf("disconnected view missing error text; got:\n%s", got)
	}
}

// TestViewListContainsNodeHandle verifies a node handle appears in list view.
func TestViewListContainsNodeHandle(t *testing.T) {
	m := makeModel(Options{NoColor: true, ASCII: true}, 100, 30)
	m.mode = modeList
	m.snapshot = &admin.SystemSnapshot{
		SystemName: "TestBBS",
		Time:       time.Now(),
		UptimeSecs: 3600,
		Nodes: []admin.NodeState{
			{NodeID: 1, Handle: "SysopJoe", Activity: "chatting", Status: admin.StatusOnline},
		},
		Counters: admin.Counters{ActiveNodes: 1, CallsToday: 42},
	}
	got := m.View()
	if !strings.Contains(got, "SysopJoe") {
		t.Errorf("list view missing node handle; got:\n%s", got)
	}
	if !strings.Contains(got, "TestBBS") {
		t.Errorf("list view missing system name; got:\n%s", got)
	}
	if !strings.Contains(got, "chatting") {
		t.Errorf("list view missing activity; got:\n%s", got)
	}
}

// TestViewListNilSnapshot renders without panic.
func TestViewListNilSnapshot(t *testing.T) {
	m := makeModel(Options{NoColor: true, ASCII: true}, 100, 30)
	m.mode = modeList
	m.snapshot = nil
	got := m.View()
	// Should not panic; should produce some output.
	if got == "" {
		t.Error("View() returned empty string with nil snapshot")
	}
}

// TestViewListShowsEvents verifies an event message appears in the feed.
func TestViewListShowsEvents(t *testing.T) {
	m := makeModel(Options{NoColor: true, ASCII: true}, 100, 30)
	m.mode = modeList
	m.snapshot = &admin.SystemSnapshot{
		SystemName: "TestBBS",
		Time:       time.Now(),
		Nodes:      []admin.NodeState{},
		Counters:   admin.Counters{},
	}
	m.events = []admin.Event{
		{
			Time:    time.Now(),
			Type:    admin.EventCallerConnected,
			NodeID:  2,
			Handle:  "GuestUser",
			Message: "caller connected",
		},
	}
	got := m.View()
	if !strings.Contains(got, "GuestUser") {
		t.Errorf("list view missing event handle; got:\n%s", got)
	}
}

// TestViewDisconnectedNilErr verifies nil lastErr doesn't panic.
func TestViewDisconnectedNilErr(t *testing.T) {
	m := makeModel(Options{NoColor: true, ASCII: true}, 100, 30)
	m.mode = modeDisconnected
	m.lastErr = nil
	got := m.View()
	if !strings.Contains(got, "Disconnected") {
		t.Errorf("disconnected view missing 'Disconnected'; got:\n%s", got)
	}
}

// TestViewASCIIBorderHasNoBraille verifies ASCII mode doesn't include box-drawing chars.
func TestViewASCIIBorderHasNoBraille(t *testing.T) {
	m := makeModel(Options{NoColor: true, ASCII: true}, 100, 30)
	m.mode = modeList
	m.snapshot = &admin.SystemSnapshot{
		SystemName: "TestBBS",
		Time:       time.Now(),
		Nodes:      []admin.NodeState{},
	}
	got := m.View()
	// ASCII mode should use only printable ASCII — no box-drawing runes (>= 0x2500).
	for i, r := range got {
		if r >= 0x2500 && r <= 0x257F {
			t.Errorf("ASCII mode output contains box-drawing rune %U at byte %d", r, i)
			break
		}
	}
}
