package scripting

import (
	"context"
	"io"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
)

// interruptibleSession is a fake session that mimics a live SSH/telnet
// connection. Read replays any scripted bytes, then blocks until the read
// interrupt is closed — mirroring the real adapters' SetReadInterrupt — at
// which point it returns an error WITHOUT consuming a keypress. This is the
// exact contract the door handlers rely on to stop the copier goroutine.
type interruptibleSession struct {
	mu        sync.Mutex
	data      []byte
	chunks    [][]byte // replayed after data, one chunk per Read
	interrupt chan struct{}
	out       []byte
}

func newInterruptibleSession(data string) *interruptibleSession {
	return &interruptibleSession{data: []byte(data), interrupt: make(chan struct{})}
}

func (s *interruptibleSession) Read(p []byte) (int, error) {
	s.mu.Lock()
	if len(s.data) == 0 && len(s.chunks) > 0 {
		s.data, s.chunks = s.chunks[0], s.chunks[1:]
	}
	if len(s.data) > 0 {
		n := copy(p, s.data)
		s.data = s.data[n:]
		s.mu.Unlock()
		return n, nil
	}
	s.mu.Unlock()
	<-s.interrupt
	return 0, io.EOF
}

func (s *interruptibleSession) Write(p []byte) (int, error) {
	s.mu.Lock()
	s.out = append(s.out, p...)
	s.mu.Unlock()
	return len(p), nil
}

// closeInterrupt mimics the door handler closing the SetReadInterrupt channel
// after the script ends.
func (s *interruptibleSession) closeInterrupt() { close(s.interrupt) }

// waitNoGoroutineLeak polls until the goroutine count drops back to (or below)
// the baseline, failing after 2s. Used to prove the engine's I/O pump and
// context-watcher goroutines all exit on Close.
func waitNoGoroutineLeak(t *testing.T, baseline int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if runtime.NumGoroutine() <= baseline {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("goroutine leak: have %d, baseline %d", runtime.NumGoroutine(), baseline)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestEngineCloseStopsIOGoroutines demonstrates that when a V3 script ends
// normally while the user stays connected, the copier (session->channel) and
// context-watcher goroutines all exit once the door
// handler closes the read interrupt and calls Close. A regression here would
// leave the copier blocked in session.Read, stealing the next menu keypress
// and leaking one goroutine per script run.
func TestEngineCloseStopsIOGoroutines(t *testing.T) {
	baseline := runtime.NumGoroutine()

	sess := newInterruptibleSession("A")
	eng := NewEngine(context.Background(), &SessionContext{
		Session:      sess,
		OutputMode:   ansi.OutputModeCP437,
		ScreenWidth:  80,
		ScreenHeight: 24,
	}, ScriptConfig{}, nil)

	// Door-handler cleanup sequence: interrupt the blocked Read, then Close.
	// Registered with t.Cleanup so an early Fatal can't leak the pump
	// goroutines into later tests; the explicit call below goes through the
	// same once-guarded path.
	var cleanupOnce sync.Once
	cleanup := func() {
		cleanupOnce.Do(func() {
			sess.closeInterrupt()
			eng.Close()
		})
	}
	t.Cleanup(cleanup)

	// Consume the one scripted byte the way a script's first key read would;
	// this starts the I/O pump goroutines and then leaves the copier blocked
	// in session.Read — the state at normal script end.
	if key, err := eng.readKey(2 * time.Second); err != nil || key != "A" {
		t.Fatalf("readKey = %q, %v; want \"A\", nil", key, err)
	}

	cleanup()

	// The copier must have actually exited (not merely timed out in Close).
	select {
	case <-eng.copierDone:
	default:
		t.Fatal("copier goroutine did not exit before Close returned")
	}

	waitNoGoroutineLeak(t, baseline)
}

// TestEngineCloseWithoutInputIsClean verifies that a script which never reads
// input starts no pump goroutines and closes cleanly.
func TestEngineCloseWithoutInputIsClean(t *testing.T) {
	baseline := runtime.NumGoroutine()

	sess := newInterruptibleSession("")
	eng := NewEngine(context.Background(), &SessionContext{
		Session:    sess,
		OutputMode: ansi.OutputModeCP437,
	}, ScriptConfig{}, nil)
	eng.Close()

	if eng.copierDone != nil {
		t.Fatal("copier goroutine started for a script that never read input")
	}
	waitNoGoroutineLeak(t, baseline)
}

// TestEngineCloseReturnsUnreadInput verifies that keys arriving in the same
// read as the one a script consumed are returned by Close, not dropped.
func TestEngineCloseReturnsUnreadInput(t *testing.T) {
	sess := newInterruptibleSession("abc")
	eng := NewEngine(context.Background(), &SessionContext{Session: sess}, ScriptConfig{}, nil)
	t.Cleanup(func() { eng.Close() })

	if key, err := eng.readKey(2 * time.Second); key != "a" || err != nil {
		t.Fatalf("readKey = %q, %v; want \"a\", nil", key, err)
	}
	sess.closeInterrupt()
	if got := string(eng.Close()); got != "bc" {
		t.Fatalf("Close returned %q, want \"bc\"", got)
	}
	if got := eng.Close(); got != nil {
		t.Fatalf("second Close returned %q, want nil", got)
	}
}

// TestEngineCloseReturnsQueuedAndHeldInput fills the copier's channel so the
// copier is left holding a chunk it cannot deliver when the script ends.
// Close must return the queued chunks and the held one, in order, and the
// copier must not read past them.
func TestEngineCloseReturnsQueuedAndHeldInput(t *testing.T) {
	sess := newInterruptibleSession("a")
	for _, c := range []string{"b", "c", "d", "e", "f", "g"} {
		sess.chunks = append(sess.chunks, []byte(c))
	}
	eng := NewEngine(context.Background(), &SessionContext{Session: sess}, ScriptConfig{}, nil)
	t.Cleanup(func() { eng.Close() })

	if key, err := eng.readKey(2 * time.Second); key != "a" || err != nil {
		t.Fatalf("readKey = %q, %v; want \"a\", nil", key, err)
	}
	// b..e fill the channel; the copier then reads f and blocks sending it.
	deadline := time.Now().Add(2 * time.Second)
	for {
		sess.mu.Lock()
		left := len(sess.chunks)
		sess.mu.Unlock()
		if left == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("copier did not read ahead: %d chunks left", left)
		}
		time.Sleep(5 * time.Millisecond)
	}

	sess.closeInterrupt()
	if got := string(eng.Close()); got != "bcdef" {
		t.Fatalf("Close returned %q, want \"bcdef\"", got)
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	if len(sess.chunks) != 1 || string(sess.chunks[0]) != "g" {
		t.Fatalf("copier read past the held chunk: %q left", sess.chunks)
	}
}

// lastWordsSession returns its data and io.EOF from the same Read, as a
// session read can when the caller hangs up.
type lastWordsSession struct {
	mu   sync.Mutex
	data []byte
}

func (s *lastWordsSession) Read(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := copy(p, s.data)
	s.data = s.data[n:]
	return n, io.EOF
}

func (*lastWordsSession) Write(p []byte) (int, error) { return len(p), nil }

// TestEngineKeepsBytesReadWithError verifies that bytes a session Read
// returns together with an error reach the script or Close, not neither.
func TestEngineKeepsBytesReadWithError(t *testing.T) {
	eng := NewEngine(context.Background(), &SessionContext{Session: &lastWordsSession{data: []byte("ab")}}, ScriptConfig{}, nil)
	t.Cleanup(func() { eng.Close() })

	var got []byte
	for {
		key, err := eng.readKey(2 * time.Second)
		if err != nil {
			break
		}
		if key == "" {
			t.Fatal("readKey timed out")
		}
		got = append(got, key...)
	}
	got = append(got, eng.Close()...)
	if string(got) != "ab" {
		t.Fatalf("script read and Close returned %q together, want \"ab\"", got)
	}
}

// waitChunksRead waits until the copier has read all but left of sess's
// chunks.
func waitChunksRead(t *testing.T, sess *interruptibleSession, left int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		sess.mu.Lock()
		n := len(sess.chunks) + len(sess.data)
		sess.mu.Unlock()
		if n == left {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("copier did not read ahead: %d chunks left", n)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestEngineSplitEnter verifies that the LF of an Enter split from its CR
// across two reads is not taken for a second key, by the script or by the
// prompt Close hands the rest to.
func TestEngineSplitEnter(t *testing.T) {
	readAEnter := func(t *testing.T, eng *Engine) {
		t.Helper()
		for _, w := range []string{"a", "\r"} {
			if key, err := eng.readKey(2 * time.Second); key != w || err != nil {
				t.Fatalf("readKey = %q, %v; want %q", key, err, w)
			}
		}
	}

	t.Run("script", func(t *testing.T) {
		sess := newInterruptibleSession("a\r")
		sess.chunks = [][]byte{[]byte("\ny")}
		eng := NewEngine(context.Background(), &SessionContext{Session: sess}, ScriptConfig{}, nil)
		t.Cleanup(func() { sess.closeInterrupt(); eng.Close() })
		readAEnter(t, eng)
		if key, err := eng.readKey(2 * time.Second); key != "y" || err != nil {
			t.Fatalf("key after Enter = %q, %v; want \"y\"", key, err)
		}
	})

	t.Run("next prompt", func(t *testing.T) {
		sess := newInterruptibleSession("a\r")
		sess.chunks = [][]byte{[]byte("\ny")}
		eng := NewEngine(context.Background(), &SessionContext{Session: sess}, ScriptConfig{}, nil)
		t.Cleanup(func() { eng.Close() })
		readAEnter(t, eng)
		waitChunksRead(t, sess, 0)
		sess.closeInterrupt()
		if got := string(eng.Close()); got != "y" {
			t.Fatalf("Close returned %q, want \"y\"", got)
		}
	})

	t.Run("lone LF later is a key", func(t *testing.T) {
		sess := newInterruptibleSession("a\rb")
		sess.chunks = [][]byte{[]byte("\n")}
		eng := NewEngine(context.Background(), &SessionContext{Session: sess}, ScriptConfig{}, nil)
		t.Cleanup(func() { sess.closeInterrupt(); eng.Close() })
		readAEnter(t, eng)
		for _, w := range []string{"b", "\r"} {
			if key, err := eng.readKey(2 * time.Second); key != w || err != nil {
				t.Fatalf("readKey = %q, %v; want %q", key, err, w)
			}
		}
	})
}

// demandSession opts a borrowed input source into reads requested by the script.
type demandSession struct{ *interruptibleSession }

// InputOnDemand leaves later keys with the owner of the borrowed input source.
func (s demandSession) InputOnDemand() bool { return true }

// TestDemandReaderLeavesNextKey verifies that no second read is made after a
// script consumes one key, even when another key is already available.
func TestDemandReaderLeavesNextKey(t *testing.T) {
	sess := newInterruptibleSession("")
	sess.chunks = [][]byte{[]byte("x"), []byte("y")}
	eng := NewEngine(context.Background(), &SessionContext{Session: demandSession{sess}}, ScriptConfig{}, nil)
	defer eng.Close()
	if key, err := eng.readKey(time.Second); key != "x" || err != nil {
		t.Fatalf("readKey = %q, %v", key, err)
	}
	eng.Close()
	sess.mu.Lock()
	defer sess.mu.Unlock()
	if len(sess.chunks) != 1 || string(sess.chunks[0]) != "y" {
		t.Fatalf("next key was prefetched: %q", sess.chunks)
	}
}

// TestDemandReaderAfterTimeout verifies that a timed-out request stays pending
// and the next getkey receives its result without starting an extra read.
func TestDemandReaderAfterTimeout(t *testing.T) {
	reader, writer := io.Pipe()
	sess := &demandPipeSession{Reader: reader}
	eng := NewEngine(context.Background(), &SessionContext{Session: sess}, ScriptConfig{}, nil)
	defer func() { _ = reader.Close(); _ = writer.Close(); eng.Close() }()
	if key, err := eng.readKey(time.Millisecond); key != "" || err != nil {
		t.Fatalf("timed read = %q, %v", key, err)
	}
	go func() { _, _ = writer.Write([]byte("x")) }()
	if key, err := eng.readKey(time.Second); key != "x" || err != nil {
		t.Fatalf("next read = %q, %v", key, err)
	}
}

// demandPipeSession models a borrowed reader with input arriving after a timeout.
type demandPipeSession struct {
	io.Reader
	returned []byte
}

// Write discards output because these tests only exercise input.
func (*demandPipeSession) Write(p []byte) (int, error) { return len(p), nil }

// InputOnDemand enables borrowed-input semantics for the test pipe.
func (*demandPipeSession) InputOnDemand() bool { return true }

// ReturnInput records the bytes handed back to the owner at shutdown.
func (s *demandPipeSession) ReturnInput(p []byte) { s.returned = append(s.returned, p...) }

// TestDemandReaderReturnsTimedOutInput keeps a key that arrives after getkey
// times out available to the caller's next prompt.
func TestDemandReaderReturnsTimedOutInput(t *testing.T) {
	reader, writer := io.Pipe()
	sess := &demandPipeSession{Reader: reader}
	eng := NewEngine(context.Background(), &SessionContext{Session: sess}, ScriptConfig{}, nil)
	defer func() { _ = reader.Close(); _ = writer.Close(); eng.Close() }()
	if _, err := eng.readKey(time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte("y")); err != nil {
		t.Fatal(err)
	}
	eng.Close()
	if string(sess.returned) != "y" {
		t.Fatalf("returned %q, want y", sess.returned)
	}
}
