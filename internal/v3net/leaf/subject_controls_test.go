package leaf

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/keystore"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// A message the hub already holds with a control character in its subject is
// imported cleaned rather than lost, its signature checked against what the
// author signed. One whose subject was altered is still refused.
func TestPollCleansSubjectControls(t *testing.T) {
	author, _, err := keystore.Load(filepath.Join(t.TempDir(), "author.key"))
	if err != nil {
		t.Fatal(err)
	}
	signed := func(n int, subject string) protocol.Message {
		m := testMessage(sigUUID(n))
		m.OriginNode = author.NodeID()
		m.Subject = subject
		if err := m.Sign(author); err != nil {
			t.Fatal(err)
		}
		return m
	}
	good := signed(1, "Signed\x1b[2Jone")
	altered := signed(2, "Signed\x1b[2Jtwo")
	altered.Subject = "Altered\x1b[2Jtwo"
	unsigned := testMessage(sigUUID(3))
	unsigned.Subject = "Unsigned\u009b2J"

	ts, _ := newMockHub(t, []protocol.Message{good, altered, unsigned})
	writer := &mockJAMWriter{}
	l, _ := setupLeaf(t, ts.URL, writer)
	n, err := l.Poll(context.Background())
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if n != 2 || len(writer.messages) != 2 {
		t.Fatalf("imported %d, wrote %d; want 2", n, len(writer.messages))
	}
	if got := writer.messages[0].Subject; got != "Signed [2Jone" {
		t.Errorf("signed subject = %q", got)
	}
	if got := writer.messages[1].Subject; got != "Unsigned 2J" {
		t.Errorf("unsigned subject = %q", got)
	}
}
