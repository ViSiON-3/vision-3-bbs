package jam

import (
	"encoding/binary"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
)

// replaceWith is an edit that sets subject and text.
func replaceWith(subject, text string) func(*Message) (string, string, bool) {
	return func(*Message) (string, string, bool) { return subject, text, true }
}

// attributeAt reads the Attribute of the header stored at offset in b's .jhr.
func attributeAt(t *testing.T, b *Base, offset uint32) uint32 {
	t.Helper()
	data, err := os.ReadFile(b.BasePath + ".jhr")
	if err != nil {
		t.Fatal(err)
	}
	// Signature, Revision, ReservedWord, then eleven 4-byte fields come
	// before Attribute.
	return binary.LittleEndian.Uint32(data[offset+52:])
}

// hdrOffset is where message msgNum's header is, by the index.
func hdrOffset(t *testing.T, b *Base, msgNum int) uint32 {
	t.Helper()
	b.mu.RLock()
	defer b.mu.RUnlock()
	idx, err := b.readIndexRecordLocked(msgNum)
	if err != nil {
		t.Fatal(err)
	}
	return idx.HdrOffset
}

// The subject and text change, longer than before; the message keeps its
// number, sender, links and kludges, and the others are untouched.
func TestReplaceMessageIf(t *testing.T) {
	b := openTestBase(t)
	writeSubjects(t, b, "one")
	msg := NewMessage()
	msg.From, msg.To, msg.Subject, msg.Text = "Caller", "All", "two", "two body"
	msg.MsgID, msg.ReplyTo, msg.SenderHandle = "1:2/3 abcd1234", 1, "caller"
	msg.Kludges = []string{"CHRS: CP437 2"}
	if _, err := b.WriteMessage(msg); err != nil {
		t.Fatal(err)
	}
	writeSubjects(t, b, "three")
	active := b.fixedHeader.ActiveMsgs

	var seen *Message
	err := b.ReplaceMessageIf(2, func(m *Message) (string, string, bool) {
		seen = m
		return "a much longer subject than two", "new body\nsecond line", true
	})
	if err != nil {
		t.Fatal(err)
	}
	if seen == nil || seen.Subject != "two" || seen.Text != "two body" {
		t.Fatalf("edit saw %+v, want message 2 as stored", seen)
	}

	m, err := b.ReadMessage(2)
	if err != nil {
		t.Fatal(err)
	}
	if m.Subject != "a much longer subject than two" || m.Text != "new body\rsecond line" {
		t.Errorf("subject %q text %q", m.Subject, m.Text)
	}
	if m.From != "Caller" || m.To != "All" || m.MsgID != msg.MsgID || m.Header.ReplyTo != 1 ||
		m.SenderHandle != "caller" || !slices.Equal(m.Kludges, msg.Kludges) || m.IsDeleted() {
		t.Errorf("other fields changed: %+v", m)
	}
	for n, subj := range map[int]string{1: "one", 3: "three"} {
		if o, err := b.ReadMessage(n); err != nil || o.Subject != subj || o.Text != subj+" body" {
			t.Errorf("message %d = %+v, %v", n, o, err)
		}
	}
	if n, _ := b.GetMessageCount(); n != 3 {
		t.Errorf("count = %d, want 3", n)
	}
	if _, err := b.RefreshFixedHeader(); err != nil {
		t.Fatal(err)
	}
	if b.fixedHeader.ActiveMsgs != active {
		t.Errorf("active messages = %d, want %d", b.fixedHeader.ActiveMsgs, active)
	}
}

// The old header is marked deleted, so a tool walking the .jhr skips it,
// and a pack reclaims it and the old text.
func TestReplaceMessageIfRetiresOldHeader(t *testing.T) {
	b := openTestBase(t)
	writeSubjects(t, b, "one", "two")
	old := hdrOffset(t, b, 1)

	if err := b.ReplaceMessageIf(1, replaceWith("one", "short")); err != nil {
		t.Fatal(err)
	}
	if now := hdrOffset(t, b, 1); now == old {
		t.Fatal("the index still points at the old header")
	}
	if attributeAt(t, b, old)&MsgDeleted == 0 {
		t.Error("the old header isn't marked deleted")
	}

	before, _ := os.Stat(b.BasePath + ".jdt")
	if _, err := b.Pack(); err != nil {
		t.Fatal(err)
	}
	after, _ := os.Stat(b.BasePath + ".jdt")
	if want := int64(len("short") + len("two body")); after.Size() != want || before.Size() <= want {
		t.Errorf(".jdt is %d bytes after the pack (%d before), want %d", after.Size(), before.Size(), want)
	}
	for n, text := range map[int]string{1: "short", 2: "two body"} {
		if m, err := b.ReadMessage(n); err != nil || m.Text != text {
			t.Errorf("after pack, message %d = %+v, %v", n, m, err)
		}
	}
}

// failReplaceAt makes replaceMessageLocked's step fail for the rest of the
// test.
func failReplaceAt(t *testing.T, steps ...string) {
	t.Helper()
	replaceFault = func(s string) error {
		if slices.Contains(steps, s) {
			return errors.New("injected " + s + " failure")
		}
		return nil
	}
	t.Cleanup(func() { replaceFault = nil })
}

// A failure writing or syncing the index leaves the message as it was: the
// error and the base agree, and the old header stays live.
func TestReplaceMessageIfIndexFailure(t *testing.T) {
	for _, step := range []string{"index", "indexsync"} {
		t.Run(step, func(t *testing.T) {
			b := openTestBase(t)
			writeSubjects(t, b, "one", "two")
			old := hdrOffset(t, b, 1)
			failReplaceAt(t, step)

			err := b.ReplaceMessageIf(1, replaceWith("edited", "new body"))
			if err == nil {
				t.Fatal("the edit reported success")
			}
			if errors.Is(err, ErrEditUncertain) {
				t.Errorf("err = %v, want it certain: the index was put back", err)
			}
			if now := hdrOffset(t, b, 1); now != old {
				t.Errorf("the index points at %d, want the old header at %d", now, old)
			}
			if attributeAt(t, b, old)&MsgDeleted != 0 {
				t.Error("the old header was retired")
			}
			if m, err := b.ReadMessage(1); err != nil || m.Subject != "one" || m.Text != "one body" {
				t.Errorf("message 1 = %+v, %v; want it unchanged", m, err)
			}
		})
	}
}

// When putting the index back fails too, the outcome isn't known, and the
// error says so.
func TestReplaceMessageIfRestoreFailure(t *testing.T) {
	for _, steps := range [][]string{{"index", "restore"}, {"indexsync", "restore"}, {"indexsync", "restoresync"}} {
		t.Run(strings.Join(steps, "+"), func(t *testing.T) {
			b := openTestBase(t)
			writeSubjects(t, b, "one", "two")
			failReplaceAt(t, steps...)

			if err := b.ReplaceMessageIf(1, replaceWith("edited", "new body")); !errors.Is(err, ErrEditUncertain) {
				t.Errorf("err = %v, want ErrEditUncertain", err)
			}
		})
	}
}

// Once the index is synced the edit has been made, so a failure retiring
// the old header after it isn't reported: the message reads as edited.
func TestReplaceMessageIfRetireFailure(t *testing.T) {
	b := openTestBase(t)
	writeSubjects(t, b, "one", "two")
	failReplaceAt(t, "retire")

	if err := b.ReplaceMessageIf(1, replaceWith("edited", "new body")); err != nil {
		t.Fatalf("err = %v, want the edit reported as made", err)
	}
	if m, err := b.ReadMessage(1); err != nil || m.Subject != "edited" || m.Text != "new body" {
		t.Errorf("message 1 = %+v, %v; want the edit", m, err)
	}
}

// A refused edit, a deleted message and a number past the end change
// nothing and give ErrMessageChanged.
func TestReplaceMessageIfRefusals(t *testing.T) {
	b := openTestBase(t)
	writeSubjects(t, b, "one", "two")
	if err := b.DeleteMessage(2); err != nil {
		t.Fatal(err)
	}
	jdt, _ := os.Stat(b.BasePath + ".jdt")

	refuse := func(*Message) (string, string, bool) { return "x", "x", false }
	for name, err := range map[string]error{
		"refused":  b.ReplaceMessageIf(1, refuse),
		"deleted":  b.ReplaceMessageIf(2, replaceWith("x", "x")),
		"past end": b.ReplaceMessageIf(9, replaceWith("x", "x")),
	} {
		if !errors.Is(err, ErrMessageChanged) {
			t.Errorf("%s: err = %v, want ErrMessageChanged", name, err)
		}
	}
	if m, _ := b.ReadMessage(1); m.Subject != "one" || m.Text != "one body" {
		t.Errorf("message 1 = %+v, want it unchanged", m)
	}
	if now, _ := os.Stat(b.BasePath + ".jdt"); now.Size() != jdt.Size() {
		t.Errorf(".jdt grew from %d to %d bytes", jdt.Size(), now.Size())
	}
}

// withSubject replaces the subject where it is, adds one when there's none,
// and drops it for an empty subject.
func TestWithSubject(t *testing.T) {
	from := CreateSubfield(SfldSenderName, "Caller")
	subj := CreateSubfield(SfldSubject, "old")
	to := CreateSubfield(SfldReceiverName, "All")
	ids := func(sfs []Subfield) (out []string) {
		for _, sf := range sfs {
			out = append(out, string(sf.Buffer))
		}
		return out
	}
	for _, tc := range []struct {
		in      []Subfield
		subject string
		want    []string
	}{
		{[]Subfield{from, subj, to}, "new", []string{"Caller", "new", "All"}},
		{[]Subfield{from, to}, "new", []string{"Caller", "All", "new"}},
		{[]Subfield{from, subj, to}, "", []string{"Caller", "All"}},
	} {
		if got := ids(withSubject(tc.in, tc.subject)); !slices.Equal(got, tc.want) {
			t.Errorf("withSubject(%q) = %v, want %v", tc.subject, got, tc.want)
		}
	}
}

// An index entry another tool blanked (#1049) is a changed message for an
// edit and a delete alike, not a generic error.
func TestBlankedIndexEntryIsAConflict(t *testing.T) {
	b := openTestBase(t)
	writeSubjects(t, b, "one", "two")
	if err := b.writeIndexRecord(2, &IndexRecord{ToCRC: 0xFFFFFFFF, HdrOffset: 0xFFFFFFFF}); err != nil {
		t.Fatal(err)
	}
	if err := b.ReplaceMessageIf(2, replaceWith("x", "x")); !errors.Is(err, ErrMessageChanged) {
		t.Errorf("edit: err = %v, want ErrMessageChanged", err)
	}
	if err := b.DeleteMessageIf(2, func(*Message) bool { return true }); !errors.Is(err, ErrMessageChanged) {
		t.Errorf("delete: err = %v, want ErrMessageChanged", err)
	}
}
