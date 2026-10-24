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
	p, err := mm.PendingOutbound(areaID)
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

func TestPendingOutbound_MarkSentAndPointer(t *testing.T) {
	mm := newV3NetTestManager(t)
	for i := 0; i < 3; i++ {
		if _, err := mm.AddMessage(1, "Bob", "All", "s", "body", ""); err != nil {
			t.Fatal(err)
		}
	}
	for _, n := range []int{1, 3} {
		if err := mm.MarkMessageSent(1, n); err != nil {
			t.Fatal(err)
		}
	}
	if got := pendingIn(t, mm, 1); len(got) != 1 || got[0] != 2 {
		t.Fatalf("pending = %v, want [2]", got)
	}
	if got := scanPointer(t, mm, 1); got != 1 {
		t.Errorf("scan pointer = %d, want 1 (just before the pending post)", got)
	}
	if err := mm.MarkMessageSent(1, 2); err != nil {
		t.Fatal(err)
	}
	if got := pendingIn(t, mm, 1); len(got) != 0 {
		t.Errorf("after all sent: pending = %v", got)
	}
	if got := scanPointer(t, mm, 1); got != 3 {
		t.Errorf("scan pointer = %d, want 3", got)
	}
}

func scanPointer(t *testing.T, mm *MessageManager, areaID int) int {
	t.Helper()
	b, err := mm.GetBase(areaID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = b.Close() }()
	lr, err := b.GetLastRead(V3NetOutboundScanner)
	if err != nil {
		t.Fatalf("GetLastRead: %v", err)
	}
	return int(lr.LastReadMsg)
}

// A pack that removes messages below a pending post renumbers it; the scan
// pointer is a lastread record, so the pack moves it too and the post is
// still found (Codex review on #685).
func TestPendingOutbound_SurvivesPack(t *testing.T) {
	mm := newV3NetTestManager(t)
	for i := 0; i < 3; i++ {
		if _, err := mm.AddMessage(1, "Bob", "All", "s", "body", ""); err != nil {
			t.Fatal(err)
		}
	}
	for _, n := range []int{1, 2} {
		if err := mm.MarkMessageSent(1, n); err != nil {
			t.Fatal(err)
		}
	}
	if got := pendingIn(t, mm, 1); len(got) != 1 || got[0] != 3 {
		t.Fatalf("pending = %v, want [3]", got)
	}
	if err := mm.DeleteMessage(1, 1); err != nil {
		t.Fatal(err)
	}
	if err := mm.PackAndLinkArea(1); err != nil {
		t.Fatal(err)
	}
	if got := pendingIn(t, mm, 1); len(got) != 1 || got[0] != 2 {
		t.Errorf("after pack: pending = %v, want [2]", got)
	}
}

// In an echomail area bound to V3Net, imports from V3Net are left
// unprocessed for the FTN tosser; the sweep must not send them back, and a
// sent post keeps its DateProcessed for the tosser (CodeRabbit on #685).
func TestPendingOutbound_EchomailBoundArea(t *testing.T) {
	tmp := t.TempDir()
	cfg := filepath.Join(tmp, "config")
	if err := os.MkdirAll(cfg, 0o755); err != nil {
		t.Fatal(err)
	}
	areas := `[{"id":1,"tag":"FD_GATE","name":"Gate","base_path":"gate","area_type":"echomail",
	            "echo_tag":"GATE","origin_addr":"1:2/3","network":"fidonet"}]`
	if err := os.WriteFile(filepath.Join(cfg, "message_areas.json"), []byte(areas), 0o644); err != nil {
		t.Fatal(err)
	}
	mm, err := NewMessageManager(tmp, cfg, "TestBBS", nil)
	if err != nil {
		t.Fatal(err)
	}
	mm.IsV3NetArea = func(int) bool { return true }

	if _, err := mm.AddMessage(1, "Remote", "All", "s", V3NetUUIDPrefix+"u-1\nfrom the hub", ""); err != nil {
		t.Fatal(err)
	}
	local, err := mm.AddMessage(1, "Bob", "All", "s", "local post", "")
	if err != nil {
		t.Fatal(err)
	}
	if got := pendingIn(t, mm, 1); len(got) != 1 || got[0] != local {
		t.Fatalf("pending = %v, want only the local post %d", got, local)
	}
	if err := mm.MarkMessageSent(1, local); err != nil {
		t.Fatal(err)
	}
	if got := pendingIn(t, mm, 1); len(got) != 0 {
		t.Errorf("after sent: pending = %v", got)
	}
	b, err := mm.GetBase(1)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = b.Close() }()
	hdr, err := b.ReadMessageHeader(local)
	if err != nil {
		t.Fatal(err)
	}
	if hdr.DateProcessed != 0 {
		t.Error("MarkMessageSent stamped DateProcessed in an echomail area; the tosser would skip its export")
	}
}
