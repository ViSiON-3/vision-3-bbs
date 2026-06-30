package jam

import (
	"path/filepath"
	"testing"
)

func TestWriteMessage_PreservesReplyTo(t *testing.T) {
	b, err := Open(filepath.Join(t.TempDir(), "base"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer b.Close()

	msg := NewMessage()
	msg.From, msg.To, msg.Subject, msg.Text = "alice", "bob", "Hi", "body"
	msg.ReplyTo = 5

	n, err := b.WriteMessage(msg)
	if err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}
	got, err := b.ReadMessage(n)
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	if got.Header == nil || got.Header.ReplyTo != 5 {
		t.Errorf("ReplyTo: want 5, got %+v", got.Header)
	}
}

func TestWriteMessage_ZeroReplyTo(t *testing.T) {
	b, err := Open(filepath.Join(t.TempDir(), "base"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer b.Close()

	msg := NewMessage()
	msg.From, msg.To, msg.Subject, msg.Text = "alice", "bob", "Hi", "body"

	n, err := b.WriteMessage(msg)
	if err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}
	got, err := b.ReadMessage(n)
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	if got.Header != nil && got.Header.ReplyTo != 0 {
		t.Errorf("ReplyTo: want 0, got %d", got.Header.ReplyTo)
	}
}

func TestWriteMessageExt_PreservesReplyTo(t *testing.T) {
	b := openTestBase(t)

	msg := NewMessage()
	msg.From, msg.To, msg.Subject, msg.Text = "u1", "u2", "Re", "reply"
	msg.ReplyTo = 3

	n, err := b.WriteMessageExt(msg, MsgTypeLocalMsg, "", "Test BBS", "")
	if err != nil {
		t.Fatalf("WriteMessageExt: %v", err)
	}
	got, err := b.ReadMessage(n)
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	if got.Header == nil || got.Header.ReplyTo != 3 {
		t.Errorf("ReplyTo: want 3, got %+v", got.Header)
	}
}
