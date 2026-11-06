package dedup

import (
	"database/sql"
	"path/filepath"
	"testing"
)

// lookup is Lookup that fails the test on error.
func lookup(t *testing.T, ix *Index, origin, uuid string) (seen, pending bool) {
	t.Helper()
	seen, pending, err := ix.Lookup("net", origin, uuid)
	if err != nil {
		t.Fatalf("Lookup(%s/%s): %v", origin, uuid, err)
	}
	return seen, pending
}

// A claim is seen and pending until confirmed, Release drops only a claim,
// and a confirmed record or a legacy one wins over a claim.
func TestClaimConfirmRelease(t *testing.T) {
	ix := openTestIndex(t)

	if seen, pending := lookup(t, ix, "node-a", "m1"); seen || pending {
		t.Fatalf("new message: seen=%v pending=%v, want neither", seen, pending)
	}
	if err := ix.Claim("net", "node-a", "m1"); err != nil {
		t.Fatal(err)
	}
	if seen, pending := lookup(t, ix, "node-a", "m1"); !seen || !pending {
		t.Errorf("claimed: seen=%v pending=%v, want both", seen, pending)
	}
	if seen, _ := lookup(t, ix, "node-b", "m1"); seen {
		t.Error("a claim from node-a hides node-b's message under the same UUID")
	}
	if err := ix.Confirm("net", "node-a", "m1", 42); err != nil {
		t.Fatal(err)
	}
	if seen, pending := lookup(t, ix, "node-a", "m1"); !seen || pending {
		t.Errorf("confirmed: seen=%v pending=%v, want seen only", seen, pending)
	}
	var num sql.NullInt64
	if err := ix.db.QueryRow("SELECT local_jam_msgnum FROM seen_messages WHERE msg_uuid = 'm1'").Scan(&num); err != nil || num.Int64 != 42 {
		t.Errorf("local_jam_msgnum = %v, %v; want 42", num, err)
	}
	if err := ix.Release("net", "node-a", "m1"); err != nil {
		t.Fatal(err)
	}
	if seen, _ := lookup(t, ix, "node-a", "m1"); !seen {
		t.Error("Release dropped a confirmed record")
	}

	if err := ix.Claim("net", "node-a", "m2"); err != nil {
		t.Fatal(err)
	}
	if err := ix.Release("net", "node-a", "m2"); err != nil {
		t.Fatal(err)
	}
	if seen, _ := lookup(t, ix, "node-a", "m2"); seen {
		t.Error("released claim still seen")
	}

	// Claiming what is already recorded changes nothing.
	if err := ix.MarkSeen("net", "node-a", "m3", nil); err != nil {
		t.Fatal(err)
	}
	if err := ix.Claim("net", "node-a", "m3"); err != nil {
		t.Fatal(err)
	}
	if _, pending := lookup(t, ix, "node-a", "m3"); pending {
		t.Error("Claim made a recorded message pending")
	}
	// A legacy row (no origin) wins over a claim for the same UUID.
	if _, err := ix.db.Exec("INSERT INTO seen_messages (network, msg_uuid, origin_node) VALUES ('net', 'm4', '')"); err != nil {
		t.Fatal(err)
	}
	if err := ix.Claim("net", "node-a", "m4"); err != nil {
		t.Fatal(err)
	}
	if seen, pending := lookup(t, ix, "node-a", "m4"); !seen || pending {
		t.Errorf("legacy row and a claim: seen=%v pending=%v, want seen only", seen, pending)
	}
}

// A claim is never the poll cursor, so its message is fetched again.
func TestLastSeenSkipsClaims(t *testing.T) {
	ix := openTestIndex(t)
	if err := ix.MarkSeen("net", "node-a", "done", nil); err != nil {
		t.Fatal(err)
	}
	if err := ix.Claim("net", "node-a", "claimed"); err != nil {
		t.Fatal(err)
	}
	if last, origin, err := ix.LastSeen("net"); err != nil || last != "done" || origin != "node-a" {
		t.Errorf("LastSeen = %q, %q, %v; want done from node-a", last, origin, err)
	}
	if err := ix.Confirm("net", "node-a", "claimed", 1); err != nil {
		t.Fatal(err)
	}
	if last, _, err := ix.LastSeen("net"); err != nil || last != "claimed" {
		t.Errorf("LastSeen after confirm = %q, %v; want claimed", last, err)
	}
}

// A post this node sent is seen at once but is not the cursor until the
// poll passes it; then it is the newest row. Passed leaves other rows alone.
func TestMarkSentAwaitsPoll(t *testing.T) {
	ix := openTestIndex(t)
	wantLast := func(want string) {
		t.Helper()
		if last, _, err := ix.LastSeen("net"); err != nil || last != want {
			t.Errorf("LastSeen = %q, %v; want %q", last, err, want)
		}
	}
	if err := ix.MarkSeen("net", "node-a", "theirs-1", nil); err != nil {
		t.Fatal(err)
	}
	if err := ix.MarkSent("net", "me", "mine"); err != nil {
		t.Fatal(err)
	}
	if seen, pending := lookup(t, ix, "me", "mine"); !seen || pending {
		t.Errorf("sent post: seen=%v pending=%v, want seen only", seen, pending)
	}
	wantLast("theirs-1")

	// A post stored on the hub before ours is imported after we sent ours.
	if err := ix.MarkSeen("net", "node-a", "theirs-2", nil); err != nil {
		t.Fatal(err)
	}
	wantLast("theirs-2")
	if err := ix.Passed("net", "me", "mine"); err != nil {
		t.Fatal(err)
	}
	wantLast("mine")

	// Passing it again, or passing a row that never awaited, changes nothing.
	for _, uuid := range []string{"mine", "theirs-1"} {
		origin := map[string]string{"mine": "me", "theirs-1": "node-a"}[uuid]
		if err := ix.Passed("net", origin, uuid); err != nil {
			t.Fatal(err)
		}
	}
	wantLast("mine")
	if err := ix.MarkSent("net", "me", "mine"); err != nil {
		t.Fatal(err)
	}
	wantLast("mine") // already recorded: MarkSent leaves it be
}

// An index from before the pending and awaiting_poll columns gets them,
// with every row confirmed and none awaiting.
func TestOpen_AddsPendingColumn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "before-pending.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
CREATE TABLE seen_messages (
	id               INTEGER PRIMARY KEY,
	network          TEXT NOT NULL,
	msg_uuid         TEXT NOT NULL,
	origin_node      TEXT NOT NULL DEFAULT '',
	local_jam_msgnum INTEGER,
	seen_at          DATETIME DEFAULT (datetime('now')),
	UNIQUE(network, msg_uuid, origin_node)
);
INSERT INTO seen_messages (network, msg_uuid, origin_node, local_jam_msgnum) VALUES ('net', 'old-1', 'node-a', 5);
`); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()

	for pass := 1; pass <= 2; pass++ {
		ix, err := Open(path)
		if err != nil {
			t.Fatalf("open %d: %v", pass, err)
		}
		if seen, pending := lookup(t, ix, "node-a", "old-1"); !seen || pending {
			t.Errorf("open %d: old-1 seen=%v pending=%v, want seen only", pass, seen, pending)
		}
		if last, _, err := ix.LastSeen("net"); err != nil || last != "old-1" {
			t.Errorf("open %d: LastSeen = %q, %v; want old-1", pass, last, err)
		}
		// and the awaiting_poll column
		if err := ix.MarkSent("net", "me", "sent-1"); err != nil {
			t.Errorf("open %d: MarkSent: %v", pass, err)
		}
		if last, _, err := ix.LastSeen("net"); err != nil || last != "old-1" {
			t.Errorf("open %d: LastSeen after MarkSent = %q, %v; want old-1", pass, last, err)
		}
		_ = ix.Close()
	}
}
