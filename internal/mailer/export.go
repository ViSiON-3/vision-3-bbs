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
	if s.cfg.MsgMgr == nil {
		slog.Warn("binkd export loop disabled: message manager unavailable")
		return
	}
	if s.cfg.FTN.Binkd.ExportSecs <= 0 {
		slog.Warn("binkd export loop disabled: export interval must be positive",
			"export_secs", s.cfg.FTN.Binkd.ExportSecs)
		return
	}
	interval := time.Duration(s.cfg.FTN.Binkd.ExportSecs) * time.Second
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
			s.exportOnce()
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
	for name, netCfg := range s.cfg.FTN.Networks {
		if !netCfg.InternalTosserEnabled {
			continue
		}
		t, err := tosser.New(name, netCfg, s.cfg.FTN, s.exportDupeDB, s.cfg.MsgMgr)
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
