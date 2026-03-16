package dedup

import (
	"path/filepath"
	"testing"
)

func openTestIndex(t *testing.T) *Index {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test_dedup.sqlite")
	ix, err := Open(path)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	t.Cleanup(func() { ix.Close() })
	return ix
}

func TestSeen_NotSeenInitially(t *testing.T) {
	ix := openTestIndex(t)

	seen, err := ix.Seen("550e8400-e29b-41d4-a716-446655440000")
	if err != nil {
		t.Fatalf("Seen failed: %v", err)
	}
	if seen {
		t.Error("expected not seen for new UUID")
	}
}

func TestMarkSeen_ThenSeen(t *testing.T) {
	ix := openTestIndex(t)
	uuid := "550e8400-e29b-41d4-a716-446655440000"

	if err := ix.MarkSeen(uuid, "felonynet", nil); err != nil {
		t.Fatalf("MarkSeen failed: %v", err)
	}

	seen, err := ix.Seen(uuid)
	if err != nil {
		t.Fatalf("Seen failed: %v", err)
	}
	if !seen {
		t.Error("expected seen after MarkSeen")
	}
}

func TestMarkSeen_WithLocalMsgNum(t *testing.T) {
	ix := openTestIndex(t)
	uuid := "550e8400-e29b-41d4-a716-446655440001"
	msgNum := int64(42)

	if err := ix.MarkSeen(uuid, "felonynet", &msgNum); err != nil {
		t.Fatalf("MarkSeen failed: %v", err)
	}

	seen, err := ix.Seen(uuid)
	if err != nil {
		t.Fatalf("Seen failed: %v", err)
	}
	if !seen {
		t.Error("expected seen after MarkSeen with msgNum")
	}
}

func TestMarkSeen_DuplicateIsNoOp(t *testing.T) {
	ix := openTestIndex(t)
	uuid := "550e8400-e29b-41d4-a716-446655440002"

	if err := ix.MarkSeen(uuid, "felonynet", nil); err != nil {
		t.Fatalf("first MarkSeen failed: %v", err)
	}
	if err := ix.MarkSeen(uuid, "felonynet", nil); err != nil {
		t.Fatalf("duplicate MarkSeen should not error: %v", err)
	}
}

func TestLastSeen_Empty(t *testing.T) {
	ix := openTestIndex(t)

	uuid, err := ix.LastSeen("felonynet")
	if err != nil {
		t.Fatalf("LastSeen failed: %v", err)
	}
	if uuid != "" {
		t.Errorf("expected empty string for no messages, got %q", uuid)
	}
}

func TestLastSeen_ReturnsLatest(t *testing.T) {
	ix := openTestIndex(t)

	uuids := []string{
		"550e8400-e29b-41d4-a716-446655440010",
		"550e8400-e29b-41d4-a716-446655440011",
		"550e8400-e29b-41d4-a716-446655440012",
	}
	for _, u := range uuids {
		if err := ix.MarkSeen(u, "felonynet", nil); err != nil {
			t.Fatalf("MarkSeen failed: %v", err)
		}
	}

	last, err := ix.LastSeen("felonynet")
	if err != nil {
		t.Fatalf("LastSeen failed: %v", err)
	}
	if last != uuids[len(uuids)-1] {
		t.Errorf("expected last UUID %q, got %q", uuids[len(uuids)-1], last)
	}
}

func TestLastSeen_FiltersByNetwork(t *testing.T) {
	ix := openTestIndex(t)

	if err := ix.MarkSeen("aaa-1", "netA", nil); err != nil {
		t.Fatalf("MarkSeen failed: %v", err)
	}
	if err := ix.MarkSeen("bbb-1", "netB", nil); err != nil {
		t.Fatalf("MarkSeen failed: %v", err)
	}

	last, err := ix.LastSeen("netA")
	if err != nil {
		t.Fatalf("LastSeen failed: %v", err)
	}
	if last != "aaa-1" {
		t.Errorf("expected %q for netA, got %q", "aaa-1", last)
	}

	last, err = ix.LastSeen("netB")
	if err != nil {
		t.Fatalf("LastSeen failed: %v", err)
	}
	if last != "bbb-1" {
		t.Errorf("expected %q for netB, got %q", "bbb-1", last)
	}
}
