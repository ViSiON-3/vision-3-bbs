// Package mailer supervises the bundled binkd FTN mailer as a child process
// of the BBS and periodically exports outbound echomail/netmail into binkd's
// outbound queue using the internal tosser.
package mailer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/tosser"
)

// Config holds the dependencies for the mailer service. FTN paths must
// already be resolved to absolute paths (FTNConfig.ResolvePaths).
type Config struct {
	BBSRoot string                  // absolute BBS root directory
	FTN     config.FTNConfig        // FTN config including the Binkd section
	MsgMgr  *message.MessageManager // for the export loop (may be nil in tests)
	DupeDB  *tosser.DupeDB          // for the export loop (may be nil in tests)
}

// Service runs binkd under supervision plus the outbound export loop.
type Service struct {
	cfg        Config
	binkdPath  string // resolved absolute path to the binkd binary
	confPath   string // absolute path to binkd.conf
	backoffMin time.Duration
	backoffMax time.Duration
	healthyRun time.Duration // process uptime that resets the backoff

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
	if _, err := os.Stat(confPath); err != nil {
		return nil, fmt.Errorf("binkd.conf not found (run the FTN Setup Wizard first): %w", err)
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

	return &Service{
		cfg:        cfg,
		binkdPath:  binkdPath,
		confPath:   confPath,
		backoffMin: 5 * time.Second,
		backoffMax: 5 * time.Minute,
		healthyRun: time.Minute,
		done:       make(chan struct{}),
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

// Close waits for the loops to finish (if Start was ever called) and
// persists the dupe database. It is safe to call Close before Start: it
// returns promptly after saving the dupe database.
func (s *Service) Close() error {
	if s.started.Load() {
		<-s.done
	}
	if s.cfg.DupeDB != nil {
		if err := s.cfg.DupeDB.Save(); err != nil {
			return fmt.Errorf("saving dupe db: %w", err)
		}
	}
	return nil
}
