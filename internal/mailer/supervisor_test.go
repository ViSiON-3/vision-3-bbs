package mailer

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

// newSupervisedService builds a service whose fake binkd writes its PID to
// pidFile and then sleeps (or exits immediately when crash is true).
func newSupervisedService(t *testing.T, crash bool) (*Service, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("supervisor tests use shell scripts; skipped on windows")
	}
	root := newTestRoot(t)
	pidFile := filepath.Join(root, "binkd.pid")
	script := "#!/bin/sh\necho $$ >> " + pidFile + "\n"
	if !crash {
		// Exit on SIGTERM the way real binkd does ("got signal #15 ...
		// quitting"), so a stop costs a moment rather than the full termGrace
		// spent escalating to SIGKILL. sleep is backgrounded because a shell
		// runs no trap while a foreground child is running.
		// The backgrounded sleep must be reaped too: it inherits stderr, and
		// a surviving holder of that pipe makes cmd.Wait block for WaitDelay
		// even after the shell itself has exited.
		script += "trap 'kill $p 2>/dev/null; exit 0' TERM\nsleep 60 & p=$!\nwait\n"
	}
	if err := os.WriteFile(filepath.Join(root, "bin", "binkd"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	cfg := testFTNConfig()
	cfg.Networks = map[string]config.FTNNetworkConfig{
		"fsxnet": {OwnAddress: "21:4/158"}, // tosser disabled: export loop idles
	}
	svc, err := New(Config{BBSRoot: root, FTN: cfg})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	svc.backoffMin = 20 * time.Millisecond
	svc.backoffMax = 100 * time.Millisecond
	svc.confWatch = 20 * time.Millisecond // the real 15s would outrun every test
	return svc, pidFile
}

// waitForLines polls pidFile until it has at least n lines or times out.
func waitForLines(t *testing.T, pidFile string, n int) []byte {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(pidFile)
		if err == nil {
			lines := 0
			for _, c := range data {
				if c == '\n' {
					lines++
				}
			}
			if lines >= n {
				return data
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d starts in %s", n, pidFile)
	return nil
}

func TestSupervisorStartsAndStops(t *testing.T) {
	svc, pidFile := newSupervisedService(t, false)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { svc.Start(ctx); close(done) }()

	waitForLines(t, pidFile, 1)
	cancel()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Start did not return after context cancel")
	}
	if err := svc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestRunOnceReportsStderrOnCrash(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("supervisor tests use shell scripts; skipped on windows")
	}
	root := newTestRoot(t)
	script := "#!/bin/sh\necho 'config error: boom' >&2\nexit 1\n"
	if err := os.WriteFile(filepath.Join(root, "bin", "binkd"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	cfg := testFTNConfig()
	cfg.Networks = map[string]config.FTNNetworkConfig{
		"fsxnet": {OwnAddress: "21:4/158"},
	}
	svc, err := New(Config{BBSRoot: root, FTN: cfg})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	runErr := svc.runOnce(context.Background())
	if runErr == nil {
		t.Fatal("expected error from crashing binkd")
	}
	if !strings.Contains(runErr.Error(), "config error: boom") {
		t.Fatalf("error must include binkd's stderr, got: %v", runErr)
	}
}

func TestStderrTailKeepsOnlyLastBytes(t *testing.T) {
	tail := &stderrTail{}
	// One oversized write followed by a small one: only the newest
	// stderrTailCap bytes may survive, ending with the small write.
	big := strings.Repeat("x", stderrTailCap*3) + "MARKER-A"
	for _, chunk := range []string{big, "MARKER-B"} {
		if _, err := tail.Write([]byte(chunk)); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}
	got := tail.String()
	if len(got) > stderrTailCap {
		t.Fatalf("tail length %d exceeds cap %d", len(got), stderrTailCap)
	}
	if !strings.HasSuffix(got, "MARKER-A"+"MARKER-B") {
		t.Fatalf("tail must end with the most recent writes, got tail of %q", got[len(got)-40:])
	}
	if len(tail.buf) > stderrTailCap || cap(tail.buf) > 2*stderrTailCap {
		t.Fatalf("backing storage unbounded: len=%d cap=%d", len(tail.buf), cap(tail.buf))
	}
}

func TestSuperviseLoopCreatesRuntimeDirs(t *testing.T) {
	svc, pidFile := newSupervisedService(t, true)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { svc.Start(ctx); close(done) }()

	waitForLines(t, pidFile, 1)
	cancel()
	<-done

	for _, d := range []string{
		filepath.Join(svc.cfg.BBSRoot, "data", "logs"),
		filepath.Join(svc.cfg.BBSRoot, "data", "ftn", "in"),
		filepath.Join(svc.cfg.BBSRoot, "data", "ftn", "secure_in"),
		filepath.Join(svc.cfg.BBSRoot, "data", "ftn", "out"),
	} {
		info, err := os.Stat(d)
		if err != nil || !info.IsDir() {
			t.Errorf("runtime dir %s must exist before binkd launch: %v", d, err)
		}
	}
}

func TestSupervisorRestartsAfterCrash(t *testing.T) {
	svc, pidFile := newSupervisedService(t, true) // script exits immediately
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { svc.Start(ctx); close(done) }()

	// The crashing script should be relaunched at least 3 times.
	waitForLines(t, pidFile, 3)
	cancel()
	<-done
}

// binkd reads its configuration once, at startup, and nothing asked it to
// re-read: the supervisor synced binkd.conf only just before a launch, and the
// only other signal was a shutdown SIGTERM. A new node, a changed hub hostname
// or a different listen port therefore sat inert until binkd happened to exit —
// on a healthy system, potentially for weeks — while the sysop looked at a
// correct config file and a running process that disagreed with it.
func TestBinkdRecycledWhenConfChanges(t *testing.T) {
	svc, pidFile := newSupervisedService(t, false)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go svc.Start(ctx)

	first := waitForLines(t, pidFile, 1)

	// Change binkd.conf the way a config-editor save would.
	conf, err := os.ReadFile(svc.confPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(svc.confPath, append(conf, "\nnode 21:4/158@fsxnet hub.example.org:24554 secret\n"...), 0600); err != nil {
		t.Fatal(err)
	}

	// The watcher should notice and relaunch binkd on the new file.
	second := waitForLines(t, pidFile, 2)
	if len(second) <= len(first) {
		t.Fatalf("binkd was not recycled after binkd.conf changed:\n%s", second)
	}

	cancel()
	_ = svc.Close()
}

// A recycle is deliberate, so it must not be reported or paced as a crash:
// the relaunch is immediate and the crash backoff is left untouched, or a
// config change could mask a genuine crash loop.
func TestRecycleDoesNotDisturbCrashBackoff(t *testing.T) {
	svc, pidFile := newSupervisedService(t, false)
	// Long enough that a relaunch paced by the crash backoff would be
	// unmistakable against the immediate relaunch a recycle must get.
	svc.backoffMin = 2 * time.Second
	svc.backoffMax = 2 * time.Second

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go svc.Start(ctx)
	waitForLines(t, pidFile, 1)

	conf, err := os.ReadFile(svc.confPath)
	if err != nil {
		t.Fatal(err)
	}
	changed := time.Now()
	if err := os.WriteFile(svc.confPath, append(conf, "\nnode 21:4/158@fsxnet hub.example.org:24554 secret\n"...), 0600); err != nil {
		t.Fatal(err)
	}

	waitForLines(t, pidFile, 2)
	if took := time.Since(changed); took >= svc.backoffMin {
		t.Errorf("relaunch after a recycle took %v, at least the crash backoff of %v: the recycle was paced as a crash",
			took, svc.backoffMin)
	}
	cancel()
	_ = svc.Close()
}

// One receive-only link (no hostname) on an unchanged configuration produced
// the same warning on every watcher tick — 5,760 a day at the real interval —
// burying anything new. The watcher must sync, and so warn, only when
// ftn.json or binkd.conf has actually changed.
func TestWatcherDoesNotResyncAnUnchangedConf(t *testing.T) {
	svc, pidFile := newSupervisedService(t, false)

	var syncs atomic.Int32
	svc.syncHook = func() { syncs.Add(1) }

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go svc.Start(ctx)
	waitForLines(t, pidFile, 1)

	// Let the watcher settle (its first tick syncs once to establish its
	// baseline), then count over many ticks with nothing changing.
	time.Sleep(100 * time.Millisecond)
	base := syncs.Load()
	time.Sleep(300 * time.Millisecond)
	if n := syncs.Load() - base; n != 0 {
		t.Errorf("watcher synced %d times with nothing changed, want 0", n)
	}

	// A hand edit to binkd.conf is still noticed and re-synced.
	conf, err := os.ReadFile(svc.confPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(svc.confPath, append(conf, "\n# hand edit\n"...), 0600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for syncs.Load() == base && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if syncs.Load() == base {
		t.Error("a changed binkd.conf must still trigger a sync")
	}
	cancel()
	_ = svc.Close()
}

// An unchanged binkd.conf must not recycle binkd, or the watcher restarts the
// mailer every tick and no session ever completes.
func TestUnchangedConfDoesNotRecycle(t *testing.T) {
	svc, pidFile := newSupervisedService(t, false)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go svc.Start(ctx)
	waitForLines(t, pidFile, 1)

	// Longer than several watch ticks would be if the interval were short;
	// with a stable file the count must stay at one.
	time.Sleep(300 * time.Millisecond)

	data, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(data), "\n"); n != 1 {
		t.Errorf("binkd started %d times with an unchanged conf, want 1:\n%s", n, data)
	}
	cancel()
	_ = svc.Close()
}

// The watcher runs every few seconds. Re-reading ftn.json on each tick meant
// LoadFTNConfig logged an Info line per tosser-enabled network every time,
// burying the log in ~19 lines a minute that said nothing had happened — the
// same trap reloadedExportInterval already guards against with its own
// mod-time check.
func TestFTNChangedOnlyReportsRealChanges(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ftn.json")
	if err := os.WriteFile(path, []byte(`{"networks":{}}`), 0644); err != nil {
		t.Fatal(err)
	}
	s := &Service{configDir: dir}

	if !s.ftnChanged() {
		t.Fatal("first check must report a change so the watcher picks up the current file")
	}
	if s.ftnChanged() {
		t.Error("an unchanged ftn.json must not report a change")
	}
	if s.ftnChanged() {
		t.Error("still unchanged on a third tick")
	}

	// A save bumps the mod-time; the watcher must notice exactly once.
	later := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
	if !s.ftnChanged() {
		t.Error("a modified ftn.json must report a change")
	}
	if s.ftnChanged() {
		t.Error("the same change must not be reported twice")
	}
}

// No config dir means hot-reload is disabled; the watcher must not stat or
// reload anything.
func TestFTNChangedWithoutConfigDir(t *testing.T) {
	s := &Service{}
	if s.ftnChanged() {
		t.Error("with no config dir there is nothing to reload")
	}
}
