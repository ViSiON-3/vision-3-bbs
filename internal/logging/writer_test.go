package logging

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

// fakeClock is a goroutine-safe, manually advanced clock for deterministic
// rolling tests.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func newWriter(t *testing.T, cfg config.LoggingConfig, file string, clk *fakeClock) *rollingWriter {
	t.Helper()
	w, err := newRollingWriter(cfg, file, clk.now)
	if err != nil {
		t.Fatalf("newRollingWriter: %v", err)
	}
	t.Cleanup(func() { w.Close() })
	return w
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

func TestWriter_None_AppendsToSingleFile(t *testing.T) {
	dir := t.TempDir()
	clk := &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	cfg := config.LoggingConfig{Dir: dir, Type: config.LogTypeNone, Cache: false}
	w := newWriter(t, cfg, "vision3.log", clk)

	w.Write([]byte("one\n"))
	w.Write([]byte("two\n"))

	got := mustRead(t, filepath.Join(dir, "vision3.log"))
	if got != "one\ntwo\n" {
		t.Errorf("content = %q, want %q", got, "one\ntwo\n")
	}
	if _, err := os.Stat(filepath.Join(dir, "vision3.log.1")); !os.IsNotExist(err) {
		t.Error("none type should never create a numbered backup")
	}
}

func TestWriter_Cache_BuffersUntilFlush(t *testing.T) {
	dir := t.TempDir()
	clk := &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	cfg := config.LoggingConfig{Dir: dir, Type: config.LogTypeNone, Cache: true}
	w := newWriter(t, cfg, "vision3.log", clk)
	path := filepath.Join(dir, "vision3.log")

	w.Write([]byte("buffered\n"))
	if got := mustRead(t, path); got != "" {
		t.Errorf("cached write should not be on disk yet, got %q", got)
	}
	if err := w.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if got := mustRead(t, path); got != "buffered\n" {
		t.Errorf("after Flush content = %q, want %q", got, "buffered\n")
	}
}

func TestWriter_Cache_CloseFlushes(t *testing.T) {
	dir := t.TempDir()
	clk := &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	cfg := config.LoggingConfig{Dir: dir, Type: config.LogTypeNone, Cache: true}
	w, err := newRollingWriter(cfg, "vision3.log", clk.now)
	if err != nil {
		t.Fatalf("newRollingWriter: %v", err)
	}
	w.Write([]byte("on close\n"))
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if got := mustRead(t, filepath.Join(dir, "vision3.log")); got != "on close\n" {
		t.Errorf("Close should flush; content = %q", got)
	}
}

func TestWriter_Size_RotatesAndCapsBackups(t *testing.T) {
	dir := t.TempDir()
	clk := &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	cfg := config.LoggingConfig{Dir: dir, Type: config.LogTypeSize, Cache: false, MaxFiles: 2, MaxSizeKB: 1}
	w := newWriter(t, cfg, "vision3.log", clk)

	line := strings.Repeat("x", 600) + "\n" // 601 bytes; two lines exceed 1KB
	for i := 0; i < 4; i++ {
		w.Write([]byte(line))
	}

	// Base exists; backups .1 and .2 exist; .3 must not (oldest dropped).
	for _, p := range []string{"vision3.log", "vision3.log.1", "vision3.log.2"} {
		if _, err := os.Stat(filepath.Join(dir, p)); err != nil {
			t.Errorf("expected %s to exist: %v", p, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "vision3.log.3")); !os.IsNotExist(err) {
		t.Errorf("vision3.log.3 should not exist (MaxFiles=2)")
	}
}

func TestWriter_Size_OversizedLineWrittenWhole(t *testing.T) {
	dir := t.TempDir()
	clk := &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	cfg := config.LoggingConfig{Dir: dir, Type: config.LogTypeSize, Cache: false, MaxFiles: 2, MaxSizeKB: 1}
	w := newWriter(t, cfg, "vision3.log", clk)

	big := strings.Repeat("y", 2000) + "\n" // exceeds 1KB on an empty file
	w.Write([]byte(big))
	if got := mustRead(t, filepath.Join(dir, "vision3.log")); got != big {
		t.Errorf("oversized first line should be written whole, got %d bytes", len(got))
	}
}

func TestWriter_Daily_RollsAndPrunes(t *testing.T) {
	dir := t.TempDir()
	clk := &fakeClock{t: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	cfg := config.LoggingConfig{Dir: dir, Type: config.LogTypeDaily, Cache: false, MaxFiles: 2}
	w := newWriter(t, cfg, "vision3.log", clk)

	w.Write([]byte("day1\n"))
	day1 := filepath.Join(dir, "vision3.2026-01-01.log")
	if mustRead(t, day1) != "day1\n" {
		t.Fatalf("day1 file not written")
	}

	// Jump well past MaxFiles days; the next write should roll to a new dated
	// file and prune the now-stale day1 file.
	clk.advance(10 * 24 * time.Hour)
	w.Write([]byte("day11\n"))

	day11 := filepath.Join(dir, "vision3.2026-01-11.log")
	if mustRead(t, day11) != "day11\n" {
		t.Errorf("day11 file not written")
	}
	if _, err := os.Stat(day1); !os.IsNotExist(err) {
		t.Errorf("day1 file should have been pruned (older than MaxFiles=2 days)")
	}
}

func TestWriter_Daily_KeepsRecentFiles(t *testing.T) {
	dir := t.TempDir()
	clk := &fakeClock{t: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	cfg := config.LoggingConfig{Dir: dir, Type: config.LogTypeDaily, Cache: false, MaxFiles: 5}
	w := newWriter(t, cfg, "vision3.log", clk)

	w.Write([]byte("d1\n"))
	clk.advance(24 * time.Hour)
	w.Write([]byte("d2\n"))

	// Within MaxFiles=5 days, both files survive.
	if _, err := os.Stat(filepath.Join(dir, "vision3.2026-01-01.log")); err != nil {
		t.Errorf("recent day1 file should survive: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "vision3.2026-01-02.log")); err != nil {
		t.Errorf("day2 file should exist: %v", err)
	}
}

func TestWriter_WriteAfterCloseErrors(t *testing.T) {
	dir := t.TempDir()
	clk := &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	cfg := config.LoggingConfig{Dir: dir, Type: config.LogTypeNone, Cache: false}
	w, err := newRollingWriter(cfg, "vision3.log", clk.now)
	if err != nil {
		t.Fatalf("newRollingWriter: %v", err)
	}
	w.Close()
	if _, err := w.Write([]byte("nope\n")); err == nil {
		t.Error("Write after Close should return an error")
	}
	if err := w.Close(); err != nil {
		t.Errorf("second Close should be a no-op, got %v", err)
	}
}
