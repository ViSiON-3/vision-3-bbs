package logging

import (
	"context"
	"log/slog"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

// Init builds the rolling writer for cfg, wraps it in a JSON slog handler at the
// configured level, installs it as slog.Default, and returns the logger plus a
// close function the caller should defer (it flushes the cache and closes the
// file). defaultFile is the binary-specific log filename (e.g. "vision3.log");
// all other settings come from cfg.
//
// An unrecognized cfg.Level does not fail startup: Init falls back to INFO and
// logs a warning through the freshly installed logger.
func Init(cfg config.LoggingConfig, defaultFile string) (*slog.Logger, func() error, error) {
	cfg.Normalize()

	level, levelErr := ParseLevel(cfg.Level)

	w, err := newRollingWriter(cfg, defaultFile, nil)
	if err != nil {
		return nil, nil, err
	}

	handler := &flushHandler{
		Handler: slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level}),
		w:       w,
	}
	logger := slog.New(handler)
	slog.SetDefault(logger)

	if levelErr != nil {
		logger.Warn("invalid log level; defaulting to INFO", "configured", cfg.Level)
	}

	return logger, w.Close, nil
}

// flushHandler wraps a slog.Handler so that Error-level records flush the
// write cache immediately, ensuring failures are durable even if the process
// dies before the next ticker flush.
type flushHandler struct {
	slog.Handler
	w *rollingWriter
}

func (h *flushHandler) Handle(ctx context.Context, r slog.Record) error {
	err := h.Handler.Handle(ctx, r)
	if r.Level >= slog.LevelError {
		_ = h.w.Flush()
	}
	return err
}

func (h *flushHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &flushHandler{Handler: h.Handler.WithAttrs(attrs), w: h.w}
}

func (h *flushHandler) WithGroup(name string) slog.Handler {
	return &flushHandler{Handler: h.Handler.WithGroup(name), w: h.w}
}
