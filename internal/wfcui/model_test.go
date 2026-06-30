package wfcui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ViSiON-3/vision-3-bbs/internal/admin"
)

func TestUpdateAppliesSnapshotAndSelection(t *testing.T) {
	m := New(nil, Options{MaxEvents: 50})
	m.width, m.height = 100, 30

	snap := &admin.SystemSnapshot{SystemName: "T", Time: time.Now(), Nodes: []admin.NodeState{
		{NodeID: 1, Handle: "A"}, {NodeID: 2, Handle: "B"},
	}}
	mi, _ := m.Update(snapshotMsg{snap: snap})
	m = mi.(Model)
	if m.snapshot == nil || len(m.snapshot.Nodes) != 2 {
		t.Fatalf("snapshot not applied: %+v", m.snapshot)
	}

	// Down arrow selects node index 1.
	mi, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = mi.(Model)
	if m.selected != 1 {
		t.Fatalf("expected selected=1, got %d", m.selected)
	}

	// Enter opens details.
	mi, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mi.(Model)
	if m.mode != modeDetails {
		t.Fatalf("expected details mode, got %v", m.mode)
	}

	// Esc returns to list.
	mi, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = mi.(Model)
	if m.mode != modeList {
		t.Fatalf("expected list mode, got %v", m.mode)
	}
}

func TestErrMsgEntersDisconnected(t *testing.T) {
	m := New(nil, Options{MaxEvents: 10})
	mi, _ := m.Update(errMsg{err: errStub{}})
	if mi.(Model).mode != modeDisconnected {
		t.Fatal("errMsg should enter disconnected mode")
	}
}

type errStub struct{}

func (errStub) Error() string { return "boom" }

// TestSnapshotMsgPollRearm verifies that the Update handler only re-arms
// pollCmd when snapshotMsg.fromPoll is true (the sustaining tick loop), and
// does NOT spawn a new chain for one-shot fetches (fromPoll=false). This
// prevents unbounded tea.Tick accumulation when the user presses R or on Init.
func TestSnapshotMsgPollRearm(t *testing.T) {
	snap := &admin.SystemSnapshot{SystemName: "T", Time: time.Now()}

	// fromPoll=false (one-shot fetch: Init paint or R-key) — must NOT re-arm.
	m := New(nil, Options{MaxEvents: 10})
	_, cmd := m.Update(snapshotMsg{snap: snap, fromPoll: false})
	if cmd != nil {
		t.Error("one-shot snapshotMsg (fromPoll=false) must not return a re-arm cmd")
	}

	// fromPoll=true (tick loop) with nil client — nil client guard must prevent re-arm.
	_, cmd = m.Update(snapshotMsg{snap: snap, fromPoll: true})
	if cmd != nil {
		t.Error("poll snapshotMsg with nil client must not return a re-arm cmd")
	}

	// fromPoll=true with a real client — must re-arm.
	srv := admin.NewServer(admin.ServerConfig{})
	client := admin.NewInProcessClient(srv)
	m2 := New(client, Options{MaxEvents: 10})
	_, cmd = m2.Update(snapshotMsg{snap: snap, fromPoll: true})
	if cmd == nil {
		t.Error("poll snapshotMsg (fromPoll=true) with non-nil client must return a re-arm cmd")
	}
}

// TestSubscribedFieldLifecycle verifies the subscribed flag tracks subscription liveness:
//   - errMsg sets subscribed=false and enters modeDisconnected.
//   - A poll snapshotMsg does NOT clear modeDisconnected while subscribed=false.
//   - A successful subscribedMsg sets subscribed=true and returns to modeList.
func TestSubscribedFieldLifecycle(t *testing.T) {
	m := New(nil, Options{MaxEvents: 10})
	m.width, m.height = 100, 30
	// Start in list mode with a snapshot so we have something to poll.
	snap := &admin.SystemSnapshot{SystemName: "T", Time: time.Now()}
	mi, _ := m.Update(snapshotMsg{snap: snap, fromPoll: false})
	m = mi.(Model)

	// Simulate subscription loss via errMsg → disconnected, subscribed=false.
	mi, _ = m.Update(errMsg{err: errStub{}})
	m = mi.(Model)
	if m.mode != modeDisconnected {
		t.Fatalf("errMsg: expected modeDisconnected, got %v", m.mode)
	}
	if m.subscribed {
		t.Fatal("errMsg: expected subscribed=false")
	}

	// A poll snapshot while disconnected and unsubscribed must NOT clear disconnected.
	mi, _ = m.Update(snapshotMsg{snap: snap, fromPoll: true})
	m = mi.(Model)
	if m.mode != modeDisconnected {
		t.Fatalf("poll snapshot while unsubscribed: expected modeDisconnected, got %v", m.mode)
	}

	// A successful subscribedMsg must restore subscribed=true and return to modeList.
	ch := make(chan admin.Event)
	mi, _ = m.Update(subscribedMsg{ch: ch, err: nil})
	m = mi.(Model)
	if !m.subscribed {
		t.Fatal("subscribedMsg success: expected subscribed=true")
	}
	if m.mode != modeList {
		t.Fatalf("subscribedMsg success: expected modeList, got %v", m.mode)
	}
}

// TestReconnectKeyReturnsBatch verifies that pressing R in disconnected mode with
// a non-nil client returns a non-nil batch cmd (fetchSnapshot + subscribeCmd).
func TestReconnectKeyReturnsBatch(t *testing.T) {
	srv := admin.NewServer(admin.ServerConfig{})
	client := admin.NewInProcessClient(srv)
	m := New(client, Options{MaxEvents: 10})
	m.width, m.height = 100, 30
	m.mode = modeDisconnected
	m.subscribed = false

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	if cmd == nil {
		t.Fatal("R in disconnected mode with non-nil client must return a non-nil cmd batch")
	}
}

func TestWindowSizeModeTooSmall(t *testing.T) {
	m := New(nil, Options{MaxEvents: 10})
	m.width, m.height = 100, 30

	// Small window should enter modeTooSmall.
	mi, _ := m.Update(tea.WindowSizeMsg{Width: 72, Height: 20})
	m = mi.(Model)
	if m.mode != modeTooSmall {
		t.Fatalf("expected modeTooSmall for small window, got %v", m.mode)
	}

	// Large window should return to modeList.
	mi, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = mi.(Model)
	if m.mode != modeList {
		t.Fatalf("expected modeList for large window, got %v", m.mode)
	}
}
