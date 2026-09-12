package main

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

// restoreLogger puts slog and the stdlib log bridge back after a test, since
// logging.Init installs both globally.
func restoreLogger(t *testing.T) {
	t.Helper()
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })
}

// The editor used to discard slog entirely, so a config save left no record
// anywhere and every warning it produces — an FTN link with no hostname, a
// network declared in binkd.conf, a rejected outbound path — vanished.
func TestEditorLogWritesToFile(t *testing.T) {
	restoreLogger(t)
	bbsRoot := t.TempDir()

	cfg := config.ServerConfig{}
	cfg.Logging.Dir = "data/logs" // relative, as shipped

	closeLog, err := initEditorLog(cfg, bbsRoot)
	if err != nil {
		t.Fatalf("initEditorLog: %v", err)
	}
	slog.Info("marker line", "saved", "events.json")
	if err := closeLog(); err != nil {
		t.Fatalf("closeLog: %v", err)
	}

	want := filepath.Join(bbsRoot, "data", "logs", "config.log")
	data, err := os.ReadFile(want)
	if err != nil {
		t.Fatalf("expected a log at %s: %v", want, err)
	}
	if !strings.Contains(string(data), "marker line") {
		t.Errorf("log does not contain the record:\n%s", data)
	}
	if !strings.Contains(string(data), "events.json") {
		t.Errorf("log dropped the record's attributes:\n%s", data)
	}
}

// vision3 and v3mail are launched from the BBS root, so a relative "data/logs"
// resolves for them against the working directory. The editor is run as
// `config --config /path/to/configs` from anywhere, so the same relative path
// has to be resolved against the BBS root or the log lands somewhere random.
func TestEditorLogResolvesRelativeDirAgainstBBSRoot(t *testing.T) {
	restoreLogger(t)
	bbsRoot := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	cfg := config.ServerConfig{}
	cfg.Logging.Dir = "data/logs"

	closeLog, err := initEditorLog(cfg, bbsRoot)
	if err != nil {
		t.Fatalf("initEditorLog: %v", err)
	}
	slog.Info("marker")
	_ = closeLog()

	if _, err := os.Stat(filepath.Join(bbsRoot, "data", "logs", "config.log")); err != nil {
		t.Errorf("log should be under the BBS root: %v", err)
	}
	// And must not have been created relative to wherever the test is running.
	if _, err := os.Stat(filepath.Join(cwd, "data", "logs", "config.log")); err == nil {
		t.Error("log was written relative to the working directory, not the BBS root")
	}
}

// An absolute log dir is the sysop's explicit choice and must be left alone.
func TestEditorLogKeepsAbsoluteDir(t *testing.T) {
	restoreLogger(t)
	logDir := t.TempDir()

	cfg := config.ServerConfig{}
	cfg.Logging.Dir = logDir

	closeLog, err := initEditorLog(cfg, t.TempDir())
	if err != nil {
		t.Fatalf("initEditorLog: %v", err)
	}
	slog.Info("marker")
	_ = closeLog()

	if _, err := os.Stat(filepath.Join(logDir, "config.log")); err != nil {
		t.Errorf("absolute dir should be used as given: %v", err)
	}
}

// An unset dir falls back to the shipped default, still under the BBS root.
func TestEditorLogDefaultsDir(t *testing.T) {
	restoreLogger(t)
	bbsRoot := t.TempDir()

	closeLog, err := initEditorLog(config.ServerConfig{}, bbsRoot)
	if err != nil {
		t.Fatalf("initEditorLog: %v", err)
	}
	slog.Info("marker")
	_ = closeLog()

	if _, err := os.Stat(filepath.Join(bbsRoot, config.DefaultLogDir, "config.log")); err != nil {
		t.Errorf("expected the default log dir under the BBS root: %v", err)
	}
}

// A logging failure has to be reported rather than panicking, because the caller
// can only fall back silently: once the TUI owns the terminal, writing to stderr
// would corrupt the display.
func TestEditorLogReportsFailure(t *testing.T) {
	restoreLogger(t)
	bbsRoot := t.TempDir()
	// A file where the log directory needs to be, so MkdirAll cannot succeed.
	blocker := filepath.Join(bbsRoot, "logs")
	if err := os.WriteFile(blocker, []byte("not a directory"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := config.ServerConfig{}
	cfg.Logging.Dir = "logs"

	if _, err := initEditorLog(cfg, bbsRoot); err == nil {
		t.Error("an unusable log directory should be reported, not ignored")
	}
}
