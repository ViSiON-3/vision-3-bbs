package hub

import (
	"net/http"
	"testing"
	"time"
)

func TestChatDisconnectKeepsReplacementStreamMembership(t *testing.T) {
	h, ts, ks := setupChatTest(t)
	url := ts.URL + "/v3net/v1/testnet/events"
	var streams []*http.Response
	for range 2 {
		resp, err := http.DefaultClient.Do(signedRequest(t, ks, "GET", url, ""))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("stream status: %d", resp.StatusCode)
		}
		streams = append(streams, resp)
	}
	base := ts.URL + "/v3net/v1/testnet/chat/rooms/"
	if code := sendSigned(t, ks, "POST", base+"join", `{"room":"lobby","handle":"alice"}`, nil); code != http.StatusOK {
		t.Fatalf("join: %d", code)
	}
	streams[0].Body.Close()
	deadline := time.Now().Add(5 * time.Second)
	for {
		h.online.mu.Lock()
		count := h.online.streams[ks.NodeID()+":testnet"]
		h.online.mu.Unlock()
		if count == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("old stream was not removed")
		}
		time.Sleep(time.Millisecond)
	}
	// Wait for all cleanup under the lifecycle lock, not just the counter write.
	h.chatStreamMu.Lock()
	joined := h.chatRooms.IsJoined("testnet", "lobby", ks.NodeID(), "alice")
	h.chatStreamMu.Unlock()
	if !joined {
		t.Fatal("old stream removed replacement membership")
	}
	if code := sendSigned(t, ks, "POST", base+"post", `{"room":"lobby","handle":"alice","text":"still connected"}`, nil); code != http.StatusNoContent {
		t.Fatalf("post through replacement: %d", code)
	}
	streams[1].Body.Close()
	deadline = time.Now().Add(5 * time.Second)
	for h.chatRooms.IsJoined("testnet", "lobby", ks.NodeID(), "alice") {
		if time.Now().After(deadline) {
			t.Fatal("last stream did not clean up membership")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestChatLeaveRepeatedCleanupAnnouncesOnce(t *testing.T) {
	h, ts, ks := setupChatTest(t)
	base := ts.URL + "/v3net/v1/testnet/chat/rooms/"
	if code := sendSigned(t, ks, "POST", base+"join", `{"room":"lobby","handle":"alice"}`, nil); code != http.StatusOK {
		t.Fatalf("join: %d", code)
	}
	events, cancel := h.broadcaster.Subscribe("testnet")
	defer cancel()
	for range 2 {
		if code := sendSigned(t, ks, "POST", base+"leave", `{"room":"lobby","handle":"alice"}`, nil); code != http.StatusNoContent {
			t.Fatalf("leave: %d", code)
		}
	}
	if len(events) != 1 {
		t.Fatalf("leave announcements: %d, want 1", len(events))
	}
}
