package message

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// newMoveTestManager builds a manager with two local areas, 1 and 2.
func newMoveTestManager(t *testing.T) *MessageManager {
	t.Helper()
	tmp := t.TempDir()
	cfg := filepath.Join(tmp, "config")
	if err := os.MkdirAll(cfg, 0o755); err != nil {
		t.Fatal(err)
	}
	writeMsgAreas(t, cfg, `[
		{"id":1,"tag":"FROM","name":"From","base_path":"from","area_type":"local"},
		{"id":2,"tag":"TO","name":"To","base_path":"to","area_type":"local"}]`)
	mm, err := NewMessageManager(tmp, cfg, "TestBBS", nil)
	if err != nil {
		t.Fatal(err)
	}
	return mm
}

func count(t *testing.T, mm *MessageManager, area int) int {
	t.Helper()
	n, err := mm.GetMessageCountForArea(area)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

var (
	always   = func(*DisplayMessage) bool { return true }
	sameBody = func(m *DisplayMessage) string { return m.Body }
)

// The message arrives in the other area with its sender, recipient,
// subject, date, private flag and sender handle, and the text body makes of
// it; the area it left is packed.
func TestMoveMessageIf(t *testing.T) {
	mm := newMoveTestManager(t)
	posted := time.Date(2026, 11, 1, 9, 30, 0, 0, time.Local)
	keep := post(t, mm, 1, "All", "stays")
	n, err := mm.Post(1, "Carl Caller", "Bob", "moving", "the text", PostOptions{Private: true, Date: posted, SenderHandle: "Caller"})
	if err != nil {
		t.Fatal(err)
	}

	got, err := mm.MoveMessageIf(1, n, 2, always, func(m *DisplayMessage) string { return m.Body + "\nmoved" })
	if err != nil {
		t.Fatal(err)
	}
	if count(t, mm, 1) != 1 || count(t, mm, 2) != 1 {
		t.Fatalf("areas hold %d and %d messages, want 1 and 1", count(t, mm, 1), count(t, mm, 2))
	}
	if m, _ := mm.GetMessage(1, keep); m.Subject != "stays" {
		t.Errorf("message left behind = %q, want stays", m.Subject)
	}
	m, err := mm.GetMessage(2, got)
	if err != nil {
		t.Fatal(err)
	}
	if m.From != "Carl Caller" || m.To != "Bob" || m.Subject != "moving" || !m.DateTime.Equal(posted) ||
		!m.IsPrivate || m.SenderHandle != "Caller" || m.Body != "the text\nmoved" {
		t.Errorf("moved message = %+v", m)
	}
}

// A message check refuses, a missing one, and a move to its own area are
// refused, and nothing changes.
func TestMoveMessageIfRefused(t *testing.T) {
	mm := newMoveTestManager(t)
	n := post(t, mm, 1, "All", "stays")
	never := func(*DisplayMessage) bool { return false }
	if _, err := mm.MoveMessageIf(1, n, 2, never, sameBody); !errors.Is(err, ErrMessageChanged) {
		t.Errorf("refused: err = %v, want ErrMessageChanged", err)
	}
	if _, err := mm.MoveMessageIf(1, n+3, 2, always, sameBody); !errors.Is(err, ErrMessageChanged) {
		t.Errorf("missing: err = %v, want ErrMessageChanged", err)
	}
	if _, err := mm.MoveMessageIf(1, n, 1, always, sameBody); err == nil {
		t.Error("a move to the same area was allowed")
	}
	if count(t, mm, 1) != 1 || count(t, mm, 2) != 0 {
		t.Errorf("areas hold %d and %d messages, want 1 and 0", count(t, mm, 1), count(t, mm, 2))
	}
}

// If the message is deleted elsewhere while it is copied, or check refuses
// it when the original is deleted, the copy is taken back: the message is
// in one area or the other, never both and never neither.
func TestMoveMessageIfChangedMeanwhile(t *testing.T) {
	mm := newMoveTestManager(t)
	n := post(t, mm, 1, "All", "gone")
	_, err := mm.MoveMessageIf(1, n, 2, always, func(m *DisplayMessage) string {
		if err := mm.DeleteMessage(1, n); err != nil {
			t.Fatal(err)
		}
		return m.Body
	})
	if !errors.Is(err, ErrMessageChanged) {
		t.Errorf("deleted meanwhile: err = %v, want ErrMessageChanged", err)
	}
	if count(t, mm, 2) != 0 {
		t.Errorf("the copy wasn't taken back: area 2 holds %d", count(t, mm, 2))
	}

	mm = newMoveTestManager(t)
	n = post(t, mm, 1, "All", "stays")
	calls := 0
	onceOnly := func(*DisplayMessage) bool { calls++; return calls == 1 }
	if _, err := mm.MoveMessageIf(1, n, 2, onceOnly, sameBody); !errors.Is(err, ErrMessageChanged) {
		t.Errorf("refused at delete: err = %v, want ErrMessageChanged", err)
	}
	if count(t, mm, 1) != 1 || count(t, mm, 2) != 0 {
		t.Errorf("areas hold %d and %d messages, want 1 and 0", count(t, mm, 1), count(t, mm, 2))
	}
	if m, _ := mm.GetMessage(1, n); m.IsDeleted || m.Subject != "stays" {
		t.Errorf("original = %+v, want it untouched", m)
	}
}

// If another session packs the area while the message is copied, and a
// different message takes its number, that message isn't deleted in its
// place: the move is refused and the copy taken back.
func TestMoveMessageIfNumberReused(t *testing.T) {
	mm := newMoveTestManager(t)
	first := post(t, mm, 1, "All", "first")
	post(t, mm, 1, "All", "second")
	_, err := mm.MoveMessageIf(1, first, 2, always, func(m *DisplayMessage) string {
		if err := mm.DeleteMessage(1, first); err != nil {
			t.Fatal(err)
		}
		if err := mm.PackAndLinkArea(1); err != nil {
			t.Fatal(err)
		}
		return m.Body
	})
	if !errors.Is(err, ErrMessageChanged) {
		t.Errorf("err = %v, want ErrMessageChanged", err)
	}
	if m, err := mm.GetMessage(1, first); err != nil || m.Subject != "second" || m.IsDeleted {
		t.Errorf("message %d = %+v, %v; want \"second\" untouched", first, m, err)
	}
	if count(t, mm, 2) != 0 {
		t.Errorf("the copy wasn't taken back: area 2 holds %d", count(t, mm, 2))
	}
}

// If another session deletes an earlier message in the destination and
// packs it before the original's delete is refused, the copy has moved to a
// lower number, with a message much like it posted after it; it is still
// found and taken back, and the destination's other messages are left alone.
func TestMoveMessageIfCopyRenumbered(t *testing.T) {
	mm := newMoveTestManager(t)
	n := post(t, mm, 1, "All", "stays")
	orig, err := mm.GetMessage(1, n)
	if err != nil {
		t.Fatal(err)
	}
	early := post(t, mm, 2, "All", "early")
	post(t, mm, 2, "All", "later")
	calls := 0
	packFirst := func(*DisplayMessage) bool {
		calls++
		if calls == 1 {
			return true
		}
		if err := mm.DeleteMessage(2, early); err != nil {
			t.Fatal(err)
		}
		if err := mm.PackAndLinkArea(2); err != nil {
			t.Fatal(err)
		}
		// The same sender, recipient, subject and date as the copy, but
		// other text: not the copy.
		if _, err := mm.Post(2, "alice", "All", "stays", "other text", PostOptions{Date: orig.DateTime}); err != nil {
			t.Fatal(err)
		}
		return false
	}
	if _, err := mm.MoveMessageIf(1, n, 2, packFirst, sameBody); !errors.Is(err, ErrMessageChanged) {
		t.Errorf("err = %v, want ErrMessageChanged", err)
	}
	if count(t, mm, 1) != 1 {
		t.Errorf("area 1 holds %d, want the original kept", count(t, mm, 1))
	}
	if count(t, mm, 2) != 2 {
		t.Fatalf("area 2 holds %d, want later and the other \"stays\" (the copy taken back)", count(t, mm, 2))
	}
	for i, want := range []string{"body", "other text"} {
		if m, _ := mm.GetMessage(2, i+1); m.Body != want || m.IsDeleted {
			t.Errorf("area 2 message %d = %+v, want the one with text %q", i+1, m, want)
		}
	}
}
