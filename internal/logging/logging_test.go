package logging

import (
	"bufio"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

// withDefaultRestored saves and restores slog.Default around a test, since Init
// mutates global logger state.
func withDefaultRestored(t *testing.T) {
	t.Helper()
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })
}

func readJSONLines(t *testing.T, path string) []map[string]any {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()
	var out []map[string]any
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(strings.TrimSpace(string(line))) == 0 {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal(line, &m); err != nil {
			t.Fatalf("line is not JSON: %v (%q)", err, string(line))
		}
		out = append(out, m)
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan %s: %v", path, err)
	}
	return out
}

func TestInit_HonorsLevelFiltering(t *testing.T) {
	withDefaultRestored(t)
	dir := t.TempDir()
	cfg := config.LoggingConfig{Dir: dir, Level: "WARN", Cache: false, Type: config.LogTypeNone}

	logger, closeFn, err := Init(cfg, "vision3.log")
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	logger.Debug("debug msg")
	logger.Info("info msg")
	logger.Warn("warn msg")
	logger.Error("error msg")
	if err := closeFn(); err != nil {
		t.Fatalf("close: %v", err)
	}

	lines := readJSONLines(t, filepath.Join(dir, "vision3.log"))
	msgs := map[string]bool{}
	for _, l := range lines {
		msgs[l["msg"].(string)] = true
	}
	if msgs["debug msg"] || msgs["info msg"] {
		t.Error("sub-threshold (DEBUG/INFO) records should be dropped at WARN level")
	}
	if !msgs["warn msg"] || !msgs["error msg"] {
		t.Error("WARN and ERROR records should be present at WARN level")
	}
}

func TestInit_ErrorFlushesCache(t *testing.T) {
	withDefaultRestored(t)
	dir := t.TempDir()
	cfg := config.LoggingConfig{Dir: dir, Level: "DEBUG", Cache: true, Type: config.LogTypeNone}

	logger, closeFn, err := Init(cfg, "vision3.log")
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	t.Cleanup(func() { closeFn() })
	path := filepath.Join(dir, "vision3.log")

	// An INFO record stays buffered (cache enabled, well under 8KB).
	logger.Info("buffered info")
	if got, _ := os.ReadFile(path); len(got) != 0 {
		t.Errorf("INFO should remain cached, found %q on disk", got)
	}

	// An ERROR record must flush the cache, making both records durable.
	logger.Error("flushing error")
	lines := readJSONLines(t, path)
	if len(lines) != 2 {
		t.Fatalf("expected 2 records on disk after Error flush, got %d", len(lines))
	}
	if lines[0]["msg"] != "buffered info" || lines[1]["msg"] != "flushing error" {
		t.Errorf("unexpected records on disk: %v", lines)
	}
}

func TestInit_InvalidLevelFallsBackToInfo(t *testing.T) {
	withDefaultRestored(t)
	dir := t.TempDir()
	cfg := config.LoggingConfig{Dir: dir, Level: "bogus", Cache: false, Type: config.LogTypeNone}

	logger, closeFn, err := Init(cfg, "vision3.log")
	if err != nil {
		t.Fatalf("Init should not fail on bad level: %v", err)
	}
	logger.Debug("dropped at info")
	logger.Info("kept at info")
	if err := closeFn(); err != nil {
		t.Fatalf("close: %v", err)
	}

	lines := readJSONLines(t, filepath.Join(dir, "vision3.log"))
	var sawWarning, sawInfo, sawDebug bool
	for _, l := range lines {
		switch l["msg"] {
		case "invalid log level; defaulting to INFO":
			sawWarning = true
		case "kept at info":
			sawInfo = true
		case "dropped at info":
			sawDebug = true
		}
	}
	if !sawWarning {
		t.Error("expected a warning about the invalid level")
	}
	if !sawInfo {
		t.Error("INFO record should be kept at the INFO fallback level")
	}
	if sawDebug {
		t.Error("DEBUG record should be dropped at the INFO fallback level")
	}
}
