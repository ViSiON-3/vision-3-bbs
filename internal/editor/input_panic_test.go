package editor

import (
	"errors"
	"io"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/safego"
	"github.com/ViSiON-3/vision-3-bbs/internal/safego/safegotest"
)

// A panic on a session's input reader ends that session's input (its reads
// get end of file, so the session ends) and is logged; the next session's
// reader works.
func TestInputReaderPanicEndsOnlyItsSession(t *testing.T) {
	logs := safegotest.Capture(t)
	safegotest.Inject(t, safego.KindInput)

	pr1, pw1 := io.Pipe()
	defer pw1.Close()
	first := NewInputHandler(pr1)
	defer first.Close()
	logs.WaitPanic(t, safego.KindInput, "editor/input.go")
	if _, err := first.ReadKey(); !errors.Is(err, io.EOF) {
		t.Errorf("first session's read = %v, want io.EOF", err)
	}

	pr2, pw2 := io.Pipe()
	defer pw2.Close()
	second := NewInputHandler(pr2)
	defer second.Close()
	go func() { _, _ = pw2.Write([]byte("x")) }()
	if k, err := second.ReadKey(); k != 'x' || err != nil {
		t.Errorf("second session's read = %q, %v; want x", rune(k), err)
	}
}

// The same holds for a reader that filters terminal replies.
func TestTrappedInputReaderPanicEndsOnlyItsSession(t *testing.T) {
	logs := safegotest.Capture(t)
	safegotest.Inject(t, safego.KindInput)

	pr1, pw1 := io.Pipe()
	defer pw1.Close()
	first := NewTrappedInputHandler(pr1, &ReplyTrap{})
	defer first.Close()
	logs.WaitPanic(t, safego.KindInput, "editor/input")
	if _, err := first.ReadKey(); !errors.Is(err, io.EOF) {
		t.Errorf("first session's read = %v, want io.EOF", err)
	}

	pr2, pw2 := io.Pipe()
	defer pw2.Close()
	second := NewTrappedInputHandler(pr2, &ReplyTrap{})
	defer second.Close()
	go func() { _, _ = pw2.Write([]byte("x")) }()
	if k, err := second.ReadKey(); k != 'x' || err != nil {
		t.Errorf("second session's read = %q, %v; want x", rune(k), err)
	}
}
