package chat

import (
	"path/filepath"
	"testing"
	"time"
)

func resetSharedState() {
	sharedMu.Lock()
	sharedRooms = make(map[string]*localRoom)
	sharedMu.Unlock()
}

func TestLocalChatJoinAndPost(t *testing.T) {
	resetSharedState()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "chat.db")

	alice, err := NewLocalChatService("alice", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer alice.Close()

	bob, err := NewLocalChatService("bob", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer bob.Close()

	if _, _, err := alice.Join("lobby"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := bob.Join("lobby"); err != nil {
		t.Fatal(err)
	}

	if err := alice.Post("lobby", "hello bob"); err != nil {
		t.Fatal(err)
	}

	select {
	case ev := <-bob.Events():
		if ev.Type != TypeMessage || ev.Message.Text != "hello bob" {
			t.Fatalf("unexpected event: %+v", ev)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("bob did not receive message")
	}
}

func TestLocalChatHistory(t *testing.T) {
	resetSharedState()
	dir := t.TempDir()
	alice, err := NewLocalChatService("alice", filepath.Join(dir, "chat.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer alice.Close()

	if _, _, err := alice.Join("lobby"); err != nil {
		t.Fatal(err)
	}
	if err := alice.Post("lobby", "msg1"); err != nil {
		t.Fatal(err)
	}
	if err := alice.Post("lobby", "msg2"); err != nil {
		t.Fatal(err)
	}

	history, err := alice.History("lobby", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(history))
	}
	if history[0].Text != "msg1" || history[1].Text != "msg2" {
		t.Fatalf("wrong order: %v %v", history[0].Text, history[1].Text)
	}
}

func TestLocalChatPrivate(t *testing.T) {
	resetSharedState()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "chat.db")
	alice, err := NewLocalChatService("alice", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer alice.Close()
	bob, err := NewLocalChatService("bob", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer bob.Close()

	if _, _, err := alice.Join("lobby"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := bob.Join("lobby"); err != nil {
		t.Fatal(err)
	}

	if err := alice.Private("bob", "", "secret"); err != nil {
		t.Fatal(err)
	}

	select {
	case ev := <-bob.Events():
		if ev.Type != TypePrivate || ev.Message.Text != "secret" {
			t.Fatalf("unexpected: %+v", ev)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("bob did not receive private message")
	}
}
