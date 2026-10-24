package message

import (
	"os"
	"path/filepath"
	"testing"
)

// newV3NetTestManager builds a manager with a V3Net area (1), a local area
// (2) and a local area the IsV3NetArea hook reports as bound (3).
func newV3NetTestManager(t *testing.T) *MessageManager {
	t.Helper()
	tmp := t.TempDir()
	cfg := filepath.Join(tmp, "config")
	if err := os.MkdirAll(cfg, 0o755); err != nil {
		t.Fatal(err)
	}
	areas := `[{"id":1,"tag":"FEL.GENERAL","name":"General","base_path":"fel_general","area_type":"v3net","network":"felonynet"},
	           {"id":2,"tag":"LOCAL","name":"Local","base_path":"local","area_type":"local"},
	           {"id":3,"tag":"BOUND","name":"Bound","base_path":"bound","area_type":"local"}]`
	if err := os.WriteFile(filepath.Join(cfg, "message_areas.json"), []byte(areas), 0o644); err != nil {
		t.Fatal(err)
	}
	mm, err := NewMessageManager(tmp, cfg, "TestBBS", nil)
	if err != nil {
		t.Fatalf("NewMessageManager: %v", err)
	}
	mm.IsV3NetArea = func(id int) bool { return id == 3 }
	return mm
}

func pendingIn(t *testing.T, mm *MessageManager, areaID int) []int {
	t.Helper()
	p, _, err := mm.PendingOutbound(areaID, 1)
	if err != nil {
		t.Fatalf("PendingOutbound(%d): %v", areaID, err)
	}
	return p
}

// Only local posts to V3Net areas wait for the outbound sweep: not imports,
// not private mail, and nothing in ordinary local areas.
func TestPendingOutbound_WhatIsPending(t *testing.T) {
	mm := newV3NetTestManager(t)
	post := func(area int, body string) int {
		n, err := mm.AddMessage(area, "Bob", "All", "s", body, "")
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	p1 := post(1, "hello")
	post(1, V3NetUUIDPrefix+"1234\nfrom elsewhere") // an import
	if _, err := mm.AddPrivateMessage(1, "Bob", "Alice", "s", "secret", ""); err != nil {
		t.Fatal(err)
	}
	p4 := post(1, "second")
	post(2, "local only")
	p6 := post(3, "bound by hook")

	if got := pendingIn(t, mm, 1); len(got) != 2 || got[0] != p1 || got[1] != p4 {
		t.Errorf("V3Net area pending = %v, want [%d %d]", got, p1, p4)
	}
	if got := pendingIn(t, mm, 2); len(got) != 0 {
		t.Errorf("local area pending = %v, want none", got)
	}
	if got := pendingIn(t, mm, 3); len(got) != 1 || got[0] != p6 {
		t.Errorf("hook-bound area pending = %v, want [%d]", got, p6)
	}
}

func TestPendingOutbound_MarkSentAndNext(t *testing.T) {
	mm := newV3NetTestManager(t)
	for i := 0; i < 3; i++ {
		if _, err := mm.AddMessage(1, "Bob", "All", "s", "body", ""); err != nil {
			t.Fatal(err)
		}
	}
	if err := mm.MarkMessageSent(1, 1); err != nil {
		t.Fatal(err)
	}
	if err := mm.MarkMessageSent(1, 3); err != nil {
		t.Fatal(err)
	}
	pending, next, err := mm.PendingOutbound(1, 1)
	if err != nil || len(pending) != 1 || pending[0] != 2 || next != 2 {
		t.Fatalf("pending=%v next=%d err=%v, want [2] next 2", pending, next, err)
	}
	if err := mm.MarkMessageSent(1, 2); err != nil {
		t.Fatal(err)
	}
	pending, next, _ = mm.PendingOutbound(1, next)
	if len(pending) != 0 || next != 4 {
		t.Errorf("after all sent: pending=%v next=%d, want none, next 4", pending, next)
	}
	// A scan point past the end (as after a pack) starts over.
	if _, next, _ = mm.PendingOutbound(1, 99); next != 4 {
		t.Errorf("stale from: next=%d, want 4", next)
	}
}
