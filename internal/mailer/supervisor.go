package mailer

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ftn"
)

// termGrace is how long binkd gets after SIGTERM before being killed.
const termGrace = 5 * time.Second

// stderrTailCap bounds how much of binkd's stderr is retained for error
// reporting; only the most recent bytes are kept.
const stderrTailCap = 2048

// stderrTail is an io.Writer that keeps only the last stderrTailCap bytes
// written, so a crashing binkd's final stderr output can be logged without
// unbounded buffering.
type stderrTail struct {
	mu  sync.Mutex
	buf []byte
}

func (t *stderrTail) Write(p []byte) (int, error) {
	n := len(p)
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(p) > stderrTailCap {
		p = p[len(p)-stderrTailCap:]
	}
	t.buf = append(t.buf, p...)
	if len(t.buf) > stderrTailCap {
		// Copy down in place so the backing array stays bounded too.
		t.buf = append(t.buf[:0], t.buf[len(t.buf)-stderrTailCap:]...)
	}
	return n, nil
}

func (t *stderrTail) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return strings.TrimSpace(string(t.buf))
}

// ensureRuntimeDirs creates the directories binkd needs at startup (log dir
// and inbound/outbound queues). binkd exits immediately if its log file's
// directory is missing, and nothing else in the launch path creates these.
func (s *Service) ensureRuntimeDirs() {
	for _, d := range []string{
		filepath.Join(s.cfg.BBSRoot, "data", "logs"),
		filepath.Join(s.cfg.BBSRoot, "data", "ftn", "in"),
		filepath.Join(s.cfg.BBSRoot, "data", "ftn", "secure_in"),
		filepath.Join(s.cfg.BBSRoot, "data", "ftn", "out"),
	} {
		if err := os.MkdirAll(d, 0755); err != nil {
			slog.Warn("creating binkd runtime dir failed", "dir", d, "error", err)
		}
	}
}

// superviseLoop keeps binkd running until ctx is cancelled, restarting with
// exponential backoff on unexpected exits.
func (s *Service) superviseLoop(ctx context.Context) {
	backoff := s.backoffMin

	for {
		if ctx.Err() != nil {
			return
		}

		// Sync dynamic settings into binkd.conf before each launch (best-effort).
		// b is the boot-time config snapshot: a config-editor port change made
		// mid-session is only re-applied here after the BBS restarts (the TUI
		// save path also syncs binkd.conf directly, for immediate effect).
		b := s.cfg.FTN.Binkd
		if err := ftn.SyncBinkdSettings(s.confPath, b.Port, b.LogLevel); err != nil {
			slog.Warn("binkd.conf settings sync failed", "error", err)
		}
		s.ensureRuntimeDirs()

		started := time.Now()
		err := s.runOnce(ctx)
		if ctx.Err() != nil {
			return // shutdown requested; exit regardless of process error
		}

		if time.Since(started) >= s.healthyRun {
			backoff = s.backoffMin // healthy run resets the backoff
		}
		slog.Error("binkd exited unexpectedly, restarting", "error", err, "backoff", backoff)

		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > s.backoffMax {
			backoff = s.backoffMax
		}
	}
}

// runOnce starts binkd and blocks until it exits or ctx is cancelled.
// On cancellation it sends SIGTERM, waits termGrace, then kills.
func (s *Service) runOnce(ctx context.Context) error {
	// No -D flag: binkd runs as a supervised child (not daemonized). The
	// BBS's signal-driven shutdown (SIGTERM, then a grace period, then
	// SIGKILL) is what stops it on exit; on Unix an orphaned child process
	// can otherwise outlive its parent.
	cmd := exec.Command(s.binkdPath, s.confPath)
	cmd.Stdout = nil // binkd logs to file per binkd.conf
	// Startup failures (config errors, unopenable log file, port in use) go
	// to stderr before binkd ever opens its log file, so keep a bounded tail
	// of it for the exit error.
	tail := &stderrTail{}
	cmd.Stderr = tail
	// The stderr pipe is inherited by any children binkd spawns (exec'd
	// tossers); WaitDelay stops Wait from blocking on the pipe after binkd
	// itself has exited.
	cmd.WaitDelay = termGrace

	if err := cmd.Start(); err != nil {
		return err
	}
	slog.Info("binkd mailer started", "pid", cmd.Process.Pid, "port", s.cfg.FTN.Binkd.Port)

	waitErr := make(chan error, 1)
	go func() { waitErr <- cmd.Wait() }()

	select {
	case err := <-waitErr:
		if err != nil {
			if msg := tail.String(); msg != "" {
				return fmt.Errorf("%w (stderr: %s)", err, msg)
			}
		}
		return err
	case <-ctx.Done():
		if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
			_ = cmd.Process.Kill() // SIGTERM unsupported (e.g. windows) or gone
		}
		select {
		case <-waitErr:
		case <-time.After(termGrace):
			_ = cmd.Process.Kill()
			<-waitErr
		}
		slog.Info("binkd mailer stopped")
		return nil
	}
}
