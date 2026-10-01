package hub

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

func TestRateLimiter_Burst(t *testing.T) {
	rl := newBurstRateLimiter(time.Minute, 3)
	defer rl.Stop()

	for i := range 3 {
		if !rl.Allow("k") {
			t.Fatalf("request %d of a burst of 3 was refused", i+1)
		}
	}
	if rl.Allow("k") {
		t.Error("request beyond the burst was allowed")
	}
	if !rl.Allow("other") {
		t.Error("a different key should have its own allowance")
	}

	// Moving the key's allowance back one interval frees exactly one request.
	rl.mu.Lock()
	rl.tat["k"] = rl.tat["k"].Add(-time.Minute)
	rl.mu.Unlock()
	if !rl.Allow("k") {
		t.Error("request after one interval was refused")
	}
	if rl.Allow("k") {
		t.Error("only one request should be freed per interval")
	}
}

// Two users on the same BBS must not throttle each other, and each post is
// credited to the user who sent it.
func TestChatPost_UsersOnOneNodeHaveSeparateLimits(t *testing.T) {
	h, ts, leafKS := setupChatTest(t)
	base := ts.URL + "/v3net/v1/testnet/chat/rooms/"
	sendSigned(t, leafKS, "POST", base+"join", `{"room":"lobby","handle":"alice"}`, nil)
	sendSigned(t, leafKS, "POST", base+"join", `{"room":"lobby","handle":"bob"}`, nil)

	ch, cancel := h.broadcaster.Subscribe("testnet")
	defer cancel()

	if code := sendSigned(t, leafKS, "POST", base+"post", `{"room":"lobby","text":"from alice","handle":"alice"}`, nil); code != http.StatusNoContent {
		t.Fatalf("alice post: expected 204, got %d", code)
	}
	if code := sendSigned(t, leafKS, "POST", base+"post", `{"room":"lobby","text":"from bob","handle":"bob"}`, nil); code != http.StatusNoContent {
		t.Fatalf("bob post straight after alice: expected 204, got %d", code)
	}
	if code := sendSigned(t, leafKS, "POST", base+"post", `{"room":"lobby","text":"again","handle":"alice"}`, nil); code != http.StatusTooManyRequests {
		t.Errorf("alice back-to-back post: expected 429, got %d", code)
	}

	for _, want := range []string{"alice", "bob"} {
		var msg protocol.ChatMsgPayload
		if err := json.Unmarshal(waitEvent(t, ch, protocol.EventChatMessage).Data, &msg); err != nil {
			t.Fatalf("decode chat event: %v", err)
		}
		if msg.FromHandle != want || msg.Text != "from "+want {
			t.Errorf("chat event from %q with %q, want %q", msg.FromHandle, msg.Text, want)
		}
	}
	hist, err := h.chatStore.RoomHistory("testnet", "lobby", 10)
	if err != nil {
		t.Fatalf("room history: %v", err)
	}
	if len(hist) != 2 || hist[0].FromHandle != "alice" || hist[1].FromHandle != "bob" {
		t.Errorf("room history = %+v, want one post each from alice and bob", hist)
	}
}

func TestChatPost_HandleMustBeJoined(t *testing.T) {
	h, ts, leafKS := setupChatTest(t)
	base := ts.URL + "/v3net/v1/testnet/chat/rooms/"
	sendSigned(t, leafKS, "POST", base+"join", `{"room":"lobby","handle":"alice"}`, nil)

	if code := sendSigned(t, leafKS, "POST", base+"post", `{"room":"lobby","text":"x","handle":"mallory"}`, nil); code != http.StatusForbidden {
		t.Errorf("post as an unjoined handle: expected 403, got %d", code)
	}
	if code := sendSigned(t, leafKS, "POST", base+"topic", `{"room":"lobby","topic":"x","handle":"mallory"}`, nil); code != http.StatusForbidden {
		t.Errorf("topic as an unjoined handle: expected 403, got %d", code)
	}

	// A topic request names its setter.
	sendSigned(t, leafKS, "POST", base+"join", `{"room":"lobby","handle":"bob"}`, nil)
	ch, cancel := h.broadcaster.Subscribe("testnet")
	defer cancel()
	if code := sendSigned(t, leafKS, "POST", base+"topic", `{"room":"lobby","topic":"doors","handle":"bob"}`, nil); code != http.StatusNoContent {
		t.Fatalf("topic as bob: expected 204, got %d", code)
	}
	var payload protocol.ChatTopicPayload
	if err := json.Unmarshal(waitEvent(t, ch, protocol.EventChatTopic).Data, &payload); err != nil {
		t.Fatalf("decode topic event: %v", err)
	}
	if payload.SetBy != "bob" {
		t.Errorf("topic set by %q, want bob", payload.SetBy)
	}
}

func TestChatPrivate_UsesSenderHandle(t *testing.T) {
	h, ts, leafKS := setupChatTest(t)
	url := ts.URL + "/v3net/v1/testnet/chat/rooms/private"
	ch, cancel := h.broadcaster.Subscribe("testnet")
	defer cancel()

	// Users on one node send private messages back to back; none has
	// joined a room, and each is credited by name.
	for _, from := range []string{"alice", "bob"} {
		body := fmt.Sprintf(`{"to_handle":"carol","to_node":%q,"text":"x","handle":%q}`, leafKS.NodeID(), from)
		if code := sendSigned(t, leafKS, "POST", url, body, nil); code != http.StatusNoContent {
			t.Fatalf("private from %s: expected 204, got %d", from, code)
		}
		var msg protocol.ChatMsgPayload
		if err := json.Unmarshal(waitEvent(t, ch, protocol.EventChatPrivate).Data, &msg); err != nil {
			t.Fatalf("decode private event: %v", err)
		}
		if msg.FromHandle != from {
			t.Errorf("private event from %q, want %q", msg.FromHandle, from)
		}
	}
	body := fmt.Sprintf(`{"to_handle":"carol","to_node":%q,"text":"x","handle":"alice"}`, leafKS.NodeID())
	if code := sendSigned(t, leafKS, "POST", url, body, nil); code != http.StatusTooManyRequests {
		t.Errorf("alice back-to-back private: expected 429, got %d", code)
	}
}

// freezeNodeLimit swaps in a node-wide chat limiter with the usual burst but
// an hour-long refill, so a test sees the burst run out however slowly the
// machine sends its requests.
func freezeNodeLimit(t *testing.T, h *Hub) {
	t.Helper()
	h.chatNodeLimiter.Stop()
	h.chatNodeLimiter = newBurstRateLimiter(time.Hour, chatNodeBurst)
}

// One node cannot get around the per-user limit by sending as many handles.
func TestChat_NodeWideCap(t *testing.T) {
	h, ts, leafKS := setupChatTest(t)
	freezeNodeLimit(t, h)
	url := ts.URL + "/v3net/v1/testnet/chat/rooms/private"

	send := func(i int) int {
		body := fmt.Sprintf(`{"to_handle":"carol","to_node":%q,"text":"x","handle":"user%d"}`, leafKS.NodeID(), i)
		return sendSigned(t, leafKS, "POST", url, body, nil)
	}
	for i := range chatNodeBurst {
		if code := send(i); code != http.StatusNoContent {
			t.Fatalf("message %d of the node burst: expected 204, got %d", i+1, code)
		}
	}
	if code := send(chatNodeBurst); code != http.StatusTooManyRequests {
		t.Errorf("message beyond the node burst: expected 429, got %d", code)
	}

	// Another node has its own cap.
	otherKS := loadTestKeystore(t, "other.key")
	registerLeaf(t, ts, otherKS)
	body := fmt.Sprintf(`{"to_handle":"carol","to_node":%q,"text":"x","handle":"user0"}`, leafKS.NodeID())
	if code := sendSigned(t, otherKS, "POST", url, body, nil); code != http.StatusNoContent {
		t.Errorf("first message from another node: expected 204, got %d", code)
	}
}

// A message refused by the node cap must not count against its user: once
// the node has room again, the user's retry goes through at once.
func TestChat_NodeCapRefusalCostsUserNothing(t *testing.T) {
	h, ts, leafKS := setupChatTest(t)
	freezeNodeLimit(t, h)
	url := ts.URL + "/v3net/v1/testnet/chat/rooms/private"
	send := func(handle string) int {
		body := fmt.Sprintf(`{"to_handle":"carol","to_node":%q,"text":"x","handle":%q}`, leafKS.NodeID(), handle)
		return sendSigned(t, leafKS, "POST", url, body, nil)
	}

	for i := range chatNodeBurst {
		if code := send(fmt.Sprintf("user%d", i)); code != http.StatusNoContent {
			t.Fatalf("filling the node burst, message %d: expected 204, got %d", i+1, code)
		}
	}
	if code := send("alice"); code != http.StatusTooManyRequests {
		t.Fatalf("alice with the node cap spent: expected 429, got %d", code)
	}
	h.chatLimiter.mu.Lock()
	n := len(h.chatLimiter.tat)
	h.chatLimiter.mu.Unlock()
	if n != chatNodeBurst {
		t.Errorf("per-user limiter holds %d entries, want %d: a refused message must not add one", n, chatNodeBurst)
	}

	// Give the node room again without touching any user's allowance.
	h.chatNodeLimiter.mu.Lock()
	clear(h.chatNodeLimiter.tat)
	h.chatNodeLimiter.mu.Unlock()
	if code := send("alice"); code != http.StatusNoContent {
		t.Errorf("alice's retry once the node has room: expected 204, got %d", code)
	}
}

// When the user's own allowance refuses a message, the node's is untouched.
func TestRateLimiter_AllowBothTakesNeitherOnRefusal(t *testing.T) {
	user := newRateLimiter(time.Hour)
	defer user.Stop()
	node := newBurstRateLimiter(time.Hour, 2)
	defer node.Stop()

	if !allowBoth(user, "alice", node, "n1") {
		t.Fatal("first message refused")
	}
	if allowBoth(user, "alice", node, "n1") {
		t.Fatal("alice's second message within the interval was allowed")
	}
	// The refusal above left the node's second slot for another user.
	if !allowBoth(user, "bob", node, "n1") {
		t.Error("bob was refused: alice's refused message used up the node allowance")
	}
	if allowBoth(user, "carol", node, "n1") {
		t.Error("node burst of 2 allowed a third message")
	}
	user.mu.Lock()
	_, carolKept := user.tat["carol"]
	user.mu.Unlock()
	if carolKept {
		t.Error("a message refused by the node cap left an entry for its user")
	}
}
