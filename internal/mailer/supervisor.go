package mailer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/ftn"
)

// termGrace is how long binkd gets after SIGTERM before being killed.
const termGrace = 5 * time.Second

// defaultConfWatch is how often binkd.conf is re-synced from ftn.json and
// compared against what the running binkd was started with. Short enough that
// a config-editor save takes effect while the sysop is still at the console,
// long enough to be nothing on an idle system: the check is a file read and a
// hash. Held on the Service so tests can shorten it.
const defaultConfWatch = 15 * time.Second

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
//
// The outbound is passed in rather than re-read so it comes from the same
// config snapshot as the settings sync that precedes it: a reload landing
// between the two would otherwise create one set of queues and point
// binkd.conf at another.
func (s *Service) ensureRuntimeDirs(outbound ftn.BinkdOutbound) {
	dirs := []string{
		filepath.Join(s.cfg.BBSRoot, "data", "logs"),
		filepath.Join(s.cfg.BBSRoot, "data", "ftn", "in"),
		filepath.Join(s.cfg.BBSRoot, "data", "ftn", "secure_in"),
	}
	// Every network's outbound, so a per-network queue exists before binkd
	// and the tosser reach for it.
	dirs = append(dirs, outbound.Dirs()...)
	for _, d := range dirs {
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

		// Pick up any config-editor save before each launch, then sync the
		// current settings into binkd.conf. Reloading here (rather than reusing
		// the boot snapshot) means the supervisor writes the sysop's latest
		// values instead of silently overwriting a newer binkd.conf with stale
		// ones. A port or loglevel change takes effect on this (re)launch;
		// while binkd is up, changes are applied the next time it respawns.
		s.reloadFTN()
		// One snapshot for the whole sync, so the port/loglevel and the outbound
		// dir cannot be drawn from two different reloads within one launch.
		snap := s.currentFTN()
		outbound := s.syncConf(snap)
		s.ensureRuntimeDirs(outbound)
		// Baseline for the watcher: binkd is about to read this file, so any
		// later difference is a change binkd has not seen.
		s.confSeen.Store(hashFile(s.confPath))

		started := time.Now()
		err := s.runOnce(ctx)
		if ctx.Err() != nil {
			return // shutdown requested; exit regardless of process error
		}

		if errors.Is(err, errRecycle) {
			// Deliberate stop for a config change: relaunch at once, and leave
			// the backoff alone so a recycle cannot mask a crash loop.
			continue
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

// binkdArgs is binkd's argv after the binary name. Flags must precede the
// positional config path, which binkd expects last.
func (s *Service) binkdArgs() []string {
	args := make([]string, 0, 2)
	if s.currentFTN().Binkd.DisableCramMD5 {
		// -m stops binkd both offering CRAM-MD5 to callers and answering a
		// remote's offer, so both directions fall back to a plaintext
		// password. Only useful against a peer whose CRAM-MD5 rejects an
		// otherwise-correct password (issue #268).
		args = append(args, "-m")
	}
	return append(args, s.confPath)
}

// runOnce starts binkd and blocks until it exits or ctx is cancelled.
// On cancellation it sends SIGTERM, waits termGrace, then kills.
func (s *Service) runOnce(ctx context.Context) error {
	// No -D flag: binkd runs as a supervised child (not daemonized). The
	// BBS's signal-driven shutdown (SIGTERM, then a grace period, then
	// SIGKILL) is what stops it on exit; on Unix an orphaned child process
	// can otherwise outlive its parent.
	cmd := exec.Command(s.binkdPath, s.binkdArgs()...)
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
	slog.Info("binkd mailer started", "pid", cmd.Process.Pid, "port", s.currentFTN().Binkd.Port)

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
	case <-s.recycle:
		// binkd.conf changed under a running binkd, which only reads it at
		// startup. Stop it the same way shutdown does and report no error, so
		// superviseLoop relaunches immediately on the new config instead of
		// backing off as it would for a crash.
		slog.Info("binkd.conf changed, recycling binkd", "pid", cmd.Process.Pid)
		s.terminate(cmd, waitErr)
		return errRecycle
	case <-ctx.Done():
		s.terminate(cmd, waitErr)
		slog.Info("binkd mailer stopped")
		return nil
	}
}

// terminate stops binkd with SIGTERM, escalating to SIGKILL after termGrace,
// and waits for it to be reaped.
func (s *Service) terminate(cmd *exec.Cmd, waitErr <-chan error) {
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		_ = cmd.Process.Kill() // SIGTERM unsupported (e.g. windows) or gone
	}
	select {
	case <-waitErr:
	case <-time.After(termGrace):
		_ = cmd.Process.Kill()
		<-waitErr
	}
}

// errRecycle marks a stop that superviseLoop asked for itself, so the relaunch
// is immediate and is not counted against the crash backoff.
var errRecycle = errors.New("binkd recycled for a config change")

// syncConf writes the current configuration into binkd.conf and returns the
// outbound directories it resolved. Shared by the launch path and the watcher
// so both produce identical files; every sync is idempotent and rewrites
// nothing when the file already matches.
func (s *Service) syncConf(snap config.FTNConfig) ftn.BinkdOutbound {
	outbound := ftn.BinkdOutboundFor(s.cfg.BBSRoot, snap)
	b := snap.Binkd

	identity := ftn.BinkdIdentity{
		BoardName: s.cfg.Server.BoardName,
		SysopName: s.cfg.Server.SysOpName,
		Location:  s.cfg.Server.BBSLocation,
	}
	links := make(map[string]ftn.BinkdLinkSync)
	for netKey, nc := range snap.Networks {
		for _, lnk := range nc.Links {
			links[fmt.Sprintf("%s@%s", lnk.Address, netKey)] = ftn.BinkdLinkSync{
				SessionPwd: lnk.SessionPassword,
				HostPort:   lnk.HostPort(),
			}
		}
	}
	if err := ftn.SyncBinkdConf(s.confPath, identity, links); err != nil {
		slog.Warn("binkd.conf link sync failed", "error", err)
	}
	if err := ftn.SyncBinkdNetworks(s.confPath, s.cfg.BBSRoot, snap); err != nil {
		slog.Warn("binkd.conf network sync failed", "error", err)
	}
	if err := ftn.SyncBinkdSettings(s.confPath, b.Port, b.LogLevel, outbound); err != nil {
		slog.Warn("binkd.conf settings sync failed", "error", err)
	}
	return outbound
}

// watchConfLoop recycles binkd when binkd.conf changes underneath it.
//
// binkd reads its configuration once, at startup. Nothing else asks it to
// re-read: the supervisor syncs the file only just before a launch, and the
// only other signal is the SIGTERM of a BBS shutdown. A new node, a changed
// hub hostname or password, a new network, a different listen port — all of it
// sat inert until binkd happened to exit, which on a healthy system could be
// weeks. Worse, the sysop sees the saved config and the correct binkd.conf and
// has no way to tell that the running process disagrees with both.
//
// So the file is re-synced from ftn.json on a timer and compared with what the
// running binkd was given. A difference means binkd is stale, and it is
// recycled: a sub-second gap in which the listener is down, against mail that
// otherwise does not flow at all.
func (s *Service) watchConfLoop(ctx context.Context) {
	ticker := time.NewTicker(s.confWatch)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		s.reloadFTN()
		s.syncConf(s.currentFTN())

		current := hashFile(s.confPath)
		if current == "" {
			continue // unreadable: leave the running binkd alone
		}
		seen, _ := s.confSeen.Load().(string)
		if seen == "" || seen == current {
			continue
		}
		s.confSeen.Store(current)
		select {
		case s.recycle <- struct{}{}:
		default: // a recycle is already pending
		}
	}
}

// hashFile returns a content hash of path, or "" if it cannot be read.
func hashFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
