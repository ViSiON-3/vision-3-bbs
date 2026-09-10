package mailer

import (
	"context"
	"log/slog"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/tosser"
)

// exportLoop periodically scans JAM bases for unsent mail and packs it into
// binkd's outbound directory. Inbound needs no loop: the binkd.conf exec hook
// runs "v3mail toss" after each receive.
func (s *Service) exportLoop(ctx context.Context) {
	if s.exportDisabled {
		// Already logged once in New; avoid repeated logging every cycle.
		return
	}
	if s.cfg.MsgMgr == nil {
		slog.Warn("binkd export loop disabled: message manager unavailable")
		return
	}
	if s.currentFTN().Binkd.ExportSecs <= 0 {
		slog.Warn("binkd export loop disabled: export interval must be positive",
			"export_secs", s.currentFTN().Binkd.ExportSecs)
		return
	}
	interval := time.Duration(s.currentFTN().Binkd.ExportSecs) * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	slog.Info("binkd export loop started", "interval", interval)
	// Run once immediately so mail queued while the BBS was down doesn't
	// wait a full interval before being exported.
	s.exportOnce()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// exportOnce uses the active snapshot, which the supervisor swaps in
			// only when it (re)launches binkd — so the tosser always packs into
			// the directory binkd is actually watching (the #266 invariant).
			// Outbound-path, port, and network changes therefore take effect on
			// the next binkd launch, not mid-run.
			s.exportOnce()
			// The export interval is the exception: it is independent of binkd,
			// so a config-editor change to it is safe to apply live. Read it
			// straight from disk (without touching the active snapshot) and
			// retune the ticker.
			if newInterval := s.reloadedExportInterval(); newInterval > 0 && newInterval != interval {
				interval = newInterval
				ticker.Reset(interval)
				slog.Info("binkd export interval changed", "interval", interval)
			}
		}
	}
}

// exportOnce runs scan+pack for every tosser-enabled network.
//
// The tosser constructor requires a *tosser.DupeDB, but export
// (ScanAndExport/PackOutbound) never consults or records dupes — only
// inbound tossing does, and inbound runs in external "v3mail toss"
// processes that own data/ftn/dupes.json. s.exportDupeDB is a throwaway
// instance backed by os.DevNull that is never read from or written to.
func (s *Service) exportOnce() {
	ftnCfg := s.currentFTN()
	for name, netCfg := range ftnCfg.Networks {
		if !netCfg.InternalTosserEnabled {
			continue
		}
		t, err := tosser.New(name, netCfg, ftnCfg, s.exportDupeDB, s.cfg.MsgMgr)
		if err != nil {
			slog.Error("binkd export: tosser init failed", "network", name, "error", err)
			continue
		}
		scan := t.ScanAndExport()
		pack := t.PackOutbound()
		if scan.MessagesExported > 0 || pack.BundlesCreated > 0 {
			slog.Info("binkd export cycle", "network", name,
				"exported", scan.MessagesExported, "bundles", pack.BundlesCreated)
		}
		for _, e := range append(scan.Errors, pack.Errors...) {
			slog.Error("binkd export error", "network", name, "msg", e)
		}
	}
}
