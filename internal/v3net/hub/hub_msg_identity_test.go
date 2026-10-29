package hub

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

const identityUUID = "c00e8400-e29b-41d4-a716-446655440001"

// A message is (network, origin_node, msg_uuid): two nodes posting the same
// UUID are both kept and both delivered, and a node posting its own UUID
// again is still a duplicate.
func TestPostMessage_SameUUIDFromTwoNodes(t *testing.T) {
	h, ts, leafKS := sigTestHub(t)
	otherKS := loadTestKeystore(t, "other.key")
	registerLeafWithAreas(t, ts, otherKS, []string{"gen.general"})

	first := sigTestMessage(leafKS.NodeID(), identityUUID)
	if err := first.Sign(leafKS); err != nil {
		t.Fatal(err)
	}
	second := sigTestMessage(otherKS.NodeID(), identityUUID)
	second.Body = "Same UUID, different node."
	if err := second.Sign(otherKS); err != nil {
		t.Fatal(err)
	}
	if status, body := postAs(t, ts, leafKS, first); status != http.StatusOK {
		t.Fatalf("POST first = %d %s, want 200", status, body)
	}
	if status, body := postAs(t, ts, otherKS, second); status != http.StatusOK {
		t.Fatalf("POST second = %d %s, want 200", status, body)
	}
	// The first node posting its UUID again is accepted and dropped.
	if status, body := postAs(t, ts, leafKS, first); status != http.StatusOK {
		t.Fatalf("POST repeat = %d %s, want 200", status, body)
	}

	if n, err := h.messages.Count("testnet"); err != nil || n != 2 {
		t.Fatalf("hub holds %d messages (%v), want 2", n, err)
	}
	msgs := storedMessages(t, ts, otherKS)
	if len(msgs) != 2 {
		t.Fatalf("delivered %d messages, want 2", len(msgs))
	}
	if msgs[0].OriginNode != leafKS.NodeID() || msgs[1].OriginNode != otherKS.NodeID() {
		t.Errorf("delivered origins %s, %s; want %s then %s",
			msgs[0].OriginNode, msgs[1].OriginNode, leafKS.NodeID(), otherKS.NodeID())
	}
	for _, m := range msgs {
		if m.MsgUUID != identityUUID {
			t.Errorf("delivered UUID %s, want %s", m.MsgUUID, identityUUID)
		}
		if err := m.VerifySignature(); err != nil {
			t.Errorf("message from %s no longer verifies: %v", m.OriginNode, err)
		}
	}
}

// openMessageStore opens a hub message store on a fresh database file.
func openMessageStore(t *testing.T) (*MessageStore, *sql.DB) {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "hub.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	ms, err := NewMessageStore(db)
	if err != nil {
		t.Fatalf("NewMessageStore: %v", err)
	}
	return ms, db
}

// fetchUUIDs returns "origin/uuid" for each message Fetch returns.
func fetchUUIDs(t *testing.T, ms *MessageStore, network, since, sinceOrigin string) []string {
	t.Helper()
	results, _, err := ms.Fetch(network, since, sinceOrigin, 100, []string{"gen.general"})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	var out []string
	for _, data := range results {
		var m protocol.Message
		if err := json.Unmarshal([]byte(data), &m); err != nil {
			t.Fatal(err)
		}
		out = append(out, m.OriginNode+"/"+m.MsgUUID)
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestMessageStore_FetchCursor(t *testing.T) {
	ms, _ := openMessageStore(t)
	store := func(network, origin, uuid string) {
		t.Helper()
		if ok, err := ms.Store(uuid, origin, network, "gen.general", testMsgJSON(origin, "gen.general", uuid)); err != nil || !ok {
			t.Fatalf("Store %s/%s = %v, %v", origin, uuid, ok, err)
		}
	}
	const (
		u1 = "c10e8400-e29b-41d4-a716-446655440001"
		u2 = "c10e8400-e29b-41d4-a716-446655440002"
		u3 = "c10e8400-e29b-41d4-a716-446655440003"
		u9 = "c10e8400-e29b-41d4-a716-446655440009"
	)
	store("testnet", "nodea", u1) // id 1
	store("testnet", "nodeb", u2) // id 2
	store("testnet", "nodeb", u1) // id 3: same UUID, another node
	store("testnet", "nodea", u3) // id 4
	store("othernet", "nodea", u9)

	cases := []struct {
		name, since, origin string
		want                []string
	}{
		{"exact cursor on the first copy", u1, "nodea", []string{"nodeb/" + u2, "nodeb/" + u1, "nodea/" + u3}},
		{"exact cursor on the second copy", u1, "nodeb", []string{"nodea/" + u3}},
		// An older leaf sends no origin: the cursor is the earliest copy, so
		// nothing after it is skipped.
		{"no origin falls back to the earliest copy", u1, "", []string{"nodeb/" + u2, "nodeb/" + u1, "nodea/" + u3}},
		// A UUID only another network holds is not a cursor here.
		{"cursor from another network", u9, "nodea", []string{"nodea/" + u1, "nodeb/" + u2, "nodeb/" + u1, "nodea/" + u3}},
		{"cursor from another network without origin", u9, "", []string{"nodea/" + u1, "nodeb/" + u2, "nodeb/" + u1, "nodea/" + u3}},
		{"unknown origin", u1, "nodec", []string{"nodea/" + u1, "nodeb/" + u2, "nodeb/" + u1, "nodea/" + u3}},
	}
	for _, c := range cases {
		if got := fetchUUIDs(t, ms, "testnet", c.since, c.origin); !equalStrings(got, c.want) {
			t.Errorf("%s: Fetch = %q, want %q", c.name, got, c.want)
		}
	}
}

// oldMessagesSchema is the messages table before origin_node.
const oldMessagesSchema = `
CREATE TABLE messages (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	msg_uuid    TEXT UNIQUE NOT NULL,
	network     TEXT NOT NULL,
	area_tag    TEXT NOT NULL,
	data        TEXT NOT NULL,
	received_at DATETIME DEFAULT (datetime('now'))
);
CREATE INDEX idx_messages_network ON messages(network, id);
CREATE INDEX idx_messages_area ON messages(network, area_tag, id);
`

func TestMessageStore_MigratesOldSchema(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "hub.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	defer db.Close()
	if _, err := db.Exec(oldMessagesSchema); err != nil {
		t.Fatal(err)
	}
	const (
		u1 = "c20e8400-e29b-41d4-a716-446655440001"
		u2 = "c20e8400-e29b-41d4-a716-446655440002"
		u5 = "c20e8400-e29b-41d4-a716-446655440005"
	)
	old := []struct {
		id         int64
		uuid, data string
	}{
		{1, u1, testMsgJSON("nodea", "gen.general", u1)},
		{2, u2, `{"msg_uuid":"` + u2 + `"}`}, // no origin_node in the JSON
		{5, u5, testMsgJSON("nodeb", "gen.general", u5)},
	}
	for _, m := range old {
		if _, err := db.Exec(
			"INSERT INTO messages (id, msg_uuid, network, area_tag, data, received_at) VALUES (?, ?, 'testnet', 'gen.general', ?, '2026-01-02 03:04:05')",
			m.id, m.uuid, m.data,
		); err != nil {
			t.Fatal(err)
		}
	}
	// Rows 6 and 7 were stored and later deleted.
	if _, err := db.Exec("UPDATE sqlite_sequence SET seq = 7 WHERE name = 'messages'"); err != nil {
		t.Fatal(err)
	}

	check := func(pass string) {
		t.Helper()
		rows, err := db.Query("SELECT id, msg_uuid, origin_node, data, received_at FROM messages ORDER BY id")
		if err != nil {
			t.Fatalf("%s: %v", pass, err)
		}
		defer rows.Close()
		wantOrigin := []string{"nodea", "", "nodeb"}
		i := 0
		for rows.Next() {
			var id int64
			var uuid, origin, data, received string
			if err := rows.Scan(&id, &uuid, &origin, &data, &received); err != nil {
				t.Fatalf("%s: %v", pass, err)
			}
			if i >= len(old) {
				t.Fatalf("%s: unexpected row %d", pass, id)
			}
			if id != old[i].id || uuid != old[i].uuid || data != old[i].data || origin != wantOrigin[i] {
				t.Errorf("%s: row %d = %d %s %q, want %d %s %q",
					pass, i, id, uuid, origin, old[i].id, old[i].uuid, wantOrigin[i])
			}
			if received != "2026-01-02T03:04:05Z" && received != "2026-01-02 03:04:05" {
				t.Errorf("%s: row %d received_at = %q", pass, id, received)
			}
			i++
		}
		if i != len(old) {
			t.Errorf("%s: %d rows, want %d", pass, i, len(old))
		}
		for _, idx := range []string{"idx_messages_network", "idx_messages_area"} {
			var n int
			if err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = ?", idx).Scan(&n); err != nil || n != 1 {
				t.Errorf("%s: index %s present %d times (%v)", pass, idx, n, err)
			}
		}
	}

	ms, err := NewMessageStore(db)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	check("first open")
	if _, err := NewMessageStore(db); err != nil {
		t.Fatalf("second open: %v", err)
	}
	check("second open")

	// The old UUID-only constraint is gone: another node may use u1, but
	// nodea may not use it again.
	if ok, err := ms.Store(u1, "nodeb", "testnet", "gen.general", testMsgJSON("nodeb", "gen.general", u1)); err != nil || !ok {
		t.Errorf("Store u1 from nodeb = %v, %v; want stored", ok, err)
	}
	if ok, err := ms.Store(u1, "nodea", "testnet", "gen.general", testMsgJSON("nodea", "gen.general", u1)); err != nil || ok {
		t.Errorf("Store u1 from nodea again = %v, %v; want a duplicate", ok, err)
	}
	// New ids continue past the old high-water mark, not past the last row.
	var maxID int64
	if err := db.QueryRow("SELECT MAX(id) FROM messages").Scan(&maxID); err != nil || maxID != 8 {
		t.Errorf("new message id = %d (%v), want 8", maxID, err)
	}
}
