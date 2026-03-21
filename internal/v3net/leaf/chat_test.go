package leaf

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/chat"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

func TestDispatch_ChatMessage_DeliveredToCorrectRoom(t *testing.T) {
	reg := newChatSessionRegistry()

	alice := &ChatSession{handle: "alice", currentRoom: "lobby", events: make(chan chat.ChatEvent, 4)}
	bob := &ChatSession{handle: "bob", currentRoom: "offtopic", events: make(chan chat.ChatEvent, 4)}
	reg.register(alice)
	reg.register(bob)

	payload, _ := json.Marshal(protocol.ChatMsgPayload{
		Room: "lobby", FromHandle: "charlie", Text: "hi",
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
	reg.dispatch(protocol.Event{Type: protocol.EventChatMessage, Data: payload})

	select {
	case ev := <-alice.events:
		if ev.Type != chat.TypeMessage {
			t.Fatalf("expected TypeMessage, got %v", ev.Type)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("alice did not receive message")
	}

	select {
	case <-bob.events:
		t.Fatal("bob should not receive message for different room")
	case <-time.After(20 * time.Millisecond):
		// correct
	}
}

func TestDispatch_Private_DeliveredToTarget(t *testing.T) {
	reg := newChatSessionRegistry()
	alice := &ChatSession{handle: "alice", currentRoom: "lobby", events: make(chan chat.ChatEvent, 4)}
	bob := &ChatSession{handle: "bob", currentRoom: "lobby", events: make(chan chat.ChatEvent, 4)}
	reg.register(alice)
	reg.register(bob)

	payload, _ := json.Marshal(protocol.ChatMsgPayload{
		ToHandle: "alice", ToNode: "anynode",
		FromHandle: "bob", Text: "psst",
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
	reg.dispatch(protocol.Event{Type: protocol.EventChatPrivate, Data: payload})

	select {
	case ev := <-alice.events:
		if ev.Type != chat.TypePrivate {
			t.Fatalf("expected TypePrivate")
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("alice did not receive private message")
	}
	select {
	case <-bob.events:
		t.Fatal("bob should not receive private message addressed to alice")
	case <-time.After(20 * time.Millisecond):
	}
}
