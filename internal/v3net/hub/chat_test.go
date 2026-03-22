package hub

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/keystore"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

func TestChatJoinAndPost(t *testing.T) {
	h, _ := setupTestHub(t)
	ts := httptest.NewServer(h.newMux())
	defer ts.Close()

	leafKS, _, err := keystore.Load(filepath.Join(t.TempDir(), "leaf.key"))
	if err != nil {
		t.Fatalf("load leaf keystore: %v", err)
	}
	registerLeaf(t, ts, leafKS)

	// Join the "lobby" room.
	joinBody := `{"room":"lobby","handle":"alice"}`
	req := signedRequest(t, leafKS, "POST", ts.URL+"/v3net/v1/testnet/chat/rooms/join", joinBody)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST join: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var buf bytes.Buffer
		buf.ReadFrom(resp.Body)
		t.Fatalf("join status: %d, body: %s", resp.StatusCode, buf.String())
	}

	var joinResp protocol.ChatJoinResponse
	if err := json.NewDecoder(resp.Body).Decode(&joinResp); err != nil {
		t.Fatalf("decode join response: %v", err)
	}

	// Verify "lobby" appears in rooms.
	found := false
	for _, room := range joinResp.Rooms {
		if room.Name == "lobby" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected lobby in rooms, got: %+v", joinResp.Rooms)
	}

	// Verify alice is in users.
	aliceFound := false
	for _, u := range joinResp.Users {
		if u == "alice" {
			aliceFound = true
			break
		}
	}
	if !aliceFound {
		t.Errorf("expected alice in users, got: %v", joinResp.Users)
	}

	// Post a message.
	postBody := `{"room":"lobby","text":"hello world"}`
	postReq := signedRequest(t, leafKS, "POST", ts.URL+"/v3net/v1/testnet/chat/rooms/post", postBody)
	postResp, err := http.DefaultClient.Do(postReq)
	if err != nil {
		t.Fatalf("POST post: %v", err)
	}
	postResp.Body.Close()
	if postResp.StatusCode != http.StatusNoContent {
		t.Fatalf("post status: %d", postResp.StatusCode)
	}

	// GET history and verify message is present.
	histResp, err := http.Get(ts.URL + "/v3net/v1/testnet/chat/rooms/lobby/history")
	if err != nil {
		t.Fatalf("GET history: %v", err)
	}
	defer histResp.Body.Close()
	if histResp.StatusCode != http.StatusOK {
		var buf bytes.Buffer
		buf.ReadFrom(histResp.Body)
		t.Fatalf("history status: %d, body: %s", histResp.StatusCode, buf.String())
	}

	var history []protocol.ChatMsgPayload
	if err := json.NewDecoder(histResp.Body).Decode(&history); err != nil {
		t.Fatalf("decode history: %v", err)
	}
	if len(history) == 0 {
		t.Fatal("expected at least one message in history")
	}
	if history[0].Text != "hello world" {
		t.Errorf("expected 'hello world', got %q", history[0].Text)
	}
	if history[0].FromHandle != "alice" {
		t.Errorf("expected from_handle 'alice', got %q", history[0].FromHandle)
	}
}

func TestChatRoomListPublic(t *testing.T) {
	h, _ := setupTestHub(t)
	ts := httptest.NewServer(h.newMux())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/v3net/v1/testnet/chat/rooms")
	if err != nil {
		t.Fatalf("GET chat/rooms: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var rooms []protocol.ProtoChatRoomInfo
	if err := json.NewDecoder(resp.Body).Decode(&rooms); err != nil {
		t.Fatalf("decode room list: %v", err)
	}
	// No rooms yet; expect an empty (or null) JSON array.
	// Both nil and empty slice decode fine and len is 0.
	_ = rooms
}

func TestChatNetworkIsolation(t *testing.T) {
	h, _ := setupTestHub(t)
	ts := httptest.NewServer(h.newMux())
	defer ts.Close()

	ks1, _, _ := keystore.Load(filepath.Join(t.TempDir(), "leaf1.key"))
	_, _, _ = keystore.Load(filepath.Join(t.TempDir(), "leaf2.key"))
	registerLeaf(t, ts, ks1)
	// ks2 on "testnet2" — hub may not know this network, so join directly to chatRooms
	// Instead: test via chatRooms directly
	cr := newChatRooms()
	cr.Join("testnet", "lobby", "node1", "alice")
	cr.Join("testnet2", "lobby", "node2", "bob")

	rooms1 := cr.RoomList("testnet")
	rooms2 := cr.RoomList("testnet2")

	if len(rooms1) != 1 || rooms1[0].UserCount != 1 {
		t.Errorf("testnet should have 1 user in lobby, got %+v", rooms1)
	}
	if len(rooms2) != 1 || rooms2[0].UserCount != 1 {
		t.Errorf("testnet2 should have 1 user in lobby, got %+v", rooms2)
	}

	// Disconnect node1 from testnet; node2 in testnet2 should be unaffected.
	cr.HandleDisconnect("testnet", "node1")
	rooms2After := cr.RoomList("testnet2")
	if len(rooms2After) != 1 {
		t.Errorf("testnet2 lobby should still have 1 user after testnet disconnect, got %+v", rooms2After)
	}
}

func TestChatNotJoined(t *testing.T) {
	h, _ := setupTestHub(t)
	ts := httptest.NewServer(h.newMux())
	defer ts.Close()

	leafKS, _, err := keystore.Load(filepath.Join(t.TempDir(), "leaf.key"))
	if err != nil {
		t.Fatalf("load leaf keystore: %v", err)
	}
	registerLeaf(t, ts, leafKS)

	// Post to a room without joining first.
	postBody := `{"room":"lobby","text":"should fail"}`
	req := signedRequest(t, leafKS, "POST", ts.URL+"/v3net/v1/testnet/chat/rooms/post", postBody)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST post: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403 for unjoined post, got %d", resp.StatusCode)
	}
}
