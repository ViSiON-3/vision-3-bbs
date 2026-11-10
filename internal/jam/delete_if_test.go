package jam

import (
	"errors"
	"runtime"
	"testing"
)

// writeSubjects writes one message per subject to b.
func writeSubjects(t *testing.T, b *Base, subjects ...string) {
	t.Helper()
	for _, s := range subjects {
		msg := NewMessage()
		msg.From, msg.To, msg.Subject, msg.Text = "Sysop", "Caller", s, s+" body"
		if _, err := b.WriteMessage(msg); err != nil {
			t.Fatalf("WriteMessage %s: %v", s, err)
		}
	}
}

func subjectIs(s string) func(*Message) bool {
	return func(m *Message) bool { return m.Subject == s }
}

// DeleteMessageIf deletes only what its check accepts, and refuses a
// message number that's gone or already deleted.
func TestDeleteMessageIf(t *testing.T) {
	b := openTestBase(t)
	writeSubjects(t, b, "one", "two", "three")

	if err := b.DeleteMessageIf(1, subjectIs("two")); !errors.Is(err, ErrMessageChanged) {
		t.Errorf("check refused: err = %v, want ErrMessageChanged", err)
	}
	if err := b.DeleteMessageIf(2, subjectIs("two")); err != nil {
		t.Fatalf("check accepted: %v", err)
	}
	if err := b.DeleteMessageIf(2, func(*Message) bool { return true }); !errors.Is(err, ErrMessageChanged) {
		t.Errorf("already deleted: err = %v, want ErrMessageChanged", err)
	}
	if err := b.DeleteMessageIf(9, func(*Message) bool { return true }); !errors.Is(err, ErrMessageChanged) {
		t.Errorf("past the end: err = %v, want ErrMessageChanged", err)
	}
	for n, want := range map[int]bool{1: false, 2: true, 3: false} {
		m, err := b.ReadMessage(n)
		if err != nil {
			t.Fatal(err)
		}
		if m.IsDeleted() != want {
			t.Errorf("message %d deleted = %v, want %v", n, m.IsDeleted(), want)
		}
	}
}

// DeleteMessageIf reads the base through files opened under the lock, not
// through b's, which still show the base as it was before another session
// deleted message 1 and packed. Through b's files message 2 is still "two";
// in the base it has moved to 1 and there is no message 2, so deleting 2
// must be refused rather than "succeed" on files nothing reads any more.
func TestDeleteMessageIfAfterPackElsewhere(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows won't pack a base another handle holds open")
	}
	b := openTestBase(t)
	writeSubjects(t, b, "one", "two")

	other, err := Open(b.BasePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := other.DeleteMessage(1); err != nil {
		t.Fatal(err)
	}
	if _, err := other.Pack(); err != nil {
		t.Fatal(err)
	}
	_ = other.Close()

	if err := b.DeleteMessageIf(2, subjectIs("two")); !errors.Is(err, ErrMessageChanged) {
		t.Fatalf("err = %v, want ErrMessageChanged", err)
	}
	check, err := Open(b.BasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = check.Close() }()
	m, err := check.ReadMessage(1)
	if err != nil || m.Subject != "two" || m.IsDeleted() {
		t.Errorf("message 1 = %+v, %v; want two, not deleted", m, err)
	}
}
