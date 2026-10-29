package editor

import (
	"errors"
	"io"
	"strings"
	"sync"
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

// A key arrives in one chunk, so a reader that decodes keys per chunk sees an
// arrow key and not Escape plus letters, and one Enter for CR LF; input
// typed ahead of it stays with the handler.
func TestReadUntilTakesOneKey(t *testing.T) {
	for _, tc := range []struct {
		queued, want, left string
	}{
		{"\x1b[Axy", "\x1b[A", "xy"},
		{"\x1bOPx", "\x1bOP", "x"},
		{"\x1b[15~x", "\x1b[15~", "x"},
		{"\r\nx", "\r\n", "x"},
		{"\r\x00x", "\r\x00", "x"},
		{"xy", "x", "y"},
		{"\x1bx", "\x1b", "x"},
	} {
		ih, w := pipeHandler(t)
		go func() { _, _ = w.Write([]byte(tc.queued)) }()
		waitQueued(t, ih, len(tc.queued))
		buf := make([]byte, 16)
		n, err := ih.ReadUntil(buf, nil)
		if err != nil || string(buf[:n]) != tc.want {
			t.Errorf("ReadUntil(%q) = %q, %v; want %q", tc.queued, buf[:n], err, tc.want)
			continue
		}
		var left []byte
		for len(left) < len(tc.left) {
			b, err := ih.ReadByteUntil(nil)
			if err != nil {
				t.Fatalf("reading what was left: %v", err)
			}
			left = append(left, b)
		}
		if string(left) != tc.left {
			t.Errorf("after ReadUntil(%q), left %q, want %q", tc.queued, left, tc.left)
		}
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

// A latched session end wins over a byte pushed back for re-reading.
func TestFailWinsOverUnreadByte(t *testing.T) {
	ih, _ := pipeHandler(t)
	ih.unreadByte('z')
	ih.Fail(ErrIdleTimeout)
	if _, err := ih.Read(make([]byte, 1)); !errors.Is(err, ErrIdleTimeout) {
		t.Errorf("Read after Fail = %v, want ErrIdleTimeout", err)
	}
}

// A stop that arrives while queued bytes are being drained gives every byte
// taken back to the handler, in order, the one already read included.
func TestDrainQueuedStopGivesBytesBack(t *testing.T) {
	ih, _ := pipeHandler(t)
	ih.unreadBuf = []byte("[A")
	stop := make(chan struct{})
	close(stop)
	p := make([]byte, 8)
	p[0] = 0x1B // taken by ReadUntil before the stop
	if _, err := ih.drainQueued(p, 1, stop); !errors.Is(err, ErrReadStopped) {
		t.Fatalf("drainQueued = %v, want ErrReadStopped", err)
	}
	if got := string(ih.unreadBuf); got != "\x1b[A" {
		t.Errorf("unread input = %q, want %q", got, "\x1b[A")
	}
}

// interruptReader is a session whose reads return its data, one byte at a
// time, and then block until the read interrupt is closed, as the SSH and
// telnet adapters' do.
type interruptReader struct {
	data []byte
	mu   sync.Mutex
	intr <-chan struct{}
}

func (r *interruptReader) SetReadInterrupt(ch <-chan struct{}) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if ch != nil {
		r.intr = ch
	}
}

func (r *interruptReader) Read(p []byte) (int, error) {
	r.mu.Lock()
	if len(r.data) > 0 {
		p[0], r.data = r.data[0], r.data[1:]
		r.mu.Unlock()
		return 1, nil
	}
	intr := r.intr
	r.mu.Unlock()
	<-intr
	return 0, errors.New("read interrupted")
}

// A handler stopped for a door hands back the bytes no one read, oldest
// first, without the LF of an Enter already read, and stops reading the
// session (#750).
func TestCloseAndTakeUnread(t *testing.T) {
	ih := NewInputHandler(&interruptReader{data: []byte("\nbcd")})
	waitQueued(t, ih, 4)
	ih.skipEnterTrailer = true
	ih.ReturnInput([]byte("a"))

	if got, trailer := ih.CloseAndTakeUnread(); string(got) != "abcd" || trailer {
		t.Fatalf("CloseAndTakeUnread = %q, %v; want %q, false", got, trailer, "abcd")
	}
	select {
	case <-ih.done:
	default:
		t.Fatal("reader goroutine still running")
	}
	if got, trailer := ih.CloseAndTakeUnread(); got != nil || trailer {
		t.Errorf("second CloseAndTakeUnread = %q, %v; want nil, false", got, trailer)
	}
}

// A handler stopped while the LF of an Enter it read is still to come says
// so, and a handler told to expect it drops it (#750).
func TestCloseAndTakeUnreadPendingTrailer(t *testing.T) {
	r := &interruptReader{}
	ih := NewInputHandler(r)
	ih.skipEnterTrailer = true
	if got, trailer := ih.CloseAndTakeUnread(); got != nil || !trailer {
		t.Fatalf("CloseAndTakeUnread = %q, %v; want nil, true", got, trailer)
	}

	r.data = []byte("\nx")
	next := NewInputHandler(r)
	defer next.CloseAndWait()
	next.ExpectEnterTrailer()
	if b, err := next.ReadByteUntil(nil); err != nil || b != 'x' {
		t.Errorf("next handler read %q, %v; want 'x'", b, err)
	}
}

// With a full queue the reader goroutine is blocked queueing a byte, not in
// a session read the interrupt can stop. CloseAndTakeUnread must still return,
// with that byte too.
func TestCloseAndTakeUnreadFullQueue(t *testing.T) {
	want := strings.Repeat("x", 300)
	ih := NewInputHandler(&interruptReader{data: []byte(want)})
	waitQueued(t, ih, cap(ih.incoming))

	done := make(chan []byte, 1)
	go func() {
		got, _ := ih.CloseAndTakeUnread()
		done <- got
	}()
	select {
	case got := <-done:
		if string(got) != want {
			t.Errorf("CloseAndTakeUnread returned %d bytes, want %d", len(got), len(want))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("CloseAndTakeUnread did not return")
	}
}
