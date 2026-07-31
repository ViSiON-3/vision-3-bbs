package testterm

import (
	"errors"
	"io"
	"testing"
	"time"
)

func TestSessionReplaysKeys(t *testing.T) {
	sess := NewSession(nil, "hi")

	buf := make([]byte, 8)
	n, err := sess.Read(buf)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got := string(buf[:n]); got != "hi" {
		t.Errorf("Read = %q, want %q", got, "hi")
	}
}

// With no interrupt set, exhausted input reads as EOF.
func TestSessionReturnsEOFWhenExhausted(t *testing.T) {
	sess := NewSession(nil, "a")
	buf := make([]byte, 8)
	sess.Read(buf)

	if _, err := sess.Read(buf); !errors.Is(err, io.EOF) {
		t.Errorf("second Read err = %v, want io.EOF", err)
	}
}

// With an interrupt channel set, exhausted input blocks like a live connection
// until the interrupt fires. This is what makes the input-handler leak test
// meaningful, so it is preserved exactly.
func TestSessionBlocksUntilInterrupt(t *testing.T) {
	sess := NewSession(nil, "")
	ch := make(chan struct{})
	sess.SetReadInterrupt(ch)

	done := make(chan error, 1)
	go func() {
		buf := make([]byte, 4)
		_, err := sess.Read(buf)
		done <- err
	}()

	select {
	case err := <-done:
		t.Fatalf("Read returned %v before the interrupt fired", err)
	case <-time.After(50 * time.Millisecond):
	}

	close(ch)
	select {
	case err := <-done:
		if err == nil {
			t.Error("Read err = nil after interrupt, want an error")
		}
	case <-time.After(time.Second):
		t.Fatal("Read did not return after the interrupt fired")
	}
}

func TestSessionSendAppendsInput(t *testing.T) {
	sess := NewSession(nil, "a")
	buf := make([]byte, 8)
	sess.Read(buf)

	sess.Send("bc")
	n, err := sess.Read(buf)
	if err != nil {
		t.Fatalf("Read after Send: %v", err)
	}
	if got := string(buf[:n]); got != "bc" {
		t.Errorf("Read = %q, want %q", got, "bc")
	}
}

// Send issued while a Read is already blocked (input exhausted, interrupt
// set, nothing sent yet) must wake it and deliver the new keys. This is
// Send's entire purpose: feeding input in stages to code under test that runs
// concurrently with the test goroutine, the shape an interactive prompt loop
// test takes. Without a wakeup, Read stays parked on the interrupt and never
// sees the new input.
func TestSessionSendUnblocksPendingRead(t *testing.T) {
	sess := NewSession(nil, "")
	ch := make(chan struct{})
	defer close(ch) // in case the test fails before Send unblocks Read
	sess.SetReadInterrupt(ch)

	type result struct {
		n   int
		err error
	}
	buf := make([]byte, 8)
	done := make(chan result, 1)
	go func() {
		n, err := sess.Read(buf)
		done <- result{n, err}
	}()

	// Confirm Read is actually blocked before sending, or this test could
	// pass merely because Send happened to run before Read checked the
	// buffer rather than because Send woke a pending Read.
	select {
	case res := <-done:
		t.Fatalf("Read returned (%d, %v) before any input was sent", res.n, res.err)
	case <-time.After(50 * time.Millisecond):
	}

	sess.Send("hi")

	select {
	case res := <-done:
		if res.err != nil {
			t.Fatalf("Read after Send: %v", res.err)
		}
		if got := string(buf[:res.n]); got != "hi" {
			t.Errorf("Read = %q, want %q", got, "hi")
		}
	case <-time.After(time.Second):
		t.Fatal("Read did not return after Send supplied input to a pending Read")
	}
}

// Pty has a demonstrated caller (RunEditorWithMetadata) that would otherwise
// panic on the embedded nil ssh.Session. Session must answer "no PTY" instead.
func TestSessionPtyReportsNoPty(t *testing.T) {
	sess := NewSession(nil, "")

	_, winCh, isPty := sess.Pty()
	if isPty {
		t.Error("Pty() isPty = true, want false")
	}
	if winCh != nil {
		t.Error("Pty() winCh != nil, want nil")
	}
}

func TestSessionWriteGoesToTerm(t *testing.T) {
	tt := New(20, 3)
	sess := NewSession(tt, "")

	if _, err := sess.Write([]byte("\x1b[2;1HX")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if got := tt.Cell(2, 1).Rune; got != 'X' {
		t.Errorf("Cell(2,1).Rune = %q, want 'X'", got)
	}
}

func TestSessionWriteWithNilTermDiscards(t *testing.T) {
	sess := NewSession(nil, "")
	if _, err := sess.Write([]byte("anything")); err != nil {
		t.Errorf("Write with nil Term: %v", err)
	}
}
