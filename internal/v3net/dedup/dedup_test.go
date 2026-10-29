package dedup

import (
	"database/sql"
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

	seen, err := ix.Seen("felonynet", "node-a", "550e8400-e29b-41d4-a716-446655440000")
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

	if err := ix.MarkSeen("felonynet", "node-a", uuid, nil); err != nil {
		t.Fatalf("MarkSeen failed: %v", err)
	}

	seen, err := ix.Seen("felonynet", "node-a", uuid)
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

	if err := ix.MarkSeen("felonynet", "node-a", uuid, &msgNum); err != nil {
		t.Fatalf("MarkSeen failed: %v", err)
	}

	seen, err := ix.Seen("felonynet", "node-a", uuid)
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

	if err := ix.MarkSeen("felonynet", "node-a", uuid, nil); err != nil {
		t.Fatalf("first MarkSeen failed: %v", err)
	}
	if err := ix.MarkSeen("felonynet", "node-a", uuid, nil); err != nil {
		t.Fatalf("duplicate MarkSeen should not error: %v", err)
	}
}

func TestLastSeen_Empty(t *testing.T) {
	ix := openTestIndex(t)

	uuid, origin, err := ix.LastSeen("felonynet")
	if err != nil {
		t.Fatalf("LastSeen failed: %v", err)
	}
	if uuid != "" || origin != "" {
		t.Errorf("expected empty strings for no messages, got %q, %q", uuid, origin)
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
		if err := ix.MarkSeen("felonynet", "node-a", u, nil); err != nil {
			t.Fatalf("MarkSeen failed: %v", err)
		}
	}

	last, origin, err := ix.LastSeen("felonynet")
	if err != nil {
		t.Fatalf("LastSeen failed: %v", err)
	}
	if last != uuids[len(uuids)-1] || origin != "node-a" {
		t.Errorf("expected last %q from node-a, got %q from %q", uuids[len(uuids)-1], last, origin)
	}
}

func TestLastSeen_FiltersByNetwork(t *testing.T) {
	ix := openTestIndex(t)

	if err := ix.MarkSeen("netA", "node-a", "aaa-1", nil); err != nil {
		t.Fatalf("MarkSeen failed: %v", err)
	}
	if err := ix.MarkSeen("netB", "node-a", "bbb-1", nil); err != nil {
		t.Fatalf("MarkSeen failed: %v", err)
	}

	last, _, err := ix.LastSeen("netA")
	if err != nil {
		t.Fatalf("LastSeen failed: %v", err)
	}
	if last != "aaa-1" {
		t.Errorf("expected %q for netA, got %q", "aaa-1", last)
	}

	last, _, err = ix.LastSeen("netB")
	if err != nil {
		t.Fatalf("LastSeen failed: %v", err)
	}
	if last != "bbb-1" {
		t.Errorf("expected %q for netB, got %q", "bbb-1", last)
	}
}

func TestSeen_KeyedOnNetworkOriginAndUUID(t *testing.T) {
	ix := openTestIndex(t)
	uuid := "550e8400-e29b-41d4-a716-446655440020"

	if err := ix.MarkSeen("felonynet", "node-a", uuid, nil); err != nil {
		t.Fatalf("MarkSeen failed: %v", err)
	}
	cases := []struct {
		network, origin string
		want            bool
	}{
		{"felonynet", "node-a", true},  // the same message again
		{"felonynet", "node-b", false}, // another node's message under the same UUID
		{"othernet", "node-a", false},  // the same pair in another network
	}
	for _, c := range cases {
		seen, err := ix.Seen(c.network, c.origin, uuid)
		if err != nil {
			t.Fatalf("Seen failed: %v", err)
		}
		if seen != c.want {
			t.Errorf("Seen(%s, %s) = %v, want %v", c.network, c.origin, seen, c.want)
		}
	}

	// Both copies can be recorded, and the newest is the cursor.
	if err := ix.MarkSeen("felonynet", "node-b", uuid, nil); err != nil {
		t.Fatalf("MarkSeen failed: %v", err)
	}
	if seen, _ := ix.Seen("felonynet", "node-b", uuid); !seen {
		t.Error("node-b's copy not seen after MarkSeen")
	}
	if last, origin, err := ix.LastSeen("felonynet"); err != nil || last != uuid || origin != "node-b" {
		t.Errorf("LastSeen = %q, %q, %v; want %s from node-b", last, origin, err, uuid)
	}
}

func TestOpen_MigratesLegacyIndex(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	// The schema before origin_node, keyed on msg_uuid alone.
	if _, err := db.Exec(`
CREATE TABLE seen_messages (
	msg_uuid       TEXT PRIMARY KEY,
	network        TEXT NOT NULL,
	local_jam_msgnum INTEGER,
	seen_at        DATETIME DEFAULT (datetime('now'))
);
INSERT INTO seen_messages (msg_uuid, network, local_jam_msgnum) VALUES
	('legacy-1', 'felonynet', 10),
	('legacy-2', 'felonynet', 11),
	('legacy-3', 'othernet', NULL);
`); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()

	for pass := 1; pass <= 2; pass++ {
		ix, err := Open(path)
		if err != nil {
			t.Fatalf("open %d: %v", pass, err)
		}
		// A legacy row has no origin and still blocks the message from any
		// node, so nothing imported before the upgrade is imported again.
		for _, origin := range []string{"node-a", "node-b"} {
			if seen, err := ix.Seen("felonynet", origin, "legacy-1"); err != nil || !seen {
				t.Errorf("open %d: Seen(legacy-1 from %s) = %v, %v; want true", pass, origin, seen, err)
			}
		}
		if seen, _ := ix.Seen("othernet", "node-a", "legacy-3"); !seen {
			t.Errorf("open %d: legacy-3 not seen in othernet", pass)
		}
		var n int
		if err := ix.db.QueryRow("SELECT COUNT(*) FROM seen_messages").Scan(&n); err != nil {
			t.Fatal(err)
		}
		switch pass {
		case 1:
			if n != 3 {
				t.Fatalf("migrated %d rows, want 3", n)
			}
			// The cursor is still the newest legacy row, with no origin.
			if last, origin, err := ix.LastSeen("felonynet"); err != nil || last != "legacy-2" || origin != "" {
				t.Errorf("LastSeen = %q, %q, %v; want legacy-2 with no origin", last, origin, err)
			}
			if err := ix.MarkSeen("felonynet", "node-b", "new-1", nil); err != nil {
				t.Fatalf("MarkSeen: %v", err)
			}
		case 2:
			if n != 4 {
				t.Errorf("second open holds %d rows, want 4", n)
			}
			if last, origin, err := ix.LastSeen("felonynet"); err != nil || last != "new-1" || origin != "node-b" {
				t.Errorf("LastSeen after reopen = %q, %q, %v; want new-1 from node-b", last, origin, err)
			}
		}
		_ = ix.Close()
	}
}
