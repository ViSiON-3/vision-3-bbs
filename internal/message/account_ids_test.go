package message

import (
	"os"
	"path/filepath"
	"testing"
)

// The account ID decides when the message and the caller both have one
// (#1048); otherwise the handle does.
func TestVisibilityByAccountID(t *testing.T) {
	m := &DisplayMessage{IsPrivate: true, From: "Bob", To: "Ann", SenderID: 3, RecipientID: 4}
	for _, tc := range []struct {
		name   string
		handle string
		id     int
		see    bool
		mail   bool
		sent   bool
	}{
		{"recipient renamed", "Annie", 4, true, true, false},
		{"sender renamed", "Robert", 3, true, false, true},
		{"new account with the recipient's old handle", "Ann", 9, false, false, false},
		{"new account with the sender's old handle", "Bob", 9, false, false, false},
		{"caller without an ID matches by handle", "Ann", 0, true, true, false},
		{"stranger", "Carl", 7, false, false, false},
	} {
		if got := m.VisibleTo(tc.handle, tc.id); got != tc.see {
			t.Errorf("%s: VisibleTo = %v", tc.name, got)
		}
		if got := m.MailFor(tc.handle, tc.id); got != tc.mail {
			t.Errorf("%s: MailFor = %v", tc.name, got)
		}
		if got := m.SentBy(tc.handle, tc.id); got != tc.sent {
			t.Errorf("%s: SentBy = %v", tc.name, got)
		}
	}
	// A message from before IDs matches by handle, whatever the caller's ID.
	old := &DisplayMessage{IsPrivate: true, From: "Bob", To: "Ann"}
	if !old.MailFor("ann", 9) || old.MailFor("Annie", 4) {
		t.Error("a message without IDs didn't match by handle")
	}
	if !m.WrittenBy("Robert", 3) || m.WrittenBy("Bob", 9) {
		t.Error("WrittenBy")
	}
}

// The IDs are stored with the message, read back, and kept by a move.
func TestAccountIDsRoundTrip(t *testing.T) {
	tmp := t.TempDir()
	cfg := filepath.Join(tmp, "config")
	if err := os.MkdirAll(cfg, 0o755); err != nil {
		t.Fatal(err)
	}
	writeMsgAreas(t, cfg, `[
		{"id":1,"position":1,"tag":"A","name":"A","area_type":"local"},
		{"id":2,"position":2,"tag":"B","name":"B","area_type":"local"}]`)
	mm, err := NewMessageManager(tmp, cfg, "TestBBS", nil)
	if err != nil {
		t.Fatal(err)
	}
	n, err := mm.Post(1, "Bob", "Ann", "hi", "body", PostOptions{Private: true, SenderID: 3, RecipientID: 4})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mm.Post(1, "Bob", "All", "public", "body", PostOptions{SenderID: 3, RecipientID: 4}); err != nil {
		t.Fatal(err)
	}
	m, err := mm.GetMessage(1, n)
	if err != nil || m.SenderID != 3 || m.RecipientID != 4 {
		t.Fatalf("private message read back: %+v, %v", m, err)
	}
	pub, _ := mm.GetMessage(1, n+1)
	if pub.SenderID != 3 || pub.RecipientID != 0 {
		t.Errorf("public message: SenderID %d RecipientID %d, want 3 and none", pub.SenderID, pub.RecipientID)
	}
	moved, err := mm.MoveMessageIf(1, n+1, 2, func(*DisplayMessage) bool { return true }, func(m *DisplayMessage) string { return m.Body })
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := mm.GetMessage(2, moved); got.SenderID != 3 {
		t.Errorf("moved message SenderID = %d, want 3", got.SenderID)
	}
}
