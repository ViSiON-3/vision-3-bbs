package editor

import (
	"bytes"
	"io"
	"testing"
	"time"
)

func TestReadKeyDoubleEscape(t *testing.T) {
	// Two ESC bytes back-to-back must decode as two bare KeyEsc events.
	ih := NewInputHandler(bytes.NewReader([]byte{0x1B, 0x1B}))
	k1, err := ih.ReadKey()
	if err != nil || k1 != KeyEsc {
		t.Fatalf("first key = %d, err = %v; want KeyEsc", k1, err)
	}
	k2, err := ih.ReadKey()
	if err != nil || k2 != KeyEsc {
		t.Fatalf("second key = %d, err = %v; want KeyEsc", k2, err)
	}
}

func TestReadKeyEnterSwallowsTrailingLFOrNUL(t *testing.T) {
	// CR+LF (SSH) and CR+NUL (telnet NVT) must each decode as a single
	// KeyEnter followed directly by the next real key.
	for _, tail := range []byte{0x0A, 0x00} {
		ih := NewInputHandler(bytes.NewReader([]byte{'\r', tail, 'x'}))
		k1, err := ih.ReadKey()
		if err != nil || k1 != KeyEnter {
			t.Fatalf("tail %#x: first key = %d, err = %v; want KeyEnter", tail, k1, err)
		}
		k2, err := ih.ReadKey()
		if err != nil || k2 != 'x' {
			t.Fatalf("tail %#x: second key = %d, err = %v; want 'x'", tail, k2, err)
		}
	}
}

// stagedInput returns an InputHandler fed by a goroutine that writes each
// chunk in turn with a pause before it, so bytes of one keypress can arrive
// after the enterTrailerWindow has closed.
func stagedInput(pause time.Duration, chunks ...string) *InputHandler {
	pr, pw := io.Pipe()
	go func() {
		for _, c := range chunks {
			time.Sleep(pause)
			_, _ = pw.Write([]byte(c))
		}
		_ = pw.Close()
	}()
	return NewInputHandler(pr)
}

// A CR LF Enter whose LF arrives after the lookahead window must still decode
// as a single Enter: the late LF is dropped, however late, provided it is the
// next byte (#415).
func TestReadKeyEnterDropsLateTrailer(t *testing.T) {
	for _, tail := range []string{"\n", "\x00"} {
		ih := stagedInput(5*enterTrailerWindow, "\r", tail+"x")
		if k, err := ih.ReadKey(); err != nil || k != KeyEnter {
			t.Fatalf("tail %q: first key = %d, err = %v; want KeyEnter", tail, k, err)
		}
		if k, err := ih.ReadKey(); err != nil || k != 'x' {
			t.Fatalf("tail %q: second key = %d, err = %v; want 'x'", tail, k, err)
		}
	}
}

// The raw Read path (used by the line-input prompts) drops a late trailer
// too, and leaves any other next byte alone.
func TestReadDropsOnlyLateTrailer(t *testing.T) {
	for _, tc := range []struct{ next, want string }{
		{"\nx", "x"},
		{"y", "y"},
	} {
		ih := stagedInput(5*enterTrailerWindow, "\r", tc.next)
		buf := make([]byte, 1)
		if _, err := ih.Read(buf); err != nil || buf[0] != '\r' {
			t.Fatalf("next %q: first byte = %q, err = %v; want CR", tc.next, buf[0], err)
		}
		ih.SkipEnterTrailer()
		if _, err := ih.Read(buf); err != nil || string(buf) != tc.want {
			t.Fatalf("next %q: second byte = %q, err = %v; want %q", tc.next, buf[0], err, tc.want)
		}
	}
}
