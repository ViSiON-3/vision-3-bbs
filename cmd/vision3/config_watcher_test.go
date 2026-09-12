package main

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/conference"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/menu"
	"github.com/ViSiON-3/vision-3-bbs/internal/scheduler"
)

// recorder counts reload invocations per target name.
type recorder struct {
	mu     sync.Mutex
	counts map[string]int
}

func newRecorder() *recorder { return &recorder{counts: make(map[string]int)} }

func (r *recorder) hit(name string) func() {
	return func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.counts[name]++
	}
}

func (r *recorder) count(name string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.counts[name]
}

// newTestWatcher builds a ConfigWatcher over a temp dir with the given file
// names as targets, wired to a recorder instead of real reload actions. It
// does not start the poll loop; tests drive poll() directly.
func newTestWatcher(t *testing.T, names ...string) (*ConfigWatcher, string, *recorder) {
	t.Helper()
	dir := t.TempDir()
	rec := newRecorder()

	cw := &ConfigWatcher{
		rootConfigPath: dir,
		interval:       defaultPollInterval,
		sentinelPath:   config.ReloadSentinelPath(dir),
		mtimes:         make(map[string]time.Time),
		stop:           make(chan struct{}),
		done:           make(chan struct{}),
	}
	for _, n := range names {
		path := filepath.Join(dir, n)
		if err := os.WriteFile(path, []byte(`{}`), 0644); err != nil {
			t.Fatalf("seed %s: %v", n, err)
		}
		cw.targets = append(cw.targets, reloadTarget{name: n, path: path, reload: rec.hit(n)})
	}
	cw.seed()
	return cw, dir, rec
}

// touch rewrites a file with a modification time distinct from its current one.
// Timestamps are set explicitly rather than relying on wall-clock movement, so
// the test does not depend on filesystem timestamp granularity.
func touch(t *testing.T, path string, age time.Duration) {
	t.Helper()
	if err := os.WriteFile(path, []byte(`{"changed":true}`), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	ts := time.Now().Add(age)
	if err := os.Chtimes(path, ts, ts); err != nil {
		t.Fatalf("chtimes %s: %v", path, err)
	}
}

// TestPollReloadsEveryFileInABurst is the regression test for issue #320: a
// tool that rewrites several config files in one save must reload all of them,
// not just the last one written. The previous event-driven watcher debounced
// by replacing the pending filename, so a burst collapsed to a single reload.
func TestPollReloadsEveryFileInABurst(t *testing.T) {
	names := []string{"config.json", "doors.json", "login.json", "strings.json"}
	cw, dir, rec := newTestWatcher(t, names...)

	// Simulate v3config's saveAll: every file rewritten back to back.
	for _, n := range names {
		touch(t, filepath.Join(dir, n), time.Second)
	}

	cw.poll()

	for _, n := range names {
		if got := rec.count(n); got != 1 {
			t.Errorf("%s reloaded %d times, want 1", n, got)
		}
	}
}

func TestPollIgnoresUnchangedFiles(t *testing.T) {
	cw, dir, rec := newTestWatcher(t, "config.json", "doors.json")

	touch(t, filepath.Join(dir, "doors.json"), time.Second)
	cw.poll()

	if got := rec.count("doors.json"); got != 1 {
		t.Errorf("doors.json reloaded %d times, want 1", got)
	}
	if got := rec.count("config.json"); got != 0 {
		t.Errorf("config.json reloaded %d times, want 0 (untouched)", got)
	}

	// A second poll with nothing further changed must be a no-op.
	cw.poll()
	if got := rec.count("doors.json"); got != 1 {
		t.Errorf("doors.json reloaded %d times across two polls, want 1", got)
	}
}

func TestSentinelReloadsEverythingOnce(t *testing.T) {
	names := []string{"config.json", "doors.json", "login.json"}
	cw, dir, rec := newTestWatcher(t, names...)

	// Only one file actually changed, but the sentinel says "re-read it all".
	touch(t, filepath.Join(dir, "doors.json"), time.Second)
	if err := config.TouchReloadSentinel(dir); err != nil {
		t.Fatalf("touch sentinel: %v", err)
	}

	cw.poll()
	for _, n := range names {
		if got := rec.count(n); got != 1 {
			t.Errorf("%s reloaded %d times after sentinel, want 1", n, got)
		}
	}

	// The changed file must not reload again on the next poll: the sentinel
	// pass already accounted for its new timestamp.
	cw.poll()
	for _, n := range names {
		if got := rec.count(n); got != 1 {
			t.Errorf("%s reloaded %d times after follow-up poll, want 1", n, got)
		}
	}
}

// TestPollDetectsBackwardTimestamp covers restoring a config file from a
// backup, which can move its modification time earlier rather than later.
func TestPollDetectsBackwardTimestamp(t *testing.T) {
	cw, dir, rec := newTestWatcher(t, "config.json")

	touch(t, filepath.Join(dir, "config.json"), -1*time.Hour)
	cw.poll()

	if got := rec.count("config.json"); got != 1 {
		t.Errorf("config.json reloaded %d times, want 1", got)
	}
}

// TestPollRetriesAfterFailedRead covers the self-healing property: a file read
// mid-write parses badly and its reload is skipped, but the writer's final
// timestamp differs from the one recorded for the failed attempt, so the next
// poll picks it up. Modelled here as two successive distinct timestamps.
func TestPollRetriesAfterFailedRead(t *testing.T) {
	cw, dir, rec := newTestWatcher(t, "config.json")
	path := filepath.Join(dir, "config.json")

	touch(t, path, time.Second) // partial write
	cw.poll()
	touch(t, path, 2*time.Second) // writer finishes
	cw.poll()

	if got := rec.count("config.json"); got != 2 {
		t.Errorf("config.json reloaded %d times, want 2 (initial + retry)", got)
	}
}

func TestPollHandlesMissingAndRecreatedFile(t *testing.T) {
	cw, dir, rec := newTestWatcher(t, "doors.json")
	path := filepath.Join(dir, "doors.json")

	if err := os.Remove(path); err != nil {
		t.Fatalf("remove: %v", err)
	}
	cw.poll()
	if got := rec.count("doors.json"); got != 0 {
		t.Errorf("deleting a config reloaded it %d times, want 0", got)
	}

	touch(t, path, time.Second)
	cw.poll()
	if got := rec.count("doors.json"); got != 1 {
		t.Errorf("recreated doors.json reloaded %d times, want 1", got)
	}
}

// TestLivePollLoopPicksUpABurst exercises the real running poll loop end to
// end (not poll() called directly): start the loop, rewrite several files the
// way v3config does, and confirm every one of them reloads.
func TestLivePollLoopPicksUpABurst(t *testing.T) {
	dir := t.TempDir()
	rec := newRecorder()

	cw := &ConfigWatcher{
		rootConfigPath: dir,
		interval:       20 * time.Millisecond, // keep the test quick
		sentinelPath:   config.ReloadSentinelPath(dir),
		mtimes:         make(map[string]time.Time),
		stop:           make(chan struct{}),
		done:           make(chan struct{}),
	}
	names := []string{"config.json", "doors.json", "login.json", "strings.json", "theme.json"}
	for _, n := range names {
		path := filepath.Join(dir, n)
		if err := os.WriteFile(path, []byte(`{}`), 0644); err != nil {
			t.Fatal(err)
		}
		cw.targets = append(cw.targets, reloadTarget{name: n, path: path, reload: rec.hit(n)})
	}
	cw.seed()

	go cw.pollLoop()
	defer cw.Stop()

	// Rewrite all five back to back, as saveAll does.
	var wg sync.WaitGroup
	for _, n := range names {
		wg.Add(1)
		go func(n string) {
			defer wg.Done()
			touch(t, filepath.Join(dir, n), time.Second)
		}(n)
	}
	wg.Wait()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		done := true
		for _, n := range names {
			if rec.count(n) < 1 {
				done = false
			}
		}
		if done {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	for _, n := range names {
		if got := rec.count(n); got != 1 {
			t.Errorf("%s reloaded %d times, want exactly 1", n, got)
		}
	}
}

func TestStopIsIdempotent(t *testing.T) {
	cw, _, _ := newTestWatcher(t, "config.json")
	go cw.pollLoop()

	cw.Stop()
	cw.Stop() // must not panic or block
}

// TestReloadProtocolsUpdatesExecutor covers the protocols.json reload path
// end to end: file on disk → LoadProtocols → MenuExecutor.SetProtocols.
func TestReloadProtocolsUpdatesExecutor(t *testing.T) {
	dir := t.TempDir()
	write := func(body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, "protocols.json"), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write(`[{"name":"ZModem","enabled":true}]`)

	e := &menu.MenuExecutor{}
	cw := &ConfigWatcher{rootConfigPath: dir, menuExecutor: e}

	cw.reloadProtocols()
	if got := e.Protocols(); len(got) != 1 || got[0].Name != "ZModem" {
		t.Fatalf("Protocols() = %+v, want the one loaded entry", got)
	}

	write(`[{"name":"ZModem","enabled":true},{"name":"YModem","enabled":true}]`)
	cw.reloadProtocols()
	if got := e.Protocols(); len(got) != 2 {
		t.Errorf("Protocols() has %d entries after reload, want 2", len(got))
	}

	// A malformed file must leave the last good config in place.
	write(`{not json`)
	cw.reloadProtocols()
	if got := e.Protocols(); len(got) != 2 {
		t.Errorf("Protocols() has %d entries after a bad reload, want 2 (unchanged)", len(got))
	}
}

// TestReloadEventsWithoutScheduler covers the watcher noticing events.json
// before main has wired the scheduler in (or when it never starts): it must
// log and return, not panic.
func TestReloadEventsWithoutScheduler(t *testing.T) {
	cw := &ConfigWatcher{rootConfigPath: t.TempDir()}
	cw.reloadEvents() // must not panic
}

// TestReloadEventsReschedules covers the events.json reload path end to end:
// file on disk → LoadEventsConfig → Scheduler.Reload.
func TestReloadEventsReschedules(t *testing.T) {
	dir := t.TempDir()
	write := func(body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, "events.json"), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write(`{"events":[{"id":"nightly","name":"nightly","schedule":"0 3 * * *","command":"true","enabled":true}]}`)

	sched := scheduler.NewScheduler(config.EventsConfig{}, filepath.Join(dir, "history.json"))
	cw := &ConfigWatcher{rootConfigPath: dir}
	cw.SetScheduler(sched)

	cw.reloadEvents()
	if got := sched.ScheduledCount(); got != 1 {
		t.Fatalf("scheduled = %d after reload, want 1", got)
	}

	write(`{"events":[]}`)
	cw.reloadEvents()
	if got := sched.ScheduledCount(); got != 0 {
		t.Errorf("scheduled = %d after emptying events.json, want 0", got)
	}
}

// TestReloadConferencesWithoutManager covers a BBS whose conference manager
// never started (conferences.json failed at boot): the reload must log and
// return, not panic.
func TestReloadConferencesWithoutManager(t *testing.T) {
	cw := &ConfigWatcher{rootConfigPath: t.TempDir(), menuExecutor: &menu.MenuExecutor{}}
	cw.reloadConferences() // must not panic
}

// TestReloadConferencesUpdatesManager covers the conferences.json reload path
// end to end: file on disk → ConferenceManager.Reload → new definitions live.
func TestReloadConferencesUpdatesManager(t *testing.T) {
	dir := t.TempDir()
	write := func(body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, "conferences.json"), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write(`[{"id":1,"tag":"GEN","name":"General"}]`)

	confMgr, err := conference.NewConferenceManager(dir)
	if err != nil {
		t.Fatal(err)
	}
	e := &menu.MenuExecutor{ConferenceMgr: confMgr}
	cw := &ConfigWatcher{rootConfigPath: dir, menuExecutor: e}

	write(`[{"id":1,"tag":"GEN","name":"General"},{"id":2,"tag":"TECH","name":"Tech"}]`)
	cw.reloadConferences()

	if _, ok := confMgr.GetByTag("TECH"); !ok {
		t.Error("conference TECH missing after reload")
	}
}

// TestPollReloadsOnRemovalWhenAsked covers the overlay theme.json: a file that
// is absent at startup, appears, and is removed again must reload on both
// transitions, because its absence means "use the shipped copy". Ordinary
// targets keep ignoring removal (TestPollHandlesMissingAndRecreatedFile).
func TestPollReloadsOnRemovalWhenAsked(t *testing.T) {
	cw, dir, rec := newTestWatcher(t)
	path := filepath.Join(dir, "theme.json")
	cw.targets = append(cw.targets, reloadTarget{name: "theme.json", path: path, reload: rec.hit("theme.json"), reloadOnRemove: true})
	cw.seed()

	cw.poll() // still absent: nothing to do
	if got := rec.count("theme.json"); got != 0 {
		t.Fatalf("absent file reloaded %d times, want 0", got)
	}

	touch(t, path, time.Second)
	cw.poll()
	if got := rec.count("theme.json"); got != 1 {
		t.Errorf("created file reloaded %d times, want 1", got)
	}

	if err := os.Remove(path); err != nil {
		t.Fatalf("remove: %v", err)
	}
	cw.poll()
	if got := rec.count("theme.json"); got != 2 {
		t.Errorf("removed file reloaded %d times, want 2", got)
	}
	cw.poll() // still absent: no repeat
	if got := rec.count("theme.json"); got != 2 {
		t.Errorf("still-absent file reloaded %d times, want 2", got)
	}
}

// TestWatcherWatchesOverlayTheme checks the constructor registers theme.json
// in both the shipped set and its overlay.
func TestWatcherWatchesOverlayTheme(t *testing.T) {
	root := t.TempDir()
	menuSet := filepath.Join(root, "menus", "v3")
	configs := filepath.Join(root, "configs")
	if err := os.MkdirAll(configs, 0755); err != nil {
		t.Fatal(err)
	}
	cw, err := NewConfigWatcher(configs, menuSet, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("NewConfigWatcher: %v", err)
	}
	defer cw.Stop()
	want := map[string]bool{
		filepath.Join(menuSet, "theme.json"):               false,
		filepath.Join(root, "menus.d", "v3", "theme.json"): true,
	}
	for _, tg := range cw.targets {
		if reloadOnRemove, ok := want[tg.path]; ok {
			if tg.reloadOnRemove != reloadOnRemove {
				t.Errorf("%s reloadOnRemove = %v, want %v", tg.path, tg.reloadOnRemove, reloadOnRemove)
			}
			delete(want, tg.path)
		}
	}
	for p := range want {
		t.Errorf("theme target not registered: %s", p)
	}
}

func TestWatcherStatErrorDoesNotCountAsRemoval(t *testing.T) {
	cw, dir, _ := newTestWatcher(t, "theme.json")
	path := filepath.Join(dir, "theme.json")
	before := cw.mtimes[path]
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("theme.json", path); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := os.Stat(path); err == nil || os.IsNotExist(err) {
		t.Fatalf("expected non-missing stat error: %v", err)
	}
	if cw.changed(path, true) {
		t.Error("stat failure triggered removal reload")
	}
	if got := cw.mtimes[path]; !got.Equal(before) {
		t.Errorf("stat failure discarded mtime: %v, want %v", got, before)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if !cw.changed(path, true) {
		t.Error("actual removal not reported after stat failure")
	}
}
