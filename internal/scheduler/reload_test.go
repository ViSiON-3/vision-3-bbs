package scheduler

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

func testEvent(t *testing.T, id, schedule string, startup bool) config.EventConfig {
	t.Helper()
	return config.EventConfig{
		ID:       id,
		Name:     id,
		Schedule: schedule,
		// Resolved via lookPath so platforms without the command (Windows)
		// skip instead of failing when a test actually executes it.
		Command:      lookPath(t, "true"),
		Enabled:      true,
		RunAtStartup: startup,
	}
}

func newTestScheduler(t *testing.T, cfg config.EventsConfig) *Scheduler {
	t.Helper()
	return NewScheduler(cfg, filepath.Join(t.TempDir(), "history.json"))
}

// startScheduler runs s.Start in the background and registers cleanup that
// cancels it and waits for it to return. Waiting matters: Start's shutdown
// path writes the history file, and a goroutine left running past test end
// races t.TempDir's removal of the directory it writes into.
func startScheduler(t *testing.T, s *Scheduler) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.Start(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
}

func TestReloadSwapsScheduledEvents(t *testing.T) {
	s := newTestScheduler(t, config.EventsConfig{
		Events: []config.EventConfig{testEvent(t, "old-a", "0 3 * * *", false), testEvent(t, "old-b", "0 4 * * *", false)},
	})
	startScheduler(t, s)

	// Wait for startup scheduling to land.
	deadline := time.Now().Add(2 * time.Second)
	for s.ScheduledCount() < 2 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := s.ScheduledCount(); got != 2 {
		t.Fatalf("entries after Start = %d, want 2", got)
	}

	s.Reload(config.EventsConfig{
		Events: []config.EventConfig{testEvent(t, "new-a", "0 5 * * *", false)},
	})

	if got := s.ScheduledCount(); got != 1 {
		t.Errorf("entries after Reload = %d, want 1 (old events replaced)", got)
	}
}

// TestReloadKeepsValidSchedulesOnPartialError: one bad cron expression must
// not take down the events that parse.
func TestReloadKeepsValidSchedulesOnPartialError(t *testing.T) {
	s := newTestScheduler(t, config.EventsConfig{})

	s.Reload(config.EventsConfig{
		Events: []config.EventConfig{
			testEvent(t, "good", "0 3 * * *", false),
			testEvent(t, "bad", "not a cron line", false),
			testEvent(t, "also-good", "30 6 * * *", false),
		},
	})

	if got := s.ScheduledCount(); got != 2 {
		t.Errorf("entries = %d, want 2 (the two valid schedules)", got)
	}
}

// TestReloadDoesNotFireStartupEvents: run-at-startup events belong to process
// startup; a config save must not re-run them.
func TestReloadDoesNotFireStartupEvents(t *testing.T) {
	s := newTestScheduler(t, config.EventsConfig{})

	// A startup-only event (no schedule) and a scheduled one.
	s.Reload(config.EventsConfig{
		Events: []config.EventConfig{
			testEvent(t, "startup-only", "", true),
			testEvent(t, "cron", "0 3 * * *", false),
		},
	})

	if got := s.ScheduledCount(); got != 1 {
		t.Errorf("entries = %d, want 1 (startup-only event not scheduled)", got)
	}
	s.mu.Lock()
	running := len(s.runningEvents)
	s.mu.Unlock()
	if running != 0 {
		t.Errorf("%d events running after Reload, want 0 (startup events must not fire)", running)
	}
}

// TestReloadResizesConcurrency: a changed maxConcurrentEvents takes effect,
// and in-flight releases pair with the semaphore they acquired from.
func TestReloadResizesConcurrency(t *testing.T) {
	s := newTestScheduler(t, config.EventsConfig{MaxConcurrentEvents: 1})

	s.mu.Lock()
	oldSem := s.concurrencySem
	s.mu.Unlock()
	if cap(oldSem) != 1 {
		t.Fatalf("initial semaphore capacity = %d, want 1", cap(oldSem))
	}

	// Hold the only slot, as an in-flight event would.
	oldSem <- struct{}{}

	s.Reload(config.EventsConfig{MaxConcurrentEvents: 4})

	s.mu.Lock()
	newSem := s.concurrencySem
	s.mu.Unlock()
	if cap(newSem) != 4 {
		t.Errorf("semaphore capacity after Reload = %d, want 4", cap(newSem))
	}
	if newSem == oldSem {
		t.Error("semaphore was not replaced despite the limit changing")
	}

	// The in-flight release goes to the old semaphore and must not block.
	done := make(chan struct{})
	go func() { <-oldSem; close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Error("release into the old semaphore blocked")
	}
}

// TestReloadRaceWithExecution runs Reload concurrently with event execution
// attempts; under -race this proves the config/cron/semaphore swaps are safe
// against the paths cron jobs take.
func TestReloadRaceWithExecution(t *testing.T) {
	s := newTestScheduler(t, config.EventsConfig{MaxConcurrentEvents: 2})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.ctx = ctx

	const reloads = 200
	var wg sync.WaitGroup
	done := make(chan struct{})

	// Events are built on the test goroutine: testEvent resolves its command
	// via lookPath, whose skip-if-missing must not fire inside a goroutine.
	reloadEv := testEvent(t, "e", "0 3 * * *", false)
	runnerEv := testEvent(t, "runner", "", false)

	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(done)
		for i := 0; i < reloads; i++ {
			s.Reload(config.EventsConfig{
				MaxConcurrentEvents: 1 + i%3,
				Events:              []config.EventConfig{reloadEv},
			})
		}
	}()

	for r := 0; r < 4; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ev := runnerEv
			for {
				select {
				case <-done:
					return
				default:
				}
				s.executeEventWithConcurrency(ev)
			}
		}()
	}

	wg.Wait()
}

// TestReloadDuringStartup overlaps Reload with Start's own scheduling pass.
// Whichever order they land in, exactly one cron survives with the reloaded
// config — Start must not resurrect the instance Reload retired, and the two
// must not race on the shared config (this is what -race checks here).
func TestReloadDuringStartup(t *testing.T) {
	events := make([]config.EventConfig, 50)
	for i := range events {
		events[i] = testEvent(t, "boot-"+string(rune('a'+i%26))+string(rune('0'+i/26)), "0 3 * * *", false)
	}
	s := newTestScheduler(t, config.EventsConfig{Events: events})

	startScheduler(t, s)

	// No synchronization on purpose: land somewhere inside (or before, or
	// after) Start's scheduling loop.
	s.Reload(config.EventsConfig{
		Events: []config.EventConfig{testEvent(t, "reloaded", "0 5 * * *", false)},
	})

	deadline := time.Now().Add(2 * time.Second)
	for s.ScheduledCount() != 1 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := s.ScheduledCount(); got != 1 {
		t.Fatalf("entries = %d, want 1 (the reloaded schedule only)", got)
	}
}
