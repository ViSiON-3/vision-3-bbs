package scheduler

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

// chainEvent is an event that appends its id to a shared file, so a chain's
// ordering is observable.
func chainEvent(t *testing.T, id, runAfter, schedule string, delay int, out string) config.EventConfig {
	t.Helper()
	return config.EventConfig{
		ID:                id,
		Name:              id,
		Schedule:          schedule,
		RunAfter:          runAfter,
		DelayAfterSeconds: delay,
		Command:           "/bin/sh",
		Args:              []string{"-c", "echo " + id + " >> " + out},
		Enabled:           true,
		TimeoutSeconds:    10,
	}
}

// waitForContent polls path until it holds n lines, returning them.
func waitForContent(t *testing.T, path string, n int) []string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil {
			lines := strings.Fields(strings.TrimSpace(string(data)))
			if len(lines) >= n {
				return lines
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	data, _ := os.ReadFile(path)
	t.Fatalf("timed out waiting for %d lines in %s, have: %q", n, path, data)
	return nil
}

// startChain runs the scheduler and registers a cleanup that cancels it and
// waits for Start to return. Without the wait, t.TempDir's RemoveAll races a
// chained event still writing into the directory.
func startChain(t *testing.T, s *Scheduler) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.Start(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Error("scheduler did not stop within 10s")
		}
	})
}

func newChainScheduler(t *testing.T, events []config.EventConfig) *Scheduler {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("chain tests run shell commands; skipped on windows")
	}
	return NewScheduler(
		config.EventsConfig{MaxConcurrentEvents: 5, Events: events},
		filepath.Join(t.TempDir(), "history.json"),
	)
}

// run_after and delay_after_seconds shipped in EventConfig and in the events
// editor while nothing read either one: a sysop could set "Run After", see it
// saved, and get no chaining at all.
func TestRunAfterChainsEventsInOrder(t *testing.T) {
	out := filepath.Join(t.TempDir(), "order.txt")
	events := []config.EventConfig{
		chainEvent(t, "first", "", "", 0, out),
		chainEvent(t, "second", "first", "", 0, out),
		chainEvent(t, "third", "second", "", 0, out),
	}
	events[0].RunAtStartup = true // kick the chain off

	s := newChainScheduler(t, events)
	startChain(t, s)

	got := waitForContent(t, out, 3)
	want := []string{"first", "second", "third"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("chain ran %v, want %v", got, want)
		}
	}
}

// An event whose only trigger is run_after has no schedule by design. It used
// to be dropped at startup with "event has no schedule and run_at_startup is
// false", so the chain could never fire even once the field was read.
func TestChainedEventWithNoScheduleIsNotSkipped(t *testing.T) {
	out := filepath.Join(t.TempDir(), "order.txt")
	events := []config.EventConfig{
		chainEvent(t, "parent", "", "", 0, out),
		chainEvent(t, "child", "parent", "", 0, out),
	}
	events[0].RunAtStartup = true

	s := newChainScheduler(t, events)
	startChain(t, s)

	got := waitForContent(t, out, 2)
	if got[1] != "child" {
		t.Errorf("chained schedule-less event did not run: %v", got)
	}
}

// "Run after" is about order, not success: a poll that failed is exactly when
// the next network still wants its turn, and gating on success would strand a
// whole chain behind one bad exit.
func TestChainRunsAfterParentFailure(t *testing.T) {
	out := filepath.Join(t.TempDir(), "order.txt")
	failing := config.EventConfig{
		ID: "failer", Name: "failer", Command: "/bin/sh",
		Args: []string{"-c", "exit 3"}, Enabled: true, RunAtStartup: true, TimeoutSeconds: 10,
	}
	events := []config.EventConfig{failing, chainEvent(t, "after", "failer", "", 0, out)}

	s := newChainScheduler(t, events)
	startChain(t, s)

	got := waitForContent(t, out, 1)
	if got[0] != "after" {
		t.Errorf("chained event should run despite the parent failing, got %v", got)
	}
}

// Several events may follow one parent; all of them run.
func TestChainFansOut(t *testing.T) {
	out := filepath.Join(t.TempDir(), "order.txt")
	events := []config.EventConfig{
		chainEvent(t, "root", "", "", 0, out),
		chainEvent(t, "leafA", "root", "", 0, out),
		chainEvent(t, "leafB", "root", "", 0, out),
	}
	events[0].RunAtStartup = true

	s := newChainScheduler(t, events)
	startChain(t, s)

	got := waitForContent(t, out, 3)
	joined := strings.Join(got, " ")
	for _, want := range []string{"root", "leafA", "leafB"} {
		if !strings.Contains(joined, want) {
			t.Errorf("%q missing from %v", want, got)
		}
	}
}

// A cycle would re-trigger forever, each hop spawning a goroutine, so it is
// refused before anything runs — and refused wholesale, so behaviour matches
// the error rather than chaining half the graph.
func TestChainCycleDetected(t *testing.T) {
	for _, tc := range []struct {
		name   string
		events []config.EventConfig
		cyclic bool
	}{
		{"two-event loop", []config.EventConfig{
			{ID: "a", RunAfter: "b"}, {ID: "b", RunAfter: "a"},
		}, true},
		{"three-event loop", []config.EventConfig{
			{ID: "a", RunAfter: "c"}, {ID: "b", RunAfter: "a"}, {ID: "c", RunAfter: "b"},
		}, true},
		{"self reference", []config.EventConfig{{ID: "a", RunAfter: "a"}}, true},
		{"linear chain", []config.EventConfig{
			{ID: "a"}, {ID: "b", RunAfter: "a"}, {ID: "c", RunAfter: "b"},
		}, false},
		{"fan out", []config.EventConfig{
			{ID: "a"}, {ID: "b", RunAfter: "a"}, {ID: "c", RunAfter: "a"},
		}, false},
		{"dangling reference", []config.EventConfig{{ID: "a", RunAfter: "nope"}}, false},
		{"no chaining", []config.EventConfig{{ID: "a"}, {ID: "b"}}, false},
		// A disabled event is never chained, so a loop through one cannot
		// re-trigger and must not switch chaining off for everything else.
		{"loop broken by a disabled event", []config.EventConfig{
			{ID: "a", RunAfter: "b"}, {ID: "b", RunAfter: "a", Enabled: false, Name: "disabled"},
		}, false},
	} {
		for i := range tc.events {
			if tc.events[i].Name != "disabled" {
				tc.events[i].Enabled = true
			}
		}
		cycle := chainCycle(tc.events)
		if tc.cyclic && cycle == "" {
			t.Errorf("%s: expected a cycle to be reported", tc.name)
		}
		if !tc.cyclic && cycle != "" {
			t.Errorf("%s: unexpected cycle %q", tc.name, cycle)
		}
	}
}

// A cycle disables chaining for every event, so one bad link cannot leave the
// scheduler quietly running part of a graph it warned about.
func TestCycleDisablesChainingEntirely(t *testing.T) {
	s := newChainScheduler(t, []config.EventConfig{
		{ID: "a", RunAfter: "b", Enabled: true},
		{ID: "b", RunAfter: "a", Enabled: true},
	})
	if s.chainingOK {
		t.Error("chaining must be disabled when run_after contains a cycle")
	}
}

// A disabled event is not chained, matching the fact that it is not scheduled.
func TestChainedAfterSkipsDisabled(t *testing.T) {
	events := []config.EventConfig{
		{ID: "root", Enabled: true},
		{ID: "on", RunAfter: "root", Enabled: true},
		{ID: "off", RunAfter: "root", Enabled: false},
	}
	got := chainedAfter(events, "root")
	if len(got) != 1 || got[0].ID != "on" {
		t.Errorf("chainedAfter = %v, want just the enabled child", got)
	}
}

// delay_after_seconds holds the child back rather than being ignored.
func TestDelayAfterSecondsIsHonoured(t *testing.T) {
	out := filepath.Join(t.TempDir(), "order.txt")
	events := []config.EventConfig{
		chainEvent(t, "parent", "", "", 0, out),
		chainEvent(t, "child", "parent", "", 1, out),
	}
	events[0].RunAtStartup = true

	s := newChainScheduler(t, events)
	startChain(t, s)

	waitForContent(t, out, 1) // parent
	start := time.Now()
	waitForContent(t, out, 2) // child, after its delay
	if elapsed := time.Since(start); elapsed < 700*time.Millisecond {
		t.Errorf("child ran after %v, want it held for ~1s", elapsed)
	}
}

// Reload must re-validate: a cycle can be introduced, or fixed, by an edit.
func TestReloadRevalidatesChains(t *testing.T) {
	s := newChainScheduler(t, []config.EventConfig{
		{ID: "a", Enabled: true}, {ID: "b", RunAfter: "a", Enabled: true},
	})
	if !s.chainingOK {
		t.Fatal("acyclic config should allow chaining")
	}

	s.Reload(config.EventsConfig{MaxConcurrentEvents: 5, Events: []config.EventConfig{
		{ID: "a", RunAfter: "b", Enabled: true}, {ID: "b", RunAfter: "a", Enabled: true},
	}})
	if s.chainingOK {
		t.Error("a reload introducing a cycle must disable chaining")
	}

	s.Reload(config.EventsConfig{MaxConcurrentEvents: 5, Events: []config.EventConfig{
		{ID: "a", Enabled: true}, {ID: "b", RunAfter: "a", Enabled: true},
	}})
	if !s.chainingOK {
		t.Error("a reload fixing the cycle must re-enable chaining")
	}
}

// The parent must give its concurrency slot back before its children ask for
// one. With max_concurrent_events at 1 — a common setting for a board that
// polls several networks over one line — a chain otherwise stopped dead after
// the parent, every child refused with "max concurrent events reached".
func TestChainRunsWithSingleConcurrencySlot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chain tests run shell commands; skipped on windows")
	}
	out := filepath.Join(t.TempDir(), "order.txt")
	events := []config.EventConfig{
		chainEvent(t, "first", "", "", 0, out),
		chainEvent(t, "second", "first", "", 0, out),
		chainEvent(t, "third", "second", "", 0, out),
	}
	events[0].RunAtStartup = true

	s := NewScheduler(
		config.EventsConfig{MaxConcurrentEvents: 1, Events: events},
		filepath.Join(t.TempDir(), "history.json"),
	)
	startChain(t, s)

	got := waitForContent(t, out, 3)
	want := []string{"first", "second", "third"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("chain ran %v, want %v", got, want)
		}
	}
}

// A job still running on a cron that Reload retired is not covered by the
// current cron's stop context. Stop has to wait for it anyway, or the job can
// finish after the chain drain and history save: its run is then missing from
// the saved history, and anything chained to it would start into a scheduler
// that has already shut down.
func TestStopWaitsForRetiredCronJobs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chain tests run shell commands; skipped on windows")
	}
	historyPath := filepath.Join(t.TempDir(), "history.json")
	// Run sleep directly rather than via "sh -c": a shell that forks the
	// sleep is what gets killed on cancellation, and the orphaned sleep then
	// holds the output pipes open until it exits on its own, which on Linux
	// stalled this test past its deadline.
	slow := config.EventConfig{
		ID: "slow", Name: "slow", Schedule: "@every 1s", Command: "/bin/sleep",
		Args: []string{"30"}, Enabled: true, TimeoutSeconds: 60,
	}
	s := NewScheduler(config.EventsConfig{MaxConcurrentEvents: 2, Events: []config.EventConfig{slow}},
		historyPath)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.Start(ctx)
	}()

	// Wait for the job to be mid-run, then retire its cron with an empty reload.
	deadline := time.Now().Add(5 * time.Second)
	for {
		s.mu.RLock()
		running := s.runningEvents["slow"]
		s.mu.RUnlock()
		if running {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("slow event never started")
		}
		time.Sleep(10 * time.Millisecond)
	}
	s.Reload(config.EventsConfig{MaxConcurrentEvents: 2})

	cancel()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("scheduler did not stop")
	}
	// The job is killed by the cancellation, and its (failed) run must have
	// been recorded before Stop saved the history — which it is only if Stop
	// waited for the retired cron's job to return.
	history, err := LoadHistory(historyPath)
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	if _, ok := history["slow"]; !ok {
		t.Errorf("retired cron's job missing from saved history %v: Stop did not wait for it", history)
	}
}

// A cron installed by Reload before Start ran its jobs under
// context.Background(), so a chain it started could not be cancelled: Stop
// then blocked on chainWg for the child's whole delay_after_seconds. The
// pre-start context is now the scheduler's own, and Stop cancels it.
func TestStopCancelsChainStartedBeforeStart(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chain tests run shell commands; skipped on windows")
	}
	out := filepath.Join(t.TempDir(), "order.txt")
	parent := chainEvent(t, "parent", "", "@every 1s", 0, out)
	child := chainEvent(t, "child", "parent", "", 30, out) // long delay
	s := NewScheduler(config.EventsConfig{MaxConcurrentEvents: 2},
		filepath.Join(t.TempDir(), "history.json"))

	// Reload before Start installs and starts a cron on the pre-start context.
	s.Reload(config.EventsConfig{MaxConcurrentEvents: 2, Events: []config.EventConfig{parent, child}})
	waitForContent(t, out, 1) // parent has run; child is waiting out its delay

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.Start(ctx)
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop blocked on a chain started before Start; its delay was not cancelled")
	}
}
