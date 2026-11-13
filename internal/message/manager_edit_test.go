package message

import (
	"errors"
	"fmt"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/jam"
)

// rawText is message msgNum's text as stored, kludge lines and all.
func rawText(t *testing.T, mm *MessageManager, areaID, msgNum int) string {
	t.Helper()
	b, _, err := mm.openBase(areaID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = b.Close() }()
	m, err := b.ReadMessage(msgNum)
	if err != nil {
		t.Fatal(err)
	}
	return m.Text
}

// The edit sees the message as stored and sets its subject and body; kludge
// lines in the stored text stay ahead of the new body, and the message keeps
// its sender, recipient and date.
func TestEditMessageIf(t *testing.T) {
	mm := newIndexTestManager(t)
	n, err := mm.AddMessage(3, "alice", "All", "old subject", "\x01TEST 1\nold body", "")
	if err != nil {
		t.Fatal(err)
	}
	before, _ := mm.GetMessage(3, n)

	err = mm.EditMessageIf(3, n, func(m *DisplayMessage) (string, string, bool) {
		if m.Subject != "old subject" || m.Body != "old body" {
			t.Errorf("edit saw subject %q body %q", m.Subject, m.Body)
		}
		return "new subject", "new body\r\nline two", true
	})
	if err != nil {
		t.Fatal(err)
	}
	after, err := mm.GetMessage(3, n)
	if err != nil {
		t.Fatal(err)
	}
	if after.Subject != "new subject" || after.Body != "new body\nline two" {
		t.Errorf("subject %q body %q", after.Subject, after.Body)
	}
	if after.From != before.From || after.To != before.To || !after.DateTime.Equal(before.DateTime) {
		t.Errorf("header changed: %+v, was %+v", after, before)
	}
	if got := rawText(t, mm, 3, n); got != "\x01TEST 1\rnew body\rline two" {
		t.Errorf("stored text = %q, want the kludge line kept ahead of the new body", got)
	}
}

// A refused edit or a missing message gives ErrMessageChanged.
func TestEditMessageIfRefused(t *testing.T) {
	mm := newIndexTestManager(t)
	n := post(t, mm, 3, "All", "kept")
	refuse := func(*DisplayMessage) (string, string, bool) { return "x", "x", false }
	if err := mm.EditMessageIf(3, n, refuse); !errors.Is(err, ErrMessageChanged) {
		t.Errorf("refused: err = %v, want ErrMessageChanged", err)
	}
	if err := mm.EditMessageIf(3, n+5, func(*DisplayMessage) (string, string, bool) { return "x", "x", true }); !errors.Is(err, ErrMessageChanged) {
		t.Errorf("missing: err = %v, want ErrMessageChanged", err)
	}
	if m, _ := mm.GetMessage(3, n); m.Subject != "kept" || m.Body != "body" {
		t.Errorf("message = %q / %q, want it unchanged", m.Subject, m.Body)
	}
}

// An edit that may or may not have been made drops the area's cached
// threading, which may describe either version, and says the outcome is
// uncertain.
func TestEditMessageIfUncertain(t *testing.T) {
	mm := newIndexTestManager(t)
	n, err := mm.AddMessage(3, "alice", "All", "subject", "body", "")
	if err != nil {
		t.Fatal(err)
	}
	replaceMessage = func(*jam.Base, int, func(*jam.Message) (string, string, bool)) error {
		return fmt.Errorf("%w: injected", jam.ErrEditUncertain)
	}
	t.Cleanup(func() { replaceMessage = (*jam.Base).ReplaceMessageIf })
	mm.threadIndex[3] = &threadIndex{}

	err = mm.EditMessageIf(3, n, func(m *DisplayMessage) (string, string, bool) { return "new", "new", true })
	if !errors.Is(err, ErrEditUncertain) {
		t.Errorf("err = %v, want ErrEditUncertain", err)
	}
	if _, ok := mm.threadIndex[3]; ok {
		t.Error("the area's thread index is still cached")
	}
}
