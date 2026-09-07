package user

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/filelock"
)

// saveUsersLocked performs the actual saving without acquiring locks.
// Uses um.path (which should point to data/users.json)
func (um *UserMgr) saveUsersLocked() error { // Receiver uses renamed type
	// The lock sidecar lives beside users.json, so the directory has to exist
	// before the acquire. On a fresh install it does not yet, and taking the
	// lock first would fail and drop the very first save back to unlocked.
	dir := filepath.Dir(um.path)
	if err := os.MkdirAll(dir, 0750); err != nil { // Use 0750 for permissions
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	// Hold the cross-process lock across the whole read-merge-write cycle.
	//
	// Detecting an external edit and then folding it in is only safe if no one
	// writes between the two. ./ue takes this same lock around its own
	// check-and-save, so the sequence below cannot interleave with it and
	// overwrite an edit made after our fingerprint check -- the narrow window
	// left open by the merge itself.
	//
	// If the lock cannot be taken we save anyway rather than lose the
	// session's data. That returns us to the pre-lock behaviour, which is a
	// small race, and is much better than dropping the write outright. It is
	// logged because a persistent failure here means the editor is wedged or
	// the data directory is not writable.
	lock, lockErr := filelock.Acquire(um.path, filelock.DefaultTimeout)
	if lockErr != nil {
		slog.Warn("saving users without the cross-process lock; a concurrent ./ue write could be overwritten",
			"path", um.path, "error", lockErr)
	}
	defer lock.Release() // nil-safe: no-op when the acquire above failed

	// This rewrites the whole file from memory, so anything a separate process
	// wrote since we last read it would be lost. Fold those edits in first.
	// ./ue is the one that does this, when a sysop changes a level or
	// validates an account while the BBS is running.
	if um.externallyModified() {
		um.mergeExternalEdits()
	}

	// Convert map back to slice for saving as JSON array.
	// Clear LegacyUsername before marshaling so the old "username" key is not written back.
	usersList := make([]*User, 0, len(um.users))
	for _, user := range um.users {
		if user.LegacyUsername != "" {
			cp := *user
			cp.LegacyUsername = ""
			usersList = append(usersList, &cp)
		} else {
			usersList = append(usersList, user)
		}
	}

	data, err := json.MarshalIndent(usersList, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal users slice: %w", err)
	}

	// Written temp-file-then-rename, so a crash mid-write cannot leave a
	// truncated users.json: readers see the whole old file or the whole new
	// one. See writeFileAtomic.
	if err = writeFileAtomic(um.path, data, 0600); err != nil {
		return fmt.Errorf("failed to write users file %s: %w", um.path, err) // Include path in error
	}
	// Record what we just wrote, so the next save does not mistake our own
	// write for somebody else's edit. Fingerprint the bytes we produced rather
	// than re-reading: identical content, and it cannot pick up a write that
	// landed in between.
	um.fileState = fileFingerprint{size: int64(len(data)), sum: sha256.Sum256(data)}
	// Everything now in the map has reached disk, so a later absence from the
	// file means an external delete rather than a registration in flight.
	for _, u := range um.users {
		u.persisted = true
	}
	return nil
}

// SaveUsers saves the current user data to the JSON file (acquires lock).
func (um *UserMgr) SaveUsers() error { // Receiver uses renamed type
	um.mu.Lock()
	defer um.mu.Unlock()
	return um.saveUsersLocked()
}

// LogAdminActivity logs an administrative action to the activity log file
func (um *UserMgr) LogAdminActivity(logEntry AdminActivityLog) error {
	um.mu.Lock()
	defer um.mu.Unlock()

	// Load existing logs
	logPath := filepath.Join(filepath.Dir(um.path), adminLogFile)
	var logs []AdminActivityLog

	// Try to load existing logs
	if data, err := os.ReadFile(logPath); err == nil {
		data = StripUTF8BOM(data)
		_ = json.Unmarshal(data, &logs) // Ignore errors, start fresh if corrupt
	}

	// Add new entry
	logEntry.ID = len(logs) + 1
	// UTC to match AdminActivityLogEntry in admin_log.go, which stamps
	// time.Now().UTC(). Mixing zones in one log file silently misorders entries
	// across a DST boundary for anyone reconstructing an incident.
	logEntry.Timestamp = time.Now().UTC()
	logs = append(logs, logEntry)

	// Keep only recent entries (prevent file from growing indefinitely)
	if len(logs) > adminLogLimit {
		logs = logs[len(logs)-adminLogLimit:]
	}

	// Save logs
	data, err := json.MarshalIndent(logs, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal admin logs: %w", err)
	}

	if err := os.WriteFile(logPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write admin log: %w", err)
	}

	return nil
}
