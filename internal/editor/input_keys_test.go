package editor

import (
	"bytes"
	"errors"
	"io"
	"testing"
	"time"
)

// A reader that has run dry reports EOF, which ends an escape sequence at once
// instead of waiting out the inter-byte timeout, so these decode instantly.
func TestReadKeyDecodesCSISequences(t *testing.T) {
	for _, tc := range []struct {
		seq  string
		want int
	}{
		{"\x1b[A", KeyArrowUp},
		{"\x1b[B", KeyArrowDown},
		{"\x1b[C", KeyArrowRight},
		{"\x1b[D", KeyArrowLeft},
		{"\x1b[H", KeyHome},
		{"\x1b[F", KeyEnd},
		{"\x1b[U", KeyPageDown}, // SyncTERM
		{"\x1b[V", KeyPageUp},   // SyncTERM
		{"\x1b[1~", KeyHome},
		{"\x1b[2~", KeyInsert},
		{"\x1b[3~", KeyDeleteKey},
		{"\x1b[4~", KeyEnd},
		{"\x1b[5~", KeyPageUp},
		{"\x1b[6~", KeyPageDown},
		{"\x1b[1;5C", KeyArrowRight}, // modifier parameters are skipped over
		// CSI K is "Erase in Line" echoed back by some clients; it must not
		// read as End.
		{"\x1b[K", KeyEsc},
		{"\x1b[9~", KeyEsc}, // unknown tilde code
		{"\x1b[~", KeyEsc},  // tilde with no code
		{"\x1b[", KeyEsc},   // CSI cut short
	} {
		ih := NewInputHandler(bytes.NewReader([]byte(tc.seq)))
		got, err := ih.ReadKey()
		if err != nil || got != tc.want {
			t.Errorf("ReadKey(%q) = %#x, %v; want %#x, nil", tc.seq, got, err, tc.want)
		}
	}
}

func TestReadKeyDecodesSS3Sequences(t *testing.T) {
	for _, tc := range []struct {
		seq  string
		want int
	}{
		{"\x1bOA", KeyArrowUp},
		{"\x1bOB", KeyArrowDown},
		{"\x1bOC", KeyArrowRight},
		{"\x1bOD", KeyArrowLeft},
		{"\x1bOH", KeyHome},
		{"\x1bOF", KeyEnd},
		{"\x1bOZ", KeyEsc},
	} {
		ih := NewInputHandler(bytes.NewReader([]byte(tc.seq)))
		got, err := ih.ReadKey()
		if err != nil || got != tc.want {
			t.Errorf("ReadKey(%q) = %#x, %v; want %#x, nil", tc.seq, got, err, tc.want)
		}
	}

	// The connection dropping mid-sequence surfaces as the read error.
	ih := NewInputHandler(bytes.NewReader([]byte("\x1bO")))
	if got, err := ih.ReadKey(); got != KeyEsc || err != io.EOF {
		t.Errorf("ReadKey(ESC O, EOF) = %#x, %v; want KeyEsc, io.EOF", got, err)
	}
}

// ESC followed by an ordinary byte is a bare Escape, and the byte is the next key.
func TestReadKeyEscThenOrdinaryByte(t *testing.T) {
	ih := NewInputHandler(bytes.NewReader([]byte("\x1bq")))
	if k, err := ih.ReadKey(); err != nil || k != KeyEsc {
		t.Fatalf("first key = %#x, %v; want KeyEsc", k, err)
	}
	if k, err := ih.ReadKey(); err != nil || k != 'q' {
		t.Fatalf("second key = %#x, %v; want 'q'", k, err)
	}
	if _, err := ih.ReadKey(); err != io.EOF {
		t.Fatalf("third read: err = %v, want io.EOF", err)
	}
}

func TestTranslateToWordStar(t *testing.T) {
	for key, want := range map[int]int{
		KeyArrowUp:    KeyCtrlE,
		KeyArrowDown:  KeyCtrlX,
		KeyArrowLeft:  KeyCtrlS,
		KeyArrowRight: KeyCtrlD,
		KeyHome:       KeyCtrlW,
		KeyEnd:        KeyCtrlP,
		KeyPageUp:     KeyCtrlR,
		KeyPageDown:   KeyCtrlC,
		KeyInsert:     KeyCtrlV,
		KeyDeleteKey:  KeyCtrlG,
		'a':           'a',
		KeyEnter:      KeyEnter,
		KeyCtrlZ:      KeyCtrlZ,
	} {
		if got := TranslateToWordStar(key); got != want {
			t.Errorf("TranslateToWordStar(%#x) = %#x, want %#x", key, got, want)
		}
	}
}

func TestReadKeyTranslatedFoldsArrowsAndReportsEOF(t *testing.T) {
	ih := NewInputHandler(bytes.NewReader([]byte("\x1b[5~x")))
	if k, err := ih.ReadKeyTranslated(); err != nil || k != KeyCtrlR {
		t.Errorf("PageUp = %#x, %v; want KeyCtrlR", k, err)
	}
	if k, err := ih.ReadKeyTranslated(); err != nil || k != 'x' {
		t.Errorf("second key = %#x, %v; want 'x'", k, err)
	}
	if _, err := ih.ReadKeyTranslated(); err != io.EOF {
		t.Errorf("at end of input: err = %v, want io.EOF", err)
	}
}

// silentInput returns an InputHandler on a connection that stays open and
// sends nothing, plus a function that hangs it up.
func silentInput(t *testing.T) (*InputHandler, func()) {
	t.Helper()
	pr, pw := io.Pipe()
	t.Cleanup(func() { _ = pw.Close() })
	return NewInputHandler(pr), func() { _ = pw.Close() }
}

func TestReadKeyWithTimeout(t *testing.T) {
	// Input already waiting is decoded as ReadKey would, escape sequences included.
	ih := NewInputHandler(bytes.NewReader([]byte("\x1b[Bz")))
	if k, err := ih.ReadKeyWithTimeout(time.Minute); err != nil || k != KeyArrowDown {
		t.Errorf("first key = %#x, %v; want KeyArrowDown", k, err)
	}
	if k, err := ih.ReadKeyWithTimeout(time.Minute); err != nil || k != 'z' {
		t.Errorf("second key = %#x, %v; want 'z'", k, err)
	}
	// A closed connection is EOF, not an idle timeout.
	if _, err := ih.ReadKeyWithTimeout(time.Minute); err != io.EOF {
		t.Errorf("at end of input: err = %v, want io.EOF", err)
	}

	quiet, _ := silentInput(t)
	if k, err := quiet.ReadKeyWithTimeout(5 * time.Millisecond); k != 0 || !errors.Is(err, ErrIdleTimeout) {
		t.Errorf("silent connection = %#x, %v; want 0, ErrIdleTimeout", k, err)
	}
}

// The session idle timeout applies to every key wait once set, and stops
// applying when cleared.
func TestSessionIdleTimeout(t *testing.T) {
	quiet, hangUp := silentInput(t)
	quiet.SetSessionIdleTimeout(5 * time.Millisecond)
	if k, err := quiet.ReadKey(); k != 0 || !errors.Is(err, ErrIdleTimeout) {
		t.Errorf("idle ReadKey = %#x, %v; want 0, ErrIdleTimeout", k, err)
	}

	// With the timeout cleared the read waits for the connection instead, so
	// it sees the hang-up rather than an idle timeout.
	quiet.SetSessionIdleTimeout(0)
	hangUp()
	if _, err := quiet.ReadKey(); err != io.EOF {
		t.Errorf("after hang-up: err = %v, want io.EOF", err)
	}

	// A key that arrives inside the window is returned normally, and a dropped
	// connection is still EOF rather than an idle timeout.
	ih := NewInputHandler(bytes.NewReader([]byte("k")))
	ih.SetSessionIdleTimeout(time.Minute)
	if k, err := ih.ReadKey(); err != nil || k != 'k' {
		t.Errorf("ReadKey = %#x, %v; want 'k'", k, err)
	}
	if _, err := ih.ReadKey(); err != io.EOF {
		t.Errorf("at end of input: err = %v, want io.EOF", err)
	}
}

// The session deadline ends a key wait with ErrTimeLimit, which is also an
// ErrIdleTimeout so existing idle handling logs the caller off. Whichever of
// the idle timeout and the deadline comes first decides the error.
func TestSessionDeadline(t *testing.T) {
	quiet, _ := silentInput(t)
	quiet.SetSessionIdleTimeout(time.Minute)
	quiet.SetSessionDeadline(time.Now().Add(5 * time.Millisecond))
	if _, err := quiet.ReadKey(); !errors.Is(err, ErrTimeLimit) || !errors.Is(err, ErrIdleTimeout) {
		t.Errorf("deadline before idle timeout: err = %v, want ErrTimeLimit wrapping ErrIdleTimeout", err)
	}

	quiet.SetSessionIdleTimeout(5 * time.Millisecond)
	quiet.SetSessionDeadline(time.Now().Add(time.Minute))
	if _, err := quiet.ReadKey(); !errors.Is(err, ErrIdleTimeout) || errors.Is(err, ErrTimeLimit) {
		t.Errorf("idle timeout before deadline: err = %v, want plain ErrIdleTimeout", err)
	}

	// With no idle timeout at all the deadline still bounds the wait.
	quiet.SetSessionIdleTimeout(0)
	quiet.SetSessionDeadline(time.Now().Add(5 * time.Millisecond))
	if _, err := quiet.ReadKey(); !errors.Is(err, ErrTimeLimit) {
		t.Errorf("deadline alone: err = %v, want ErrTimeLimit", err)
	}

	// The event-aware read honours it too.
	quiet.SetSessionDeadline(time.Now().Add(5 * time.Millisecond))
	if _, _, _, err := readKeyOrEvent[struct{}](quiet, nil); !errors.Is(err, ErrTimeLimit) {
		t.Errorf("readKeyOrEvent: err = %v, want ErrTimeLimit", err)
	}

	// Once passed, a caller who keeps typing is stopped too: input waiting in
	// the queue is not read.
	busy := NewInputHandler(bytes.NewReader([]byte("abc")))
	busy.SetSessionDeadline(time.Now().Add(-time.Second))
	if k, err := busy.ReadKey(); !errors.Is(err, ErrTimeLimit) {
		t.Errorf("past deadline with input queued = %#x, %v; want ErrTimeLimit", k, err)
	}

	// Suspending it lets reads through; restoring it brings it back.
	restore := busy.SuspendSessionDeadline()
	if k, err := busy.ReadKey(); err != nil || k != 'a' {
		t.Errorf("while suspended = %#x, %v; want 'a'", k, err)
	}
	restore()
	if _, err := busy.ReadKey(); !errors.Is(err, ErrTimeLimit) {
		t.Errorf("after restoring: err = %v, want ErrTimeLimit", err)
	}

	// Clearing it restores ordinary reads.
	busy.SetSessionDeadline(time.Time{})
	if k, err := busy.ReadKey(); err != nil || k != 'b' {
		t.Errorf("after clearing = %#x, %v; want 'b'", k, err)
	}
}

// The raw Read path serves pushed-back bytes before new input, so a byte an
// escape-sequence lookahead rejected is not lost to the next reader.
func TestReadServesPushedBackByteFirst(t *testing.T) {
	ih := NewInputHandler(bytes.NewReader([]byte("\x1bab")))
	if k, err := ih.ReadKey(); err != nil || k != KeyEsc {
		t.Fatalf("ReadKey = %#x, %v; want KeyEsc", k, err)
	}

	if n, err := ih.Read(nil); n != 0 || err != nil {
		t.Errorf("Read(nil) = %d, %v; want 0, nil", n, err)
	}
	buf := make([]byte, 4)
	for _, want := range []byte{'a', 'b'} {
		if n, err := ih.Read(buf); n != 1 || err != nil || buf[0] != want {
			t.Errorf("Read = %d, %v, %q; want 1, nil, %q", n, err, buf[0], want)
		}
	}
	if n, err := ih.Read(buf); n != 0 || err != io.EOF {
		t.Errorf("Read at end of input = %d, %v; want 0, io.EOF", n, err)
	}
}

func TestIsTimeoutError(t *testing.T) {
	if !isTimeoutError(errTimeout) {
		t.Error("errTimeout is not recognised as a timeout")
	}
	if isTimeoutError(nil) || isTimeoutError(io.EOF) {
		t.Error("nil and io.EOF must not count as timeouts")
	}
	if errTimeout.Error() != "i/o timeout" || !errTimeout.Temporary() {
		t.Errorf("errTimeout = %q (temporary=%v), want a temporary \"i/o timeout\"", errTimeout.Error(), errTimeout.Temporary())
	}
}

func TestIsPrintable(t *testing.T) {
	for key, want := range map[int]bool{
		' ': true, 'a': true, '~': true,
		KeyEsc: false, KeyEnter: false, KeyDelete: false, KeyArrowUp: false, 0x80: false,
	} {
		if got := IsPrintable(key); got != want {
			t.Errorf("IsPrintable(%#x) = %v, want %v", key, got, want)
		}
	}
}
