package admin

import (
	"errors"
	"sync"
	"testing"
	"time"
)

// fakeRequester scripts SendRequest: each call pops the next behaviour.
type fakeRequester struct {
	mu    sync.Mutex
	calls int
	err   error
	block chan struct{} // when non-nil, SendRequest blocks until closed
}

func (f *fakeRequester) SendRequest(string, bool, []byte) (bool, []byte, error) {
	f.mu.Lock()
	f.calls++
	err, block := f.err, f.block
	f.mu.Unlock()
	if block != nil {
		<-block
	}
	// A "false" reply is what a server without a handler sends; it still
	// proves liveness.
	return false, nil, err
}

func (f *fakeRequester) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func waitFor(t *testing.T, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestKeepAliveHealthyPeerNeverDies(t *testing.T) {
	fr := &fakeRequester{}
	dead := make(chan error, 1)
	stop := KeepAlive(fr, 10*time.Millisecond, 50*time.Millisecond, func(err error) { dead <- err })
	defer stop()
	waitFor(t, func() bool { return fr.count() >= 3 }, "three keepalives")
	select {
	case err := <-dead:
		t.Fatalf("healthy peer reported dead: %v", err)
	default:
	}
}

func TestKeepAliveTransportErrorReportsDeath(t *testing.T) {
	fr := &fakeRequester{err: errors.New("broken pipe")}
	dead := make(chan error, 1)
	stop := KeepAlive(fr, 10*time.Millisecond, 50*time.Millisecond, func(err error) { dead <- err })
	defer stop()
	select {
	case err := <-dead:
		if err == nil || !errors.Is(err, fr.err) {
			t.Fatalf("death reason = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("transport error not reported")
	}
}

func TestKeepAliveSilentPeerTimesOut(t *testing.T) {
	fr := &fakeRequester{block: make(chan struct{})}
	defer close(fr.block)
	dead := make(chan error, 1)
	stop := KeepAlive(fr, 10*time.Millisecond, 30*time.Millisecond, func(err error) { dead <- err })
	defer stop()
	select {
	case err := <-dead:
		if !errors.Is(err, ErrKeepAliveTimeout) {
			t.Fatalf("death reason = %v, want timeout", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("silent peer not reported")
	}
}

func TestKeepAliveStopIsIdempotentAndQuiet(t *testing.T) {
	fr := &fakeRequester{}
	died := false
	stop := KeepAlive(fr, 5*time.Millisecond, 50*time.Millisecond, func(error) { died = true })
	stop()
	stop()
	n := fr.count()
	time.Sleep(30 * time.Millisecond)
	if fr.count() > n+1 || died {
		t.Fatalf("keepalive kept running after stop: calls %d→%d died=%v", n, fr.count(), died)
	}
}
