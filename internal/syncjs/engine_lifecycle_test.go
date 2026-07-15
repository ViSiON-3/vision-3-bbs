package syncjs

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
// exact contract the door handler relies on to stop the copier goroutine.
type interruptibleSession struct {
	mu        sync.Mutex
	data      []byte
	interrupt chan struct{}
	out       []byte
}

func newInterruptibleSession(data string) *interruptibleSession {
	return &interruptibleSession{data: []byte(data), interrupt: make(chan struct{})}
}

func (s *interruptibleSession) Read(p []byte) (int, error) {
	s.mu.Lock()
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

func (s *interruptibleSession) closeInterrupt() { close(s.interrupt) }

// waitNoGoroutineLeak polls until the goroutine count drops back to (or below)
// the baseline, failing after 2s.
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

func newLifecycleEngine(sess io.ReadWriter) *Engine {
	return NewEngine(context.Background(), &SessionContext{
		Session:      sess,
		OutputMode:   ansi.OutputModeCP437,
		ScreenWidth:  80,
		ScreenHeight: 24,
	}, SyncJSDoorConfig{WorkingDir: ".", ExecDir: ".", DataDir: ".", NodeDir: "."})
}

// TestEngineCloseStopsIOGoroutines demonstrates that when a SyncJS door ends
// normally while the user stays connected, the copier (session->pipe), reader
// (pipe->channel) and context-watcher goroutines all exit once the door
// handler closes the read interrupt and calls Close. A regression here would
// leave the copier blocked in session.Read, stealing the next menu keypress
// and leaking one goroutine per door run.
func TestEngineCloseStopsIOGoroutines(t *testing.T) {
	baseline := runtime.NumGoroutine()

	sess := newInterruptibleSession("A")
	eng := newLifecycleEngine(sess)

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

	// Consume the one scripted byte the way a door's first key read would;
	// this starts the I/O pump goroutines and then leaves the copier blocked
	// in session.Read — the state at normal door end.
	if key, err := eng.readKey(2 * time.Second); err != nil || key != "A" {
		t.Fatalf("readKey = %q, %v; want \"A\", nil", key, err)
	}

	cleanup()

	select {
	case <-eng.copierDone:
	default:
		t.Fatal("copier goroutine did not exit before Close returned")
	}

	waitNoGoroutineLeak(t, baseline)
}

// TestEngineCloseWithoutInputIsClean verifies that a door which never reads
// input starts no pump goroutines and closes cleanly.
func TestEngineCloseWithoutInputIsClean(t *testing.T) {
	baseline := runtime.NumGoroutine()

	eng := newLifecycleEngine(newInterruptibleSession(""))
	eng.Close()

	if eng.copierDone != nil {
		t.Fatal("copier goroutine started for a door that never read input")
	}
	waitNoGoroutineLeak(t, baseline)
}
