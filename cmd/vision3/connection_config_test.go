package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

// newConfigTracker builds a tracker the way newRateTracker does, with limits
// set as NewConnectionTracker would.
func newConfigTracker(maxNodes, maxPerIP int) *ConnectionTracker {
	ct := &ConnectionTracker{
		activeConnections:   make(map[string]int),
		failedLogins:        make(map[string]*IPLockoutTracker),
		connAttempts:        make(map[string][]time.Time),
		connTempBans:        make(map[string]time.Time),
		maxNodes:            maxNodes,
		maxConnectionsPerIP: maxPerIP,
	}
	return ct
}

// TestApplyServerConfigUpdatesLimits is the regression test for issue #322:
// reloading config.json must reach the connection tracker, not just the
// in-memory config struct.
func TestApplyServerConfigUpdatesLimits(t *testing.T) {
	ct := newConfigTracker(1, 1)

	a := addr("203.0.113.10")
	if ok, _ := ct.TryAccept(a); !ok {
		t.Fatal("first connection rejected")
	}
	// maxNodes is 1, so a second connection is refused.
	if ok, _ := ct.CanAccept(addr("203.0.113.11")); ok {
		t.Fatal("second connection accepted with maxNodes=1")
	}

	ct.ApplyServerConfig(config.ServerConfig{
		MaxNodes:            10,
		MaxConnectionsPerIP: 5,
		MaxFailedLogins:     7,
		LockoutMinutes:      15,
	})

	if ok, reason := ct.CanAccept(addr("203.0.113.11")); !ok {
		t.Errorf("connection refused after raising maxNodes: %s", reason)
	}

	ct.mu.Lock()
	defer ct.mu.Unlock()
	if ct.maxNodes != 10 || ct.maxConnectionsPerIP != 5 {
		t.Errorf("limits = (%d, %d), want (10, 5)", ct.maxNodes, ct.maxConnectionsPerIP)
	}
	if ct.maxFailedLogins != 7 || ct.lockoutMinutes != 15 {
		t.Errorf("lockout settings = (%d, %d), want (7, 15)", ct.maxFailedLogins, ct.lockoutMinutes)
	}
}

// TestApplyServerConfigReachesRateLimiter covers the rate-limit fields, which
// had a working setter that reloadServerConfig simply never called.
func TestApplyServerConfigReachesRateLimiter(t *testing.T) {
	ct := newConfigTracker(0, 0)

	ct.ApplyServerConfig(config.ServerConfig{
		EnableConnRateLimit:        true,
		ConnRateLimitHits:          2,
		ConnRateLimitWindowSeconds: 10,
		ConnRateLimitBanMinutes:    5,
	})

	a := addr("203.0.113.20")
	if ok, _ := ct.TryAccept(a); !ok {
		t.Fatal("first attempt rejected")
	}
	if ok, _ := ct.TryAccept(a); ok {
		t.Fatal("second attempt should trip the rate limit")
	}
}

// TestLoweringMaxNodesDoesNotDropCallers documents the shrink policy: the new
// limit gates future accepts, it does not disconnect anyone already online.
func TestLoweringMaxNodesDoesNotDropCallers(t *testing.T) {
	ct := newConfigTracker(10, 10)

	for _, ip := range []string{"203.0.113.30", "203.0.113.31", "203.0.113.32"} {
		if ok, _ := ct.TryAccept(addr(ip)); !ok {
			t.Fatalf("connection from %s rejected", ip)
		}
	}

	ct.SetLimits(2, 10, 3, 10) // below the 3 already connected

	total, _ := ct.GetStats()
	if total != 3 {
		t.Errorf("active connections = %d after lowering the limit, want 3 (nobody dropped)", total)
	}
	if ok, _ := ct.CanAccept(addr("203.0.113.33")); ok {
		t.Error("new connection accepted while over the lowered limit")
	}

	// As callers log off and usage falls under the limit, accepts resume.
	ct.RemoveConnection(addr("203.0.113.30"))
	ct.RemoveConnection(addr("203.0.113.31"))
	if ok, reason := ct.CanAccept(addr("203.0.113.33")); !ok {
		t.Errorf("connection refused after draining below the limit: %s", reason)
	}
}

func TestSetIPListPathsSwapsLists(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "block1.txt")
	second := filepath.Join(dir, "block2.txt")
	if err := os.WriteFile(first, []byte("203.0.113.40\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("203.0.113.41\n"), 0644); err != nil {
		t.Fatal(err)
	}

	ct := newConfigTracker(0, 0)
	ct.SetIPListPaths(first, "")
	defer ct.StopWatching()

	if ok, _ := ct.CanAccept(addr("203.0.113.40")); ok {
		t.Error("IP on the first blocklist was accepted")
	}
	if ok, _ := ct.CanAccept(addr("203.0.113.41")); !ok {
		t.Error("IP absent from the first blocklist was refused")
	}

	ct.SetIPListPaths(second, "")

	if ok, _ := ct.CanAccept(addr("203.0.113.41")); ok {
		t.Error("IP on the second blocklist was accepted after the path change")
	}
	if ok, _ := ct.CanAccept(addr("203.0.113.40")); !ok {
		t.Error("IP from the old blocklist still refused after the path change")
	}
}

// TestClearingIPListPathDropsList covers removing a blocklist path in
// config.json: the previously loaded entries must stop filtering.
func TestClearingIPListPathDropsList(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "block.txt")
	if err := os.WriteFile(path, []byte("203.0.113.50\n"), 0644); err != nil {
		t.Fatal(err)
	}

	ct := newConfigTracker(0, 0)
	ct.SetIPListPaths(path, "")
	defer ct.StopWatching()

	if ok, _ := ct.CanAccept(addr("203.0.113.50")); ok {
		t.Fatal("blocked IP accepted while the blocklist was configured")
	}

	ct.SetIPListPaths("", "")

	if ok, reason := ct.CanAccept(addr("203.0.113.50")); !ok {
		t.Errorf("IP still blocked after clearing the blocklist path: %s", reason)
	}
}

// TestSetIPListPathsUnchangedIsNoOp guards against tearing down and restarting
// the file watcher on every config.json reload.
func TestSetIPListPathsUnchangedIsNoOp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "block.txt")
	if err := os.WriteFile(path, []byte("203.0.113.60\n"), 0644); err != nil {
		t.Fatal(err)
	}

	ct := newConfigTracker(0, 0)
	ct.SetIPListPaths(path, "")
	defer ct.StopWatching()

	ct.mu.Lock()
	before := ct.watcher
	ct.mu.Unlock()

	ct.SetIPListPaths(path, "") // same paths

	ct.mu.Lock()
	after := ct.watcher
	ct.mu.Unlock()

	if before != after {
		t.Error("watcher was restarted despite the paths being unchanged")
	}
}

// TestStopWatchingIsIdempotent covers a latent panic: StopWatching closed its
// stop channel unconditionally, so the second call (shutdown, after a path
// change restarted the watcher) closed an already-closed channel.
func TestStopWatchingIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "block.txt")
	if err := os.WriteFile(path, []byte("203.0.113.70\n"), 0644); err != nil {
		t.Fatal(err)
	}

	ct := newConfigTracker(0, 0)
	ct.SetIPListPaths(path, "")

	ct.StopWatching()
	ct.StopWatching() // must not panic
}

// TestStopWatchingWithoutWatcher covers a tracker configured with no IP list
// files at all, where startWatching never created a watcher.
func TestStopWatchingWithoutWatcher(t *testing.T) {
	ct := newConfigTracker(0, 0)
	ct.StopWatching() // must not panic
}
