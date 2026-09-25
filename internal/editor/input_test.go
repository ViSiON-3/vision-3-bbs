package editor

import (
	"bytes"
	"testing"
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
