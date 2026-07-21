package mailer

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
		script += "sleep 60\n"
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
