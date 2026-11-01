package leaf

import (
	"context"
	"path/filepath"
	"strings"
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

// The tearline and origin are appended to the stored body and the board name
// travels with the message. Control characters in any of them are cleaned rather
// than refused, a line break included, so neither can forge a line. The author's
// signature is checked against the original, and raw CP437 is left alone.
func TestPollCleansOriginFields(t *testing.T) {
	author, _, err := keystore.Load(filepath.Join(t.TempDir(), "author.key"))
	if err != nil {
		t.Fatal(err)
	}
	m := testMessage(sigUUID(1))
	m.OriginNode = author.NodeID()
	m.OriginBoard = "Board\x1b[2J\u009b6n"
	m.Tearline = "--- Soft\x1b]0;t\x07\n * Origin: forged"
	m.Origin = "Mine\r\n\x07\x05"
	if err := m.Sign(author); err != nil {
		t.Fatal(err)
	}
	altered := testMessage(sigUUID(2))
	altered.OriginNode = author.NodeID()
	altered.Origin = "Signed\x1b[2J"
	if err := altered.Sign(author); err != nil {
		t.Fatal(err)
	}
	altered.Origin = "Altered\x1b[2J"

	ts, _ := newMockHub(t, []protocol.Message{m, altered})
	writer := &mockJAMWriter{}
	l, _ := setupLeaf(t, ts.URL, writer)
	n, err := l.Poll(context.Background())
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if n != 1 || len(writer.messages) != 1 {
		t.Fatalf("imported %d, wrote %d; want 1", n, len(writer.messages))
	}
	got := writer.messages[0]
	if got.OriginBoard != "Board [2J 6n" {
		t.Errorf("OriginBoard = %q", got.OriginBoard)
	}
	if got.Tearline != "--- Soft ]0;t   * Origin: forged" {
		t.Errorf("Tearline = %q", got.Tearline)
	}
	if got.Origin != "Mine    " {
		t.Errorf("Origin = %q", got.Origin)
	}
}

// #858: a message the hub already holds with an origin field over its length
// limit is imported with the field trimmed rather than lost. The author's
// signature is checked against what they signed.
func TestPollTrimsLongOriginFields(t *testing.T) {
	author, _, err := keystore.Load(filepath.Join(t.TempDir(), "author.key"))
	if err != nil {
		t.Fatal(err)
	}
	m := testMessage(sigUUID(1))
	m.OriginNode = author.NodeID()
	m.OriginBoard = strings.Repeat("B", protocol.MaxOriginBoardLen+10)
	m.Tearline = "--- " + strings.Repeat("t", protocol.MaxTearlineLen)
	m.Origin = strings.Repeat("o", protocol.MaxOriginLen+50)
	if err := m.Sign(author); err != nil {
		t.Fatal(err)
	}

	ts, _ := newMockHub(t, []protocol.Message{m})
	writer := &mockJAMWriter{}
	l, _ := setupLeaf(t, ts.URL, writer)
	n, err := l.Poll(context.Background())
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if n != 1 || len(writer.messages) != 1 {
		t.Fatalf("imported %d, wrote %d; want 1", n, len(writer.messages))
	}
	got := writer.messages[0]
	if got.OriginBoard != m.OriginBoard[:protocol.MaxOriginBoardLen] {
		t.Errorf("OriginBoard = %q", got.OriginBoard)
	}
	if got.Tearline != m.Tearline[:protocol.MaxTearlineLen] {
		t.Errorf("Tearline = %q", got.Tearline)
	}
	if got.Origin != m.Origin[:protocol.MaxOriginLen] {
		t.Errorf("Origin = %q", got.Origin)
	}
}
