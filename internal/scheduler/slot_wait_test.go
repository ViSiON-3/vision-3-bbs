package scheduler

import (
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

// holdSlot takes one of s's concurrency slots, as an event in flight would,
// and returns a func that gives it back.
func holdSlot(t *testing.T, s *Scheduler) func() {
	t.Helper()
	s.mu.Lock()
	sem := s.concurrencySem
	s.mu.Unlock()
	sem <- struct{}{}
	return func() { <-sem }
}

// waitFor polls cond until it holds or a deadline passes.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (s *Scheduler) historyOf(id string) EventHistory {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if h := s.history[id]; h != nil {
		return *h
	}
	return EventHistory{}
}

func (s *Scheduler) isWaiting(id string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.waitingEvents[id]
}

// TestEventWaitsForFreeSlot pins #694: an event that fires while every slot
// is taken waits and then runs, instead of being dropped.
func TestEventWaitsForFreeSlot(t *testing.T) {
	s := newTestScheduler(t, config.EventsConfig{MaxConcurrentEvents: 1})
	release := holdSlot(t, s)

	ev := config.EventConfig{ID: "nightly_purge", Name: "Purge", Command: lookPath(t, "echo"), TimeoutSeconds: testEventTimeout}
	ran := make(chan bool, 1)
	go func() { _, ok := s.runWithSlot(ev); ran <- ok }()

	waitFor(t, "the event to wait for a slot", func() bool { return s.isWaiting(ev.ID) })
	release()

	if ok := <-ran; !ok {
		t.Fatal("event did not run once a slot freed")
	}
	h := s.historyOf(ev.ID)
	if h.RunCount != 1 || h.SkipCount != 0 {
		t.Errorf("history = %+v, want 1 run and no skips", h)
	}
	if s.isWaiting(ev.ID) {
		t.Error("event still marked waiting after it ran")
	}
}

// TestEventSkippedWhenNoSlotWithinTimeout checks that an event gives up
// after its timeout and records the skip in its history.
func TestEventSkippedWhenNoSlotWithinTimeout(t *testing.T) {
	s := newTestScheduler(t, config.EventsConfig{MaxConcurrentEvents: 1})
	defer holdSlot(t, s)()

	ev := config.EventConfig{ID: "nightly_pack", Name: "Pack", Command: "unused", TimeoutSeconds: 1}
	if _, ok := s.runWithSlot(ev); ok {
		t.Fatal("event ran with no slot free")
	}
	h := s.historyOf(ev.ID)
	if h.SkipCount != 1 || h.LastSkipReason != skipNoSlot || h.LastSkipped.IsZero() || h.RunCount != 0 {
		t.Errorf("history = %+v, want one skip for %q and no runs", h, skipNoSlot)
	}
}

// TestWaitingEventCountsAsRunning checks that a second firing of an event
// already waiting for a slot is skipped, not queued behind it.
func TestWaitingEventCountsAsRunning(t *testing.T) {
	s := newTestScheduler(t, config.EventsConfig{MaxConcurrentEvents: 1})
	release := holdSlot(t, s)

	ev := config.EventConfig{ID: "scan", Name: "Scan", Command: lookPath(t, "echo"), TimeoutSeconds: testEventTimeout}
	first := make(chan bool, 1)
	go func() { _, ok := s.runWithSlot(ev); first <- ok }()
	waitFor(t, "the first firing to wait", func() bool { return s.isWaiting(ev.ID) })

	if _, ok := s.runWithSlot(ev); ok {
		t.Error("second firing ran while the first was waiting")
	}
	if h := s.historyOf(ev.ID); h.SkipCount != 1 || h.LastSkipReason != skipAlreadyRunning {
		t.Errorf("history = %+v, want one %q skip", h, skipAlreadyRunning)
	}

	release()
	if ok := <-first; !ok {
		t.Error("first firing did not run")
	}
}

// TestStopReleasesWaitingEvents checks that shutdown ends a wait with no
// timeout instead of blocking on it.
func TestStopReleasesWaitingEvents(t *testing.T) {
	s := newTestScheduler(t, config.EventsConfig{MaxConcurrentEvents: 1})
	defer holdSlot(t, s)()

	ev := config.EventConfig{ID: "daemon", Name: "Daemon", Command: "unused"} // no timeout: waits until shutdown
	ran := make(chan bool, 1)
	go func() { _, ok := s.runWithSlot(ev); ran <- ok }()
	waitFor(t, "the event to wait", func() bool { return s.isWaiting(ev.ID) })

	s.Stop()
	select {
	case ok := <-ran:
		if ok {
			t.Error("event ran during shutdown")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Stop did not release the waiting event")
	}
	if h := s.historyOf(ev.ID); h.LastSkipReason != skipStopping {
		t.Errorf("skip reason = %q, want %q", h.LastSkipReason, skipStopping)
	}
}

// TestStatusReportsWaitingAndSkips checks the Events tab view carries the
// waiting mark and the skip history.
func TestStatusReportsWaitingAndSkips(t *testing.T) {
	ev := config.EventConfig{ID: "purge", Name: "Purge", Command: "unused", Enabled: true, TimeoutSeconds: 1}
	s := newTestScheduler(t, config.EventsConfig{MaxConcurrentEvents: 1, Events: []config.EventConfig{ev}})
	defer holdSlot(t, s)()

	done := make(chan struct{})
	go func() { s.runWithSlot(ev); close(done) }()
	waitFor(t, "the event to wait", func() bool { return s.isWaiting(ev.ID) })
	if st := s.Status(time.Now()); !st[0].Waiting || st[0].Running {
		t.Errorf("status while waiting = %+v, want Waiting and not Running", st[0])
	}
	<-done
	st := s.Status(time.Now())[0]
	if st.Waiting || st.SkipCount != 1 || st.LastSkipReason != skipNoSlot || st.LastSkipped.IsZero() {
		t.Errorf("status after giving up = %+v, want one %q skip", st, skipNoSlot)
	}
}

// TestWaitingEventUsesReloadedSemaphore checks that raising the limit while
// an event waits lets it run on the new semaphore's free capacity, while
// the slot held on the old one still releases into it.
func TestWaitingEventUsesReloadedSemaphore(t *testing.T) {
	s := newTestScheduler(t, config.EventsConfig{MaxConcurrentEvents: 1})
	defer holdSlot(t, s)() // the long-running event on the old semaphore

	ev := config.EventConfig{ID: "nightly_purge", Name: "Purge", Command: lookPath(t, "echo"), TimeoutSeconds: testEventTimeout}
	ran := make(chan bool, 1)
	go func() { _, ok := s.runWithSlot(ev); ran <- ok }()
	waitFor(t, "the event to wait", func() bool { return s.isWaiting(ev.ID) })

	s.Reload(config.EventsConfig{MaxConcurrentEvents: 4})
	select {
	case ok := <-ran:
		if !ok {
			t.Error("event skipped after the limit was raised")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("event still waiting after Reload raised the limit")
	}
}
