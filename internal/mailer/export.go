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
	if s.cfg.MsgMgr == nil || s.cfg.DupeDB == nil {
		slog.Warn("binkd export loop disabled: message manager or dupe db unavailable")
		return
	}
	interval := time.Duration(s.cfg.FTN.Binkd.ExportSecs) * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	slog.Info("binkd export loop started", "interval", interval)
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
func (s *Service) exportOnce() {
	for name, netCfg := range s.cfg.FTN.Networks {
		if !netCfg.InternalTosserEnabled {
			continue
		}
		t, err := tosser.New(name, netCfg, s.cfg.FTN, s.cfg.DupeDB, s.cfg.MsgMgr)
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
