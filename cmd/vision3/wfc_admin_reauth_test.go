package main

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// TestWatchAdminAuthorizationKicksOnRevocation verifies that an open admin
// session is terminated when its authorization stops holding (key revoked,
// user demoted, or WFC access disabled mid-session).
func TestWatchAdminAuthorizationKicksOnRevocation(t *testing.T) {
	var allowed atomic.Bool
	allowed.Store(true)
	kicked := make(chan struct{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go watchAdminAuthorization(ctx, "boss", time.Millisecond,
		func(string) bool { return allowed.Load() },
		func() { close(kicked) })

	// Still authorized: must not be kicked.
	select {
	case <-kicked:
		t.Fatal("session kicked while still authorized")
	case <-time.After(20 * time.Millisecond):
	}

	allowed.Store(false)
	select {
	case <-kicked:
	case <-time.After(time.Second):
		t.Fatal("session not kicked after authorization was revoked")
	}
}

// TestWatchAdminAuthorizationStopsOnContextCancel verifies the watcher exits
// without kicking when the session ends normally.
func TestWatchAdminAuthorizationStopsOnContextCancel(t *testing.T) {
	var kicks atomic.Int32
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		watchAdminAuthorization(ctx, "boss", time.Millisecond,
			func(string) bool { return true },
			func() { kicks.Add(1) })
		close(done)
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("watcher did not exit on context cancellation")
	}
	if kicks.Load() != 0 {
		t.Fatalf("watcher kicked %d times on a normal shutdown, want 0", kicks.Load())
	}
}
