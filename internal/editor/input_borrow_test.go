package editor

import (
	"errors"
	"io"
	"testing"
	"time"
)

// pipeHandler returns a handler fed by a pipe the test writes to.
func pipeHandler(t *testing.T) (*InputHandler, *io.PipeWriter) {
	t.Helper()
	r, w := io.Pipe()
	t.Cleanup(func() { _ = w.Close() })
	return NewInputHandler(r), w
}

// waitQueued blocks until n bytes sit in the handler's channel.
func waitQueued(t *testing.T, ih *InputHandler, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for len(ih.incoming) < n {
		if time.Now().After(deadline) {
			t.Fatalf("only %d of %d bytes queued", len(ih.incoming), n)
		}
		time.Sleep(time.Millisecond)
	}
}

// An arrow key's escape sequence reaches a borrower in one chunk, so a reader
// that decodes keys per chunk sees the key and not Escape plus letters.
func TestReadUntilTakesEverythingQueued(t *testing.T) {
	ih, w := pipeHandler(t)
	go func() { _, _ = w.Write([]byte("\x1b[A")) }()
	waitQueued(t, ih, 3)
	buf := make([]byte, 16)
	n, err := ih.ReadUntil(buf, nil)
	if err != nil || string(buf[:n]) != "\x1b[A" {
		t.Errorf("ReadUntil = %q, %v; want the whole sequence", buf[:n], err)
	}
}

// A stopped borrower leaves queued input for the handler's owner.
func TestReadUntilStopLeavesInput(t *testing.T) {
	ih, w := pipeHandler(t)
	go func() { _, _ = w.Write([]byte("k")) }()
	waitQueued(t, ih, 1)
	stop := make(chan struct{})
	close(stop)
	for i := 0; i < 50; i++ {
		if _, err := ih.ReadUntil(make([]byte, 4), stop); !errors.Is(err, ErrReadStopped) {
			t.Fatalf("ReadUntil after stop = %v, want ErrReadStopped", err)
		}
	}
	if b, err := ih.ReadByteUntil(nil); err != nil || b != 'k' {
		t.Errorf("owner's read = %q, %v; want 'k'", b, err)
	}
}

// The LF of a CR LF Enter the owner already read is not a key for a borrower.
func TestReadUntilDropsLateEnterTrailer(t *testing.T) {
	ih, w := pipeHandler(t)
	ih.skipEnterTrailer = true
	go func() { _, _ = w.Write([]byte("\nx")) }()
	waitQueued(t, ih, 2)
	if b, err := ih.ReadByteUntil(nil); err != nil || b != 'x' {
		t.Errorf("ReadByteUntil = %q, %v; want 'x'", b, err)
	}
}

// Fail ends every later key read with its error, with keys waiting or not.
func TestFailLatchesKeyReads(t *testing.T) {
	ih, w := pipeHandler(t)
	go func() { _, _ = w.Write([]byte("a")) }()
	waitQueued(t, ih, 1)
	ih.Fail(ErrIdleTimeout)
	ih.Fail(io.EOF) // the first error wins
	if _, err := ih.ReadKey(); !errors.Is(err, ErrIdleTimeout) {
		t.Errorf("ReadKey after Fail = %v, want ErrIdleTimeout", err)
	}
	if _, err := ih.Read(make([]byte, 1)); !errors.Is(err, ErrIdleTimeout) {
		t.Errorf("Read after Fail = %v, want ErrIdleTimeout", err)
	}
}
