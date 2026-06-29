package tosser

import (
	"context"
	"log/slog"
	"time"
)

// Start begins the tosser background polling loop.
// It runs import+export cycles at the configured interval.
// Call cancel on the context to stop.
func (t *Tosser) Start(ctx context.Context) {
	if t.config.PollSeconds <= 0 {
		slog.Info("polling disabled, use RunOnce for manual toss", "network", t.networkName)
		return
	}

	interval := time.Duration(t.config.PollSeconds) * time.Second
	slog.Info("tosser started", "network", t.networkName, "interval", interval)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("tosser stopping", "network", t.networkName)
			// Save dupe DB on shutdown
			if err := t.dupeDB.Save(); err != nil {
				slog.Warn("failed to save dupe DB on shutdown", "network", t.networkName, "error", err)
			}
			return
		case <-ticker.C:
			result := t.RunOnce()
			if result.PacketsProcessed > 0 || result.MessagesExported > 0 {
				slog.Info("toss cycle complete", "network", t.networkName, "imported", result.MessagesImported, "exported", result.MessagesExported, "dupes", result.DupesSkipped, "packets", result.PacketsProcessed)
			}
			if len(result.Errors) > 0 {
				for _, e := range result.Errors {
					slog.Error("toss cycle error", "network", t.networkName, "msg", e)
				}
			}
		}
	}
}

// RunOnce performs a single import+export cycle.
func (t *Tosser) RunOnce() TossResult {
	importResult := t.ProcessInbound()
	exportResult := t.ScanAndExport()

	return TossResult{
		PacketsProcessed: importResult.PacketsProcessed,
		MessagesImported: importResult.MessagesImported,
		MessagesExported: exportResult.MessagesExported,
		DupesSkipped:     importResult.DupesSkipped,
		Errors:           append(importResult.Errors, exportResult.Errors...),
	}
}

// PurgeDupes removes old entries from the dupe database.
func (t *Tosser) PurgeDupes() error {
	return t.dupeDB.Purge()
}
