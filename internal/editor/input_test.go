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
