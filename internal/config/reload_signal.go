package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// ReloadSentinelName is the semaphore file a config-writing tool touches to
// tell a running BBS that configuration changed and should be re-read.
//
// The running BBS also polls the individual config files' modification times,
// so the sentinel is not strictly required for a change to be noticed. It
// exists because a tool that rewrites several files in one save (v3config
// writes eleven) can signal "I am finished" exactly once, after every write
// has succeeded, instead of leaving the BBS to infer completion from a burst
// of separate file modifications. A save that fails partway never touches it.
//
// Sysops can also trigger a reload by hand:
//
//	touch configs/reload.now
//
// This mirrors Synchronet's ctrl/recycle semaphore.
const ReloadSentinelName = "reload.now"

// ReloadSentinelPath returns the full path to the reload sentinel within the
// given config directory.
func ReloadSentinelPath(configPath string) string {
	return filepath.Join(configPath, ReloadSentinelName)
}

// TouchReloadSentinel creates or updates the modification time of the reload
// sentinel, signalling a running BBS to re-read its configuration.
//
// Callers should invoke this only after every file in a save has been written
// successfully, so a partial save is never signalled as complete.
func TouchReloadSentinel(configPath string) error {
	path := ReloadSentinelPath(configPath)

	// Create it if absent. An empty file is enough — only the timestamp is read.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("creating reload sentinel %s: %w", path, err)
	}
	if cerr := f.Close(); cerr != nil {
		return fmt.Errorf("closing reload sentinel %s: %w", path, cerr)
	}

	// Bump the timestamp explicitly: an existing file opened without O_TRUNC
	// keeps its old mtime, and the timestamp is the whole signal.
	now := time.Now()
	if err := os.Chtimes(path, now, now); err != nil {
		return fmt.Errorf("updating reload sentinel %s: %w", path, err)
	}
	return nil
}

// ReloadForceSentinelName is the semaphore a sysop touches to apply DEFERRED
// configuration changes immediately, without waiting for the board to go
// idle, and to re-read everything else at the same time:
//
//	touch configs/reload.force
//
// Unlike ReloadSentinelName, no tool touches this automatically — structural
// changes (file areas, and eventually message areas) normally wait for zero
// active sessions because applying them mid-session can pull state out from
// under a caller. The force sentinel is the explicit "I know, do it now".
const ReloadForceSentinelName = "reload.force"

// ReloadForceSentinelPath returns the full path to the force sentinel within
// the given config directory.
func ReloadForceSentinelPath(configPath string) string {
	return filepath.Join(configPath, ReloadForceSentinelName)
}
