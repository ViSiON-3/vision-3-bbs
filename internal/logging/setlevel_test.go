package logging

import (
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

// withLevelRestored saves and restores the package-level activeLevel, which
// Init mutates and which persists across tests in one binary.
func withLevelRestored(t *testing.T) {
	t.Helper()
	prev := activeLevel
	t.Cleanup(func() { activeLevel = prev })
}

func initTestLogger(t *testing.T, level string) string {
	t.Helper()
	withDefaultRestored(t)
	withLevelRestored(t)
	dir := t.TempDir()
	_, closeFn, err := Init(config.LoggingConfig{Dir: dir, Level: level, Cache: false}, "test.log", false)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	t.Cleanup(func() { _ = closeFn() })
	return filepath.Join(dir, "test.log")
}

// TestSetLevelChangesEffectiveLevel is the point of the feature: a level
// change must affect which records the running logger emits, not just a
// number somewhere.
func TestSetLevelChangesEffectiveLevel(t *testing.T) {
	logPath := initTestLogger(t, "INFO")

	slog.Debug("suppressed-before")
	if err := SetLevel("DEBUG"); err != nil {
		t.Fatalf("SetLevel(DEBUG): %v", err)
	}
	slog.Debug("emitted-after")
	if err := SetLevel("ERROR"); err != nil {
		t.Fatalf("SetLevel(ERROR): %v", err)
	}
	slog.Info("suppressed-at-error")

	var msgs []string
	for _, line := range readJSONLines(t, logPath) {
		if m, ok := line["msg"].(string); ok {
			msgs = append(msgs, m)
		}
	}
	want := map[string]bool{"emitted-after": true}
	for _, m := range msgs {
		switch m {
		case "suppressed-before", "suppressed-at-error":
			t.Errorf("%q was emitted despite the level filtering it", m)
		case "emitted-after":
			delete(want, m)
		}
	}
	for m := range want {
		t.Errorf("%q missing from the log", m)
	}
}

func TestSetLevelRejectsUnknownAndKeepsCurrent(t *testing.T) {
	initTestLogger(t, "WARN")

	if err := SetLevel("VERBOSE"); err == nil {
		t.Fatal("SetLevel accepted an unknown level name")
	}
	if got := Level(); got != slog.LevelWarn {
		t.Errorf("Level() = %v after a rejected SetLevel, want WARN", got)
	}
}

func TestSetLevelBeforeInitErrors(t *testing.T) {
	withLevelRestored(t)
	activeLevel = nil

	if err := SetLevel("DEBUG"); err == nil {
		t.Error("SetLevel before Init succeeded; it must error, not silently no-op")
	}
	if got := Level(); got != slog.LevelInfo {
		t.Errorf("Level() before Init = %v, want the INFO default", got)
	}
}

func TestInitSetsActiveLevel(t *testing.T) {
	initTestLogger(t, "DEBUG")
	if got := Level(); got != slog.LevelDebug {
		t.Errorf("Level() after Init(DEBUG) = %v, want DEBUG", got)
	}
}
