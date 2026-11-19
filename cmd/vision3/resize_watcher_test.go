package main

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gliderlabs/ssh"
)

// watcherRig runs a resizeWatcher against a transfer flag the test sets and
// records each size the terminal is given.
type watcherRig struct {
	transfer atomic.Bool
	checks   atomic.Int32 // times the watcher looked at the transfer flag
	mu       sync.Mutex
	sizes    [][2]int
	resizes  chan ssh.Window
}

func newWatcherRig(t *testing.T) *watcherRig {
	t.Helper()
	r := &watcherRig{resizes: make(chan ssh.Window)}
	rw := &resizeWatcher{
		node: 1,
		transferActive: func() bool {
			v := r.transfer.Load()
			r.checks.Add(1)
			return v
		},
		setSize: func(w, h int) {
			r.mu.Lock()
			r.sizes = append(r.sizes, [2]int{w, h})
			r.mu.Unlock()
		},
		onSize:     func(ssh.Window) {},
		replayPoll: 5 * time.Millisecond,
	}
	done := make(chan struct{})
	go func() { rw.run(r.resizes); close(done) }()
	t.Cleanup(func() { close(r.resizes); <-done })
	return r
}

// resizeDuringTransfer sends a window change while a transfer runs, and
// ends the transfer only once the watcher has seen it running.
func (r *watcherRig) resizeDuringTransfer(t *testing.T, w, h int) {
	t.Helper()
	r.transfer.Store(true)
	before := r.checks.Load()
	r.resizes <- ssh.Window{Width: w, Height: h}
	deadline := time.Now().Add(5 * time.Second)
	for r.checks.Load() == before {
		if time.Now().After(deadline) {
			t.Fatal("the watcher never looked at the transfer")
		}
		time.Sleep(time.Millisecond)
	}
	r.transfer.Store(false)
}

// waitFor waits for the terminal to be given w x h, the size it was last given.
func (r *watcherRig) waitFor(t *testing.T, w, h int, what string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		r.mu.Lock()
		var last [2]int
		if n := len(r.sizes); n > 0 {
			last = r.sizes[n-1]
		}
		got := append([][2]int(nil), r.sizes...)
		r.mu.Unlock()
		if last == [2]int{w, h} {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s: the terminal was never given %dx%d; sizes given: %v", what, w, h, got)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// A resize held back during each of two transfers in a row, with no resize
// between them, is applied when each transfer ends.
func TestResizeHeldBackDuringEachTransferIsReplayed(t *testing.T) {
	r := newWatcherRig(t)

	r.resizeDuringTransfer(t, 100, 30)
	r.waitFor(t, 100, 30, "first transfer")
	r.resizeDuringTransfer(t, 120, 40)
	r.waitFor(t, 120, 40, "second transfer")
}

// A resize outside a transfer is applied at once, and one held back is still
// replayed after a transfer that follows it.
func TestResizeOutsideATransferIsAppliedAtOnce(t *testing.T) {
	r := newWatcherRig(t)
	r.resizes <- ssh.Window{Width: 90, Height: 25}
	r.waitFor(t, 90, 25, "no transfer")

	r.resizeDuringTransfer(t, 110, 35)
	r.waitFor(t, 110, 35, "transfer after a plain resize")
}

// A size given after a transfer has ended wins over the one held back during
// it, whichever the watcher sees first.
func TestNewerResizeWinsOverOneHeldBack(t *testing.T) {
	r := newWatcherRig(t)
	r.resizeDuringTransfer(t, 100, 30)
	r.resizes <- ssh.Window{Width: 130, Height: 45}
	r.waitFor(t, 130, 45, "the newer size")
	time.Sleep(50 * time.Millisecond) // several replay polls
	r.mu.Lock()
	last := r.sizes[len(r.sizes)-1]
	r.mu.Unlock()
	if last != [2]int{130, 45} {
		t.Errorf("the held-back size was applied over the newer one: last %v", last)
	}
}
