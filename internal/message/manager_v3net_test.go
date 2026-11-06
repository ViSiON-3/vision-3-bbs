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

// FindV3NetMessage finds a V3Net import by its UUID and origin lines: not
// another node's message under the same UUID, a UUID that only starts the
// same way, a local post that mentions it, or a legacy import with no origin
// line (unless no origin is asked for).
func TestFindV3NetMessage(t *testing.T) {
	mm := newV3NetTestManager(t)
	post := func(body string) int {
		t.Helper()
		n, err := mm.AddMessage(1, "Bob", "All", "s", body, "")
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	fromA := post(V3NetUUIDPrefix + "uuid-1\n" + V3NetOriginPrefix + "node-a\nfrom elsewhere")
	fromB := post(V3NetUUIDPrefix + "uuid-1\n" + V3NetOriginPrefix + "node-b\nsame UUID, another node")
	post(V3NetUUIDPrefix + "uuid-10\n" + V3NetOriginPrefix + "node-a\nanother")
	post("a local post about uuid-2 from node-a")
	legacy := post(V3NetUUIDPrefix + "uuid-3\nimported before origin lines")
	bare := post(V3NetUUIDPrefix + "uuid-4\n" + V3NetOriginPrefix + "node-a") // no body after
	// Another node's body that starts with an origin line of its own.
	post(V3NetUUIDPrefix + "uuid-5\n" + V3NetOriginPrefix + "node-b\n" + V3NetOriginPrefix + "node-a\nspoof")

	for _, tc := range []struct {
		uuid, origin string
		want         int
		found        bool
	}{
		{"uuid-1", "node-a", fromA, true},
		{"uuid-1", "node-b", fromB, true},
		{"uuid-1", "node-c", 0, false},
		{"uuid-4", "node-a", bare, true},
		{"uuid-3", "node-a", 0, false},
		{"uuid-3", "", legacy, true},
		{"uuid-5", "node-a", 0, false},
		{"uuid-2", "node-a", 0, false},
		{"uuid-", "node-a", 0, false},
		{"nope", "node-a", 0, false},
	} {
		n, found, err := mm.FindV3NetMessage(1, tc.uuid, tc.origin)
		if err != nil || found != tc.found || n != tc.want {
			t.Errorf("FindV3NetMessage(%q, %q) = %d, %v, %v; want %d, %v", tc.uuid, tc.origin, n, found, err, tc.want, tc.found)
		}
	}
	if _, _, err := mm.FindV3NetMessage(99, "uuid-1", "node-a"); err == nil {
		t.Error("FindV3NetMessage in a missing area returned no error")
	}
}

// A message FindV3NetMessage cannot read might be the one it looks for, so
// with no match it reports the read error rather than "not there".
func TestFindV3NetMessageUnreadable(t *testing.T) {
	mm := newV3NetTestManager(t)
	if _, err := mm.AddMessage(1, "Bob", "All", "s", V3NetUUIDPrefix+"uuid-1\n"+V3NetOriginPrefix+"node-a\nhi", ""); err != nil {
		t.Fatal(err)
	}
	area, _ := mm.GetAreaByID(1)
	jdt := mm.resolveBasePath(area) + ".jdt"
	if err := os.Truncate(jdt, 0); err != nil {
		t.Fatal(err)
	}
	if n, found, err := mm.FindV3NetMessage(1, "uuid-1", "node-a"); err == nil || found {
		t.Errorf("FindV3NetMessage over an unreadable message = %d, %v, %v; want an error", n, found, err)
	}
}
