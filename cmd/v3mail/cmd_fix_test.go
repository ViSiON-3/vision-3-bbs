package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/jam"
)

// seedReplyBase writes two messages from the same address and a third that
// replies to the second with the given ReplyID, returning the base path.
func seedReplyBase(t *testing.T, replyID string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "echo")
	b, err := jam.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	for _, ids := range [][2]string{
		{"21:1/100 00000001", ""},
		{"21:1/100 00000002", ""},
		{"21:1/200 00000001", replyID},
	} {
		msg := jam.NewMessage()
		msg.From, msg.To, msg.Subject, msg.Text = "alice", "All", "Hi", "body"
		msg.MsgID, msg.ReplyID = ids[0], ids[1]
		if _, err := b.WriteMessage(msg); err != nil {
			t.Fatalf("WriteMessage: %v", err)
		}
	}
	if err := b.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	return path
}

// readMessage opens the base at path and returns message n.
func readMessage(t *testing.T, path string, n int) *jam.Message {
	t.Helper()
	b, err := jam.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = b.Close() }()
	msg, err := b.ReadMessage(n)
	if err != nil {
		t.Fatalf("ReadMessage(%d): %v", n, err)
	}
	return msg
}

func TestFixAcceptsAddressSerialReplyID(t *testing.T) {
	path := seedReplyBase(t, "21:1/100 00000002")

	code, out, _ := runV3mail(t, t.TempDir(), "fix", path)
	if code != 0 {
		t.Errorf("fix exit code = %d, want 0\n%s", code, out)
	}
	if strings.Contains(out, "Malformed ReplyID") {
		t.Errorf("fix flagged a well-formed ReplyID:\n%s", out)
	}
}

// A repair followed by link must still thread the reply onto the message it
// names, not the first message from the same address.
func TestFixRepairThenLinkKeepsParent(t *testing.T) {
	for _, tc := range []struct {
		name    string
		replyID string
	}{
		{"well-formed", "21:1/100 00000002"},
		{"extra tokens", "21:1/100 00000002 21:1/100 00000003"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := seedReplyBase(t, tc.replyID)

			if code, out, _ := runV3mail(t, t.TempDir(), "fix", "--repair", path); code != 0 {
				t.Fatalf("fix --repair exit code = %d, want 0\n%s", code, out)
			}
			if got := readMessage(t, path, 3).ReplyID; got != "21:1/100 00000002" {
				t.Errorf("ReplyID after repair = %q, want %q", got, "21:1/100 00000002")
			}

			if code, out, _ := runV3mail(t, t.TempDir(), "link", path); code != 0 {
				t.Fatalf("link exit code = %d, want 0\n%s", code, out)
			}
			if got := readMessage(t, path, 3).Header.ReplyTo; got != 2 {
				t.Errorf("ReplyTo after link = %d, want 2", got)
			}
			if got := readMessage(t, path, 2).Header.Reply1st; got != 3 {
				t.Errorf("parent Reply1st after link = %d, want 3", got)
			}
		})
	}
}
