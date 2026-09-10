package config

import (
	"os"
	"testing"
	"time"
)

func TestTouchReloadSentinelCreatesFile(t *testing.T) {
	dir := t.TempDir()

	if err := TouchReloadSentinel(dir); err != nil {
		t.Fatalf("TouchReloadSentinel: %v", err)
	}

	info, err := os.Stat(ReloadSentinelPath(dir))
	if err != nil {
		t.Fatalf("sentinel not created: %v", err)
	}
	if info.IsDir() {
		t.Error("sentinel is a directory, want a regular file")
	}
}

// TestTouchReloadSentinelAdvancesTimestamp is the property the whole signal
// depends on: touching an existing sentinel must move its modification time,
// or a second save in a row would go unnoticed.
func TestTouchReloadSentinelAdvancesTimestamp(t *testing.T) {
	dir := t.TempDir()
	path := ReloadSentinelPath(dir)

	if err := TouchReloadSentinel(dir); err != nil {
		t.Fatalf("first touch: %v", err)
	}
	// Backdate it so the second touch has an unambiguous effect regardless of
	// filesystem timestamp granularity.
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	if err := TouchReloadSentinel(dir); err != nil {
		t.Fatalf("second touch: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if !info.ModTime().After(old) {
		t.Errorf("modification time %v did not advance past %v", info.ModTime(), old)
	}
}

func TestTouchReloadSentinelErrorsOnMissingDir(t *testing.T) {
	if err := TouchReloadSentinel("/nonexistent-dir-for-test/nope"); err == nil {
		t.Error("expected an error touching a sentinel in a missing directory")
	}
}
