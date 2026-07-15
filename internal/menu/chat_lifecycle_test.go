package menu

import (
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/chat"
)

// TestChatEventPumpExitsOnClose demonstrates the shutdown contract runChat's
// cleanup() relies on: the event-pump goroutine ranges over svc.Events(), and
// cleanup() calls svc.Close() then blocks on <-done. If Close() did not close
// the Events() channel, the range never ends, close(done) never runs, and the
// session goroutine would hang forever on every chat exit (including abnormal
// disconnect, which reaches cleanup() via the io.EOF path).
//
// This exercises the real LocalChatService using the same pump/cleanup shape
// as runChat, and asserts no goroutine leak.
func TestChatEventPumpExitsOnClose(t *testing.T) {
	baseline := runtime.NumGoroutine()

	dbPath := filepath.Join(t.TempDir(), "chat.db")
	svc, err := chat.NewLocalChatService("tester", dbPath)
	if err != nil {
		t.Fatalf("NewLocalChatService: %v", err)
	}
	// Fallback so an early Fatal can't leave the service (and its pump
	// goroutines/DB handle) open for later tests. The closed flag prevents
	// a double Close of the events channel.
	closed := false
	t.Cleanup(func() {
		if !closed {
			_ = svc.Close()
		}
	})
	if _, _, err := svc.Join("lobby"); err != nil {
		t.Fatalf("Join: %v", err)
	}

	// Mirror runChat's event pump goroutine.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range svc.Events() {
			// Drain; runChat renders these to the session.
		}
	}()

	// Mirror runChat's cleanup(): leave, close, wait for the pump.
	if err := svc.Leave("lobby"); err != nil {
		t.Errorf("Leave: %v", err)
	}
	closed = true
	if err := svc.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("event pump goroutine did not exit after svc.Close(); cleanup would hang")
	}

	waitChatNoGoroutineLeak(t, baseline)
}

func waitChatNoGoroutineLeak(t *testing.T, baseline int) {
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
