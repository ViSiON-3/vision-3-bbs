package editor

import (
	"bytes"
	"errors"
	"io"
	"testing"
	"time"
)

// TestReadRawKeyOrEventPreservesNavigationAndControlKeys checks that raw event
// reads preserve navigation and control-key identities.
func TestReadRawKeyOrEventPreservesNavigationAndControlKeys(t *testing.T) {
	ih := NewInputHandler(bytes.NewBufferString("\x1b[A\x1b[B\x1b[C\x1b[D\x1b[H\x1b[F\x1b[3~\x17\x13"))
	defer ih.Close()
	for _, want := range []int{KeyArrowUp, KeyArrowDown, KeyArrowRight, KeyArrowLeft, KeyHome, KeyEnd, KeyDeleteKey, 0x17, 0x13} {
		key, _, event, err := ReadRawKeyOrEvent(ih, (<-chan int)(nil))
		if err != nil || event || key != want {
			t.Fatalf("key=%d, event=%v, err=%v; want %d", key, event, err, want)
		}
	}
}

// TestReadRawKeyOrEventHandlesEventsAndIdleTimeout checks queued event delivery
// and idle expiration when no input arrives.
func TestReadRawKeyOrEventHandlesEventsAndIdleTimeout(t *testing.T) {
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	ih := NewInputHandler(reader)
	defer ih.Close()
	events := make(chan int, 1)
	events <- 40
	key, ev, event, err := ReadRawKeyOrEvent(ih, events)
	if err != nil || !event || ev != 40 || key != 0 {
		t.Fatalf("key/event=%d/%d (%v), err=%v", key, ev, event, err)
	}
	ih.SetSessionIdleTimeout(10 * time.Millisecond)
	_, _, _, err = ReadRawKeyOrEvent(ih, events)
	if !errors.Is(err, ErrIdleTimeout) {
		t.Fatalf("timeout=%v", err)
	}
}
