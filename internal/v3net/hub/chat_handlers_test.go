package hub

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/keystore"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// sendSigned performs a signed request and returns the status code. When out
// is non-nil the response body is decoded into it.
func sendSigned(t *testing.T, ks *keystore.Keystore, method, url, body string, out any) int {
	t.Helper()
	resp, err := http.DefaultClient.Do(signedRequest(t, ks, method, url, body))
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer resp.Body.Close()
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			t.Fatalf("%s %s: decode response: %v", method, url, err)
		}
	}
	return resp.StatusCode
}

// waitEvent returns the next event of the given type, skipping others.
func waitEvent(t *testing.T, ch <-chan protocol.Event, eventType string) protocol.Event {
	t.Helper()
	timeout := time.After(2 * time.Second)
	for {
		select {
		case ev := <-ch:
			if ev.Type == eventType {
				return ev
			}
		case <-timeout:
			t.Fatalf("timed out waiting for %s event", eventType)
		}
	}
}

// resetChatLimit clears the per-node chat rate limit so a test can send
// several chat requests back to back.
func resetChatLimit(h *Hub, nodeID string) {
	h.chatLimiter.mu.Lock()
	defer h.chatLimiter.mu.Unlock()
	delete(h.chatLimiter.last, nodeID)
}

func setupChatTest(t *testing.T) (*Hub, *httptest.Server, *keystore.Keystore) {
	t.Helper()
	h, _ := setupTestHub(t)
	ts := httptest.NewServer(h.newMux())
	t.Cleanup(ts.Close)
	leafKS := loadTestKeystore(t, "leaf.key")
	registerLeaf(t, ts, leafKS)
	return h, ts, leafKS
}

func TestChatJoin_Validation(t *testing.T) {
	h, ts, leafKS := setupChatTest(t)
	url := ts.URL + "/v3net/v1/testnet/chat/rooms/join"

	cases := []struct {
		name, body string
	}{
		{"invalid JSON", `{"room":`},
		{"empty room", `{"room":"","handle":"alice"}`},
		{"bad room chars", `{"room":"lob_by!","handle":"alice"}`},
		{"missing handle", `{"room":"lobby"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var errResp map[string]string
			if code := sendSigned(t, leafKS, "POST", url, tc.body, &errResp); code != http.StatusBadRequest {
				t.Errorf("expected 400, got %d", code)
			}
			if errResp["error"] == "" {
				t.Errorf("expected JSON error body, got %v", errResp)
			}
		})
	}
	if rooms := h.chatRooms.RoomList("testnet"); len(rooms) != 0 {
		t.Errorf("rejected joins must not create rooms, got %+v", rooms)
	}
}

func TestChatJoin_NormalizesRoomAndBroadcasts(t *testing.T) {
	h, ts, leafKS := setupChatTest(t)
	ch, cancel := h.broadcaster.Subscribe("testnet")
	defer cancel()

	var resp protocol.ChatJoinResponse
	code := sendSigned(t, leafKS, "POST", ts.URL+"/v3net/v1/testnet/chat/rooms/join",
		`{"room":"Dev Talk","handle":"alice"}`, &resp)
	if code != http.StatusOK {
		t.Fatalf("join status: %d", code)
	}
	if len(resp.Rooms) != 1 || resp.Rooms[0].Name != "dev-talk" || resp.Rooms[0].UserCount != 1 {
		t.Errorf("expected one room dev-talk with 1 user, got %+v", resp.Rooms)
	}

	var payload protocol.ChatJoinPayload
	if err := json.Unmarshal(waitEvent(t, ch, protocol.EventChatJoin).Data, &payload); err != nil {
		t.Fatalf("decode join event: %v", err)
	}
	want := protocol.ChatJoinPayload{Room: "dev-talk", Handle: "alice", BBS: "Test BBS"}
	if payload != want {
		t.Errorf("join event = %+v, want %+v", payload, want)
	}

	// Joining again with the same handle must not duplicate the member.
	var again protocol.ChatJoinResponse
	sendSigned(t, leafKS, "POST", ts.URL+"/v3net/v1/testnet/chat/rooms/join",
		`{"room":"dev-talk","handle":"alice"}`, &again)
	if len(again.Users) != 1 {
		t.Errorf("duplicate join should keep 1 user, got %v", again.Users)
	}
}

func TestChatLeave(t *testing.T) {
	h, ts, leafKS := setupChatTest(t)
	base := ts.URL + "/v3net/v1/testnet/chat/rooms/"

	if code := sendSigned(t, leafKS, "POST", base+"join", `{"room":"lobby","handle":"alice"}`, nil); code != http.StatusOK {
		t.Fatalf("join status: %d", code)
	}
	if !h.chatRooms.IsJoined("testnet", "lobby", leafKS.NodeID(), "alice") {
		t.Fatal("alice should be joined after join")
	}

	ch, cancel := h.broadcaster.Subscribe("testnet")
	defer cancel()

	if code := sendSigned(t, leafKS, "POST", base+"leave", `{"room":"lobby","handle":"alice"}`, nil); code != http.StatusNoContent {
		t.Fatalf("leave status: %d", code)
	}

	var payload protocol.ChatLeavePayload
	if err := json.Unmarshal(waitEvent(t, ch, protocol.EventChatLeave).Data, &payload); err != nil {
		t.Fatalf("decode leave event: %v", err)
	}
	want := protocol.ChatLeavePayload{Room: "lobby", Handle: "alice", BBS: "Test BBS"}
	if payload != want {
		t.Errorf("leave event = %+v, want %+v", payload, want)
	}
	if h.chatRooms.IsJoined("testnet", "lobby", leafKS.NodeID(), "alice") {
		t.Error("alice should not be joined after leave")
	}
	// The last member leaving removes the room.
	if rooms := h.chatRooms.RoomList("testnet"); len(rooms) != 0 {
		t.Errorf("expected empty room list after last leave, got %+v", rooms)
	}
}

func TestChatLeave_Validation(t *testing.T) {
	_, ts, leafKS := setupChatTest(t)
	url := ts.URL + "/v3net/v1/testnet/chat/rooms/leave"

	if code := sendSigned(t, leafKS, "POST", url, `not json`, nil); code != http.StatusBadRequest {
		t.Errorf("invalid JSON: expected 400, got %d", code)
	}
	if code := sendSigned(t, leafKS, "POST", url, `{"room":"","handle":"alice"}`, nil); code != http.StatusBadRequest {
		t.Errorf("empty room: expected 400, got %d", code)
	}
}

func TestChatPost_Validation(t *testing.T) {
	h, ts, leafKS := setupChatTest(t)
	url := ts.URL + "/v3net/v1/testnet/chat/rooms/post"

	if code := sendSigned(t, leafKS, "POST", url, `{"room":`, nil); code != http.StatusBadRequest {
		t.Errorf("invalid JSON: expected 400, got %d", code)
	}
	resetChatLimit(h, leafKS.NodeID())
	if code := sendSigned(t, leafKS, "POST", url, `{"room":"bad room!","text":"x"}`, nil); code != http.StatusBadRequest {
		t.Errorf("bad room: expected 400, got %d", code)
	}
}

func TestChatPost_BroadcastsMessage(t *testing.T) {
	h, ts, leafKS := setupChatTest(t)
	base := ts.URL + "/v3net/v1/testnet/chat/rooms/"
	sendSigned(t, leafKS, "POST", base+"join", `{"room":"lobby","handle":"alice"}`, nil)

	ch, cancel := h.broadcaster.Subscribe("testnet")
	defer cancel()

	if code := sendSigned(t, leafKS, "POST", base+"post", `{"room":"lobby","text":"hi all"}`, nil); code != http.StatusNoContent {
		t.Fatalf("post status: %d", code)
	}

	var msg protocol.ChatMsgPayload
	if err := json.Unmarshal(waitEvent(t, ch, protocol.EventChatMessage).Data, &msg); err != nil {
		t.Fatalf("decode chat event: %v", err)
	}
	if msg.Room != "lobby" || msg.FromHandle != "alice" || msg.FromNode != leafKS.NodeID() ||
		msg.FromBBS != "Test BBS" || msg.Text != "hi all" {
		t.Errorf("unexpected chat event payload: %+v", msg)
	}
	if _, err := time.Parse(time.RFC3339, msg.Timestamp); err != nil {
		t.Errorf("timestamp %q is not RFC3339: %v", msg.Timestamp, err)
	}
}

func TestChatPost_StorageError(t *testing.T) {
	h, ts, leafKS := setupChatTest(t)
	base := ts.URL + "/v3net/v1/testnet/chat/rooms/"
	sendSigned(t, leafKS, "POST", base+"join", `{"room":"lobby","handle":"alice"}`, nil)

	if _, err := h.db.Exec(`DROP TABLE chat_history`); err != nil {
		t.Fatalf("drop chat_history: %v", err)
	}

	ch, cancel := h.broadcaster.Subscribe("testnet")
	defer cancel()

	if code := sendSigned(t, leafKS, "POST", base+"post", `{"room":"lobby","text":"lost"}`, nil); code != http.StatusInternalServerError {
		t.Errorf("expected 500 when history table is gone, got %d", code)
	}
	// A message that could not be stored must not be broadcast.
	select {
	case ev := <-ch:
		t.Errorf("unexpected event after storage failure: %s", ev.Type)
	default:
	}

	resp, err := http.Get(base + "lobby/history")
	if err != nil {
		t.Fatalf("GET history: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("history: expected 500 when table is gone, got %d", resp.StatusCode)
	}
}

func TestChatPrivate(t *testing.T) {
	h, ts, aliceKS := setupChatTest(t)
	bobKS := loadTestKeystore(t, "bob.key")
	registerLeaf(t, ts, bobKS)
	base := ts.URL + "/v3net/v1/testnet/chat/rooms/"

	ch, cancel := h.broadcaster.Subscribe("testnet")
	defer cancel()

	// Without a joined room the sender's handle falls back to its BBS name.
	body := fmt.Sprintf(`{"to_handle":"bob","to_node":%q,"text":"psst"}`, bobKS.NodeID())
	if code := sendSigned(t, aliceKS, "POST", base+"private", body, nil); code != http.StatusNoContent {
		t.Fatalf("private status: %d", code)
	}
	var msg protocol.ChatMsgPayload
	if err := json.Unmarshal(waitEvent(t, ch, protocol.EventChatPrivate).Data, &msg); err != nil {
		t.Fatalf("decode private event: %v", err)
	}
	if msg.FromHandle != "Test BBS" || msg.FromNode != aliceKS.NodeID() ||
		msg.ToHandle != "bob" || msg.ToNode != bobKS.NodeID() || msg.Text != "psst" || msg.Room != "" {
		t.Errorf("unexpected private event payload: %+v", msg)
	}

	// Once joined, the room handle is used as the sender.
	sendSigned(t, aliceKS, "POST", base+"join", `{"room":"lobby","handle":"alice"}`, nil)
	resetChatLimit(h, aliceKS.NodeID())
	if code := sendSigned(t, aliceKS, "POST", base+"private", body, nil); code != http.StatusNoContent {
		t.Fatalf("second private status: %d", code)
	}
	if err := json.Unmarshal(waitEvent(t, ch, protocol.EventChatPrivate).Data, &msg); err != nil {
		t.Fatalf("decode private event: %v", err)
	}
	if msg.FromHandle != "alice" {
		t.Errorf("expected from_handle alice once joined, got %q", msg.FromHandle)
	}

	// Both messages are persisted to the private history table only.
	rows, err := h.db.Query(`SELECT from_handle, to_handle, to_node, text FROM chat_private_history ORDER BY id`)
	if err != nil {
		t.Fatalf("query private history: %v", err)
	}
	defer rows.Close()
	var senders []string
	for rows.Next() {
		var from, toHandle, toNode, text string
		if err := rows.Scan(&from, &toHandle, &toNode, &text); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if toHandle != "bob" || toNode != bobKS.NodeID() || text != "psst" {
			t.Errorf("unexpected stored private message: %s -> %s@%s %q", from, toHandle, toNode, text)
		}
		senders = append(senders, from)
	}
	if got := strings.Join(senders, ","); got != "Test BBS,alice" {
		t.Errorf("stored senders = %q, want %q", got, "Test BBS,alice")
	}
	if hist, _ := h.chatStore.RoomHistory("testnet", "lobby", 50); len(hist) != 0 {
		t.Errorf("private messages must not appear in room history, got %+v", hist)
	}
}

func TestChatPrivate_Errors(t *testing.T) {
	h, ts, leafKS := setupChatTest(t)
	url := ts.URL + "/v3net/v1/testnet/chat/rooms/private"

	cases := []struct {
		name, body string
		want       int
	}{
		{"invalid JSON", `{"to_node":`, http.StatusBadRequest},
		{"missing to_node", `{"to_handle":"bob","text":"x"}`, http.StatusBadRequest},
		{"missing to_handle", `{"to_node":"abcd","text":"x"}`, http.StatusBadRequest},
		{"unknown target node", `{"to_handle":"bob","to_node":"0000000000000000","text":"x"}`, http.StatusNotFound},
	}
	// The cases run back to back with no limit reset: a rejected request must
	// not take the node's rate-limit token, so each one gets its own error
	// rather than a 429.
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if code := sendSigned(t, leafKS, "POST", url, tc.body, nil); code != tc.want {
				t.Errorf("expected %d, got %d", tc.want, code)
			}
		})
	}

	var count int
	if err := h.db.QueryRow(`SELECT COUNT(*) FROM chat_private_history`).Scan(&count); err != nil {
		t.Fatalf("count private history: %v", err)
	}
	if count != 0 {
		t.Errorf("rejected private messages must not be stored, got %d rows", count)
	}

	// The token is still there for the first valid message; an immediate
	// second one is throttled.
	valid := fmt.Sprintf(`{"to_handle":"me","to_node":%q,"text":"x"}`, leafKS.NodeID())
	if code := sendSigned(t, leafKS, "POST", url, valid, nil); code != http.StatusNoContent {
		t.Fatalf("valid private message after rejected ones: expected 204, got %d", code)
	}
	if code := sendSigned(t, leafKS, "POST", url, valid, nil); code != http.StatusTooManyRequests {
		t.Errorf("expected 429 for back-to-back private message, got %d", code)
	}
}

func TestChatPost_RejectedRequestsKeepRateLimitToken(t *testing.T) {
	h, ts, leafKS := setupChatTest(t)
	base := ts.URL + "/v3net/v1/testnet/chat/rooms/"

	// 400 (invalid JSON, bad room) and 403 (not joined) are all rejected
	// without touching the limiter.
	for _, body := range []string{`{"room":`, `{"room":"bad room!","text":"x"}`, `{"room":"lobby","text":"x"}`} {
		if code := sendSigned(t, leafKS, "POST", base+"post", body, nil); code != http.StatusBadRequest && code != http.StatusForbidden {
			t.Errorf("post %s: expected 400 or 403, got %d", body, code)
		}
	}

	sendSigned(t, leafKS, "POST", base+"join", `{"room":"lobby","handle":"alice"}`, nil)
	if code := sendSigned(t, leafKS, "POST", base+"post", `{"room":"lobby","text":"hi"}`, nil); code != http.StatusNoContent {
		t.Fatalf("first valid post after rejected ones: expected 204, got %d", code)
	}
	if code := sendSigned(t, leafKS, "POST", base+"post", `{"room":"lobby","text":"again"}`, nil); code != http.StatusTooManyRequests {
		t.Errorf("back-to-back post: expected 429, got %d", code)
	}
	// Room posts and private messages share the node's bucket.
	resetChatLimit(h, leafKS.NodeID())
	sendSigned(t, leafKS, "POST", base+"post", `{"room":"lobby","text":"one"}`, nil)
	private := fmt.Sprintf(`{"to_handle":"me","to_node":%q,"text":"x"}`, leafKS.NodeID())
	if code := sendSigned(t, leafKS, "POST", base+"private", private, nil); code != http.StatusTooManyRequests {
		t.Errorf("private right after a post: expected 429, got %d", code)
	}
}

func TestChatPrivate_StorageError(t *testing.T) {
	h, ts, leafKS := setupChatTest(t)
	if _, err := h.db.Exec(`DROP TABLE chat_private_history`); err != nil {
		t.Fatalf("drop chat_private_history: %v", err)
	}
	// A node may message itself; the target only has to be a subscriber.
	body := fmt.Sprintf(`{"to_handle":"me","to_node":%q,"text":"x"}`, leafKS.NodeID())
	code := sendSigned(t, leafKS, "POST", ts.URL+"/v3net/v1/testnet/chat/rooms/private", body, nil)
	if code != http.StatusInternalServerError {
		t.Errorf("expected 500 when private history table is gone, got %d", code)
	}
}

func TestChatTopic(t *testing.T) {
	h, ts, leafKS := setupChatTest(t)
	base := ts.URL + "/v3net/v1/testnet/chat/rooms/"
	topicBody := `{"room":"lobby","topic":"Friday night doors"}`

	// Setting a topic requires membership.
	if code := sendSigned(t, leafKS, "POST", base+"topic", topicBody, nil); code != http.StatusForbidden {
		t.Errorf("unjoined topic: expected 403, got %d", code)
	}
	if code := sendSigned(t, leafKS, "POST", base+"topic", `{"room":`, nil); code != http.StatusBadRequest {
		t.Errorf("invalid JSON: expected 400, got %d", code)
	}
	if code := sendSigned(t, leafKS, "POST", base+"topic", `{"room":"","topic":"x"}`, nil); code != http.StatusBadRequest {
		t.Errorf("empty room: expected 400, got %d", code)
	}

	sendSigned(t, leafKS, "POST", base+"join", `{"room":"lobby","handle":"alice"}`, nil)
	ch, cancel := h.broadcaster.Subscribe("testnet")
	defer cancel()

	if code := sendSigned(t, leafKS, "POST", base+"topic", topicBody, nil); code != http.StatusNoContent {
		t.Fatalf("topic status: %d", code)
	}
	var payload protocol.ChatTopicPayload
	if err := json.Unmarshal(waitEvent(t, ch, protocol.EventChatTopic).Data, &payload); err != nil {
		t.Fatalf("decode topic event: %v", err)
	}
	want := protocol.ChatTopicPayload{Room: "lobby", Topic: "Friday night doors", SetBy: "alice"}
	if payload != want {
		t.Errorf("topic event = %+v, want %+v", payload, want)
	}

	// The public room list reflects the new topic.
	resp, err := http.Get(ts.URL + "/v3net/v1/testnet/chat/rooms")
	if err != nil {
		t.Fatalf("GET rooms: %v", err)
	}
	defer resp.Body.Close()
	var rooms []protocol.ProtoChatRoomInfo
	if err := json.NewDecoder(resp.Body).Decode(&rooms); err != nil {
		t.Fatalf("decode rooms: %v", err)
	}
	if len(rooms) != 1 || rooms[0].Topic != "Friday night doors" || rooms[0].UserCount != 1 {
		t.Errorf("unexpected room list: %+v", rooms)
	}
}

func TestChatHistory_LimitAndValidation(t *testing.T) {
	h, ts, _ := setupChatTest(t)
	for i := 0; i < 5; i++ {
		// Explicit timestamps keep the ordering deterministic.
		if _, err := h.db.Exec(
			`INSERT INTO chat_history (network,room,from_handle,from_node,from_bbs,text,created_at) VALUES (?,?,?,?,?,?,?)`,
			"testnet", "lobby", "alice", "node1", "bbs1", fmt.Sprintf("msg %d", i),
			time.Now().UTC().Add(time.Duration(i-10)*time.Minute),
		); err != nil {
			t.Fatalf("seed history: %v", err)
		}
	}

	get := func(path string) (int, []protocol.ChatMsgPayload) {
		t.Helper()
		resp, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		defer resp.Body.Close()
		var msgs []protocol.ChatMsgPayload
		if resp.StatusCode == http.StatusOK {
			if err := json.NewDecoder(resp.Body).Decode(&msgs); err != nil {
				t.Fatalf("decode history: %v", err)
			}
		}
		return resp.StatusCode, msgs
	}

	// limit returns the most recent N, oldest first.
	code, msgs := get("/v3net/v1/testnet/chat/rooms/lobby/history?limit=2")
	if code != http.StatusOK || len(msgs) != 2 {
		t.Fatalf("limit=2: status %d, %d messages", code, len(msgs))
	}
	if msgs[0].Text != "msg 3" || msgs[1].Text != "msg 4" {
		t.Errorf("limit=2 returned %q, %q; want msg 3, msg 4", msgs[0].Text, msgs[1].Text)
	}

	// Oversized and unparseable limits still return everything available.
	for _, q := range []string{"?limit=9999", "?limit=abc", "?limit=-3"} {
		if code, msgs := get("/v3net/v1/testnet/chat/rooms/lobby/history" + q); code != http.StatusOK || len(msgs) != 5 {
			t.Errorf("%s: status %d, %d messages; want 200 and 5", q, code, len(msgs))
		}
	}

	if code, _ := get("/v3net/v1/testnet/chat/rooms/bad_room/history"); code != http.StatusBadRequest {
		t.Errorf("invalid room name: expected 400, got %d", code)
	}
}

func TestChatRooms_MembershipBookkeeping(t *testing.T) {
	cr := newChatRooms()

	// Operations on unknown networks and rooms are no-ops.
	cr.Leave("nonet", "lobby", "node1", "alice")
	if cr.IsJoined("nonet", "lobby", "node1", "alice") {
		t.Error("IsJoined on unknown network should be false")
	}
	if cr.Users("nonet", "lobby") != nil || cr.AnyHandleForNode("nonet", "node1") != "" {
		t.Error("unknown network should have no users or handles")
	}

	cr.Join("net", "lobby", "node1", "alice")
	cr.Join("net", "lobby", "node1", "carol")
	cr.Join("net", "lobby", "node2", "bob")
	cr.Leave("net", "other", "node1", "alice") // unknown room
	if cr.IsJoined("net", "other", "node1", "alice") {
		t.Error("IsJoined on unknown room should be false")
	}
	if cr.Users("net", "other") != nil {
		t.Error("unknown room should have no users")
	}

	if got := len(cr.Users("net", "lobby")); got != 3 {
		t.Errorf("expected 3 users, got %d", got)
	}
	if got := cr.AnyHandleForNode("net", "node2"); got != "bob" {
		t.Errorf("AnyHandleForNode(node2) = %q, want bob", got)
	}
	if got := cr.AnyHandleForNode("net", "node9"); got != "" {
		t.Errorf("AnyHandleForNode(unknown) = %q, want empty", got)
	}

	// Leaving with one handle keeps the node's other handle in the room.
	cr.Leave("net", "lobby", "node1", "alice")
	if cr.IsJoined("net", "lobby", "node1", "alice") {
		t.Error("alice should have left")
	}
	if !cr.IsJoined("net", "lobby", "node1", "carol") {
		t.Error("carol should still be joined")
	}
	if got := cr.HandleForNode("net", "lobby", "node1"); got != "carol" {
		t.Errorf("HandleForNode(node1) = %q, want carol", got)
	}

	// A topic set on a room with no members creates the room.
	cr.SetTopic("net", "empty", "placeholder")
	rooms := cr.RoomList("net")
	if len(rooms) != 2 || rooms[0].Name != "empty" || rooms[0].Topic != "placeholder" || rooms[0].UserCount != 0 {
		t.Errorf("unexpected room list after SetTopic: %+v", rooms)
	}

	// The room and then the network disappear once the last member leaves.
	cr.Leave("net", "lobby", "node1", "carol")
	cr.Leave("net", "lobby", "node2", "bob")
	if rooms := cr.RoomList("net"); len(rooms) != 1 || rooms[0].Name != "empty" {
		t.Errorf("lobby should be removed when empty, got %+v", rooms)
	}
}

func TestChatStore_SavePrivateAndPruner(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// A single connection keeps every query on the same in-memory database.
	db.SetMaxOpenConns(1)

	store, err := NewChatHistoryStore(db, 7)
	if err != nil {
		t.Fatalf("NewChatHistoryStore: %v", err)
	}
	if err := store.SavePrivate("net", "alice", "node1", "bob", "node2", "fresh"); err != nil {
		t.Fatalf("SavePrivate: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO chat_private_history (network,from_handle,from_node,to_handle,to_node,text,created_at) VALUES (?,?,?,?,?,?,?)`,
		"net", "alice", "node1", "bob", "node2", "stale", time.Now().UTC().AddDate(0, 0, -30),
	); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store.StartPruner(ctx)

	// The pruner runs once at startup; wait for the stale row to go.
	deadline := time.Now().Add(5 * time.Second)
	for {
		var texts []string
		rows, err := db.Query(`SELECT text FROM chat_private_history ORDER BY id`)
		if err != nil {
			t.Fatalf("query: %v", err)
		}
		for rows.Next() {
			var s string
			if err := rows.Scan(&s); err != nil {
				t.Fatalf("scan: %v", err)
			}
			texts = append(texts, s)
		}
		rows.Close()
		if len(texts) == 1 && texts[0] == "fresh" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("pruner did not remove stale row; rows = %v", texts)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestChatStore_ErrorsOnClosedDB(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewChatHistoryStore(db, 7)
	if err != nil {
		t.Fatalf("NewChatHistoryStore: %v", err)
	}
	db.Close()

	if err := store.SaveMessage("net", "lobby", "a", "n", "b", "x"); err == nil {
		t.Error("SaveMessage on closed DB should fail")
	}
	if err := store.SavePrivate("net", "a", "n", "b", "n2", "x"); err == nil {
		t.Error("SavePrivate on closed DB should fail")
	}
	if _, err := store.RoomHistory("net", "lobby", 10); err == nil {
		t.Error("RoomHistory on closed DB should fail")
	}
	if err := store.prune(); err == nil {
		t.Error("prune on closed DB should fail")
	}
	if _, err := NewChatHistoryStore(db, 7); err == nil {
		t.Error("NewChatHistoryStore on closed DB should fail")
	}
}
