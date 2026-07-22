// Package mailer supervises the bundled binkd FTN mailer as a child process
// of the BBS and periodically exports outbound echomail/netmail into binkd's
// outbound queue using the internal tosser.
package mailer

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/ftn"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/tosser"
)

// Config holds the dependencies for the mailer service. FTN paths must
// already be resolved to absolute paths (FTNConfig.ResolvePaths).
type Config struct {
	BBSRoot string                  // absolute BBS root directory
	FTN     config.FTNConfig        // FTN config including the Binkd section
	Server  config.ServerConfig     // BBS identity for binkd.conf regeneration
	MsgMgr  *message.MessageManager // for the export loop (may be nil in tests)
}

// Service runs binkd under supervision plus the outbound export loop.
type Service struct {
	cfg        Config
	binkdPath  string // resolved absolute path to the binkd binary
	confPath   string // absolute path to binkd.conf
	backoffMin time.Duration
	backoffMax time.Duration
	healthyRun time.Duration // process uptime that resets the backoff

	// exportDupeDB is a throwaway dupe database used only to satisfy the
	// tosser constructor's signature during export. The export path never
	// records or persists dupes: v3mail toss (external process) owns
	// data/ftn/dupes.json for real dupe tracking on inbound mail.
	exportDupeDB *tosser.DupeDB

	// exportDisabled is set in New when FTN config validation fails for a
	// tosser-enabled network (e.g. blank global paths). It suppresses the
	// export loop entirely so the failure is logged once, up front, instead
	// of on every export cycle. Inbound (binkd itself) is unaffected.
	exportDisabled bool

	started atomic.Bool
	done    chan struct{} // closed when Start's loops have both finished
	wg      sync.WaitGroup
}

// New validates the environment (preflight) and returns a ready service.
// Errors are expected to be logged as warnings by the caller; they must not
// abort BBS startup.
func New(cfg Config) (*Service, error) {
	b := cfg.FTN.Binkd

	binkdPath := b.BinaryPath
	if !filepath.IsAbs(binkdPath) {
		binkdPath = filepath.Join(cfg.BBSRoot, binkdPath)
	}
	info, err := os.Stat(binkdPath)
	if err != nil {
		return nil, fmt.Errorf("binkd binary not found at %s: %w", binkdPath, err)
	}
	if info.Mode()&0111 == 0 {
		return nil, fmt.Errorf("binkd binary %s is not executable", binkdPath)
	}

	confPath := filepath.Join(cfg.BBSRoot, "data", "ftn", "binkd.conf")

	// A deleted binkd.conf is regenerated from configuration (best-effort):
	// the FTN Setup Wizard refuses to re-run for an existing network, so
	// without this the mailer would stay down until the next TUI save.
	if created, ensureErr := ftn.EnsureBinkdConf(cfg.BBSRoot, cfg.FTN, cfg.Server); ensureErr != nil {
		slog.Warn("binkd.conf regeneration failed", "error", ensureErr)
	} else if created {
		slog.Info("binkd.conf regenerated from configuration", "path", confPath)
	}

	confData, err := os.ReadFile(confPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("binkd.conf not found (run the FTN Setup Wizard first): %w", err)
		}
		return nil, fmt.Errorf("reading binkd.conf: %w", err)
	}
	// An unconfigured template conf makes binkd exit 1 in a restart loop;
	// refuse it up front with a clear message instead.
	if ftn.HasPlaceholders(string(confData), cfg.BBSRoot) {
		return nil, fmt.Errorf("binkd.conf at %s still contains template placeholders (run the FTN Setup Wizard)", confPath)
	}

	if b.Port < 1 || b.Port > 65535 {
		return nil, fmt.Errorf("binkd port %d out of range 1-65535", b.Port)
	}

	hasAddress := false
	for _, net := range cfg.FTN.Networks {
		if net.OwnAddress != "" {
			hasAddress = true
			break
		}
	}
	if !hasAddress {
		return nil, fmt.Errorf("no FTN network has an own address configured")
	}

	// Throwaway dupe DB for the export path: the tosser constructor requires
	// one, but export never records dupes (only inbound v3mail toss does),
	// and os.DevNull guarantees this instance never reads or writes real
	// dupe state on disk.
	exportDupeDB, err := tosser.NewDupeDB(os.DevNull, 0)
	if err != nil {
		return nil, fmt.Errorf("creating export dupe db: %w", err)
	}

	// Validate FTN global paths for any tosser-enabled network up front.
	// binkd can still serve inbound with an invalid config, so this must not
	// fail New; instead the export loop is disabled with a single warning
	// here rather than repeated tosser init/scan errors on every cycle.
	exportDisabled := false
	if err := config.ValidateFTNConfig(cfg.FTN); err != nil {
		slog.Warn("binkd export loop disabled: FTN config invalid", "error", err)
		exportDisabled = true
	}

	return &Service{
		cfg:            cfg,
		binkdPath:      binkdPath,
		confPath:       confPath,
		backoffMin:     5 * time.Second,
		backoffMax:     5 * time.Minute,
		healthyRun:     time.Minute,
		done:           make(chan struct{}),
		exportDupeDB:   exportDupeDB,
		exportDisabled: exportDisabled,
	}, nil
}

// Start runs the binkd supervisor and the export loop until ctx is cancelled.
// It blocks; run it in a goroutine. Calling Start more than once is a no-op.
func (s *Service) Start(ctx context.Context) {
	if !s.started.CompareAndSwap(false, true) {
		return
	}
	defer close(s.done)

	s.wg.Add(2)
	go func() {
		defer s.wg.Done()
		s.superviseLoop(ctx)
	}()
	go func() {
		defer s.wg.Done()
		s.exportLoop(ctx)
	}()
	s.wg.Wait()
}

// Close waits for the loops to finish (if Start was ever called). It is
// safe to call Close before Start: it returns promptly. There is no dupe
// database to persist here — the export path never records dupes, and
// v3mail toss (external process) owns data/ftn/dupes.json for inbound mail.
func (s *Service) Close() error {
	if s.started.Load() {
		<-s.done
	}
	return nil
}
