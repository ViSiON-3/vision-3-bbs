package mailer

import (
	"context"
	"log/slog"
	"os/exec"
	"syscall"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ftn"
)

// termGrace is how long binkd gets after SIGTERM before being killed.
const termGrace = 5 * time.Second

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
	cmd.Stderr = nil

	if err := cmd.Start(); err != nil {
		return err
	}
	slog.Info("binkd mailer started", "pid", cmd.Process.Pid, "port", s.cfg.FTN.Binkd.Port)

	waitErr := make(chan error, 1)
	go func() { waitErr <- cmd.Wait() }()

	select {
	case err := <-waitErr:
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
