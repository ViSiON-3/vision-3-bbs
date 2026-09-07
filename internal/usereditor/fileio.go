package usereditor

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/ViSiON-3/vision-3-bbs/internal/filelock"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"github.com/google/uuid"
)

// ErrFileChanged reports that users.json was written by someone else -- in
// practice a running BBS -- since this editor loaded it. Saving anyway would
// discard whatever they wrote, so the caller has to decide.
var ErrFileChanged = errors.New("users.json was modified externally since it was loaded")

// LoadUsers reads users.json and returns the user slice plus a fingerprint of
// the bytes it parsed, for detecting a later external write.
func LoadUsers(path string) ([]*user.User, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", fmt.Errorf("read %s: %w", path, err)
	}
	// Fingerprint what we actually read, not a separate stat: taking the two
	// from one set of bytes means they cannot disagree about which version of
	// the file this is.
	fingerprint := user.FingerprintBytes(data)
	data = user.StripUTF8BOM(data)

	var users []*user.User
	if err := json.Unmarshal(data, &users); err != nil {
		return nil, "", fmt.Errorf("unmarshal %s: %w", path, err)
	}

	// Sort by ID for consistent display
	sort.Slice(users, func(i, j int) bool {
		return users[i].ID < users[j].ID
	})

	return users, fingerprint, nil
}

// CheckFileChanged reports whether the file differs from the fingerprint taken
// when it was loaded.
//
// This compares contents rather than modification time. mtime resolution
// varies by filesystem, so a BBS write landing in the same tick as our load
// reads as unchanged and the next save silently overwrites it -- the exact
// loss this guard exists to prevent.
//
// An unreadable file reports unchanged: there is nothing there to overwrite,
// and refusing to save would strand the sysop's edits with no way out.
func CheckFileChanged(path string, storedFingerprint string) bool {
	current := user.FingerprintFile(path)
	if current == "" {
		return false
	}
	return current != storedFingerprint
}

// SaveUsers writes the user slice to disk atomically, taking the cross-process
// lock for the duration. Returns the fingerprint of what was written.
//
// This is the unconditional write. Callers that loaded the file earlier and
// could be overwriting someone else's work should use SaveUsersChecked.
func SaveUsers(path string, users []*user.User) (string, error) {
	lock, err := filelock.Acquire(path, filelock.DefaultTimeout)
	if err != nil {
		return "", fmt.Errorf("could not lock %s for writing: %w", path, err)
	}
	defer lock.Release()
	return writeUsers(path, users)
}

// SaveUsersChecked writes the user slice only if the file still matches the
// fingerprint it was loaded at, returning ErrFileChanged when it does not.
// Passing force writes regardless, discarding the other writer's changes.
//
// The check and the write happen under one lock. Checking and then writing as
// two separate steps leaves a window for the BBS to save in between, and the
// write that follows would destroy it -- the same data loss the check is
// there to catch, just through a narrower gap.
func SaveUsersChecked(path string, users []*user.User, loadedFingerprint string, force bool) (string, error) {
	lock, err := filelock.Acquire(path, filelock.DefaultTimeout)
	if err != nil {
		return "", fmt.Errorf("could not lock %s for writing: %w", path, err)
	}
	defer lock.Release()

	if !force && CheckFileChanged(path, loadedFingerprint) {
		return "", ErrFileChanged
	}
	return writeUsers(path, users)
}

// writeUsers marshals and atomically replaces the file. The caller must hold
// the lock.
func writeUsers(path string, users []*user.User) (string, error) {
	// Sort by ID before saving for consistent output
	sorted := make([]*user.User, len(users))
	copy(sorted, users)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].ID < sorted[j].ID
	})

	data, err := json.MarshalIndent(sorted, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal users: %w", err)
	}

	// Atomic write: write to temp file, then rename
	dir := filepath.Dir(path)
	tmpFile, err := os.CreateTemp(dir, "users-*.json.tmp")
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()    // cleanup on error path
		_ = os.Remove(tmpPath) // cleanup on error path
		return "", fmt.Errorf("write temp file: %w", err)
	}
	// Flush before the rename so a crash cannot leave a correctly-named file
	// of zeroes, which looks intact and is worse than an obvious truncation.
	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()    // cleanup on error path
		_ = os.Remove(tmpPath) // cleanup on error path
		return "", fmt.Errorf("sync temp file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath) // cleanup on error path
		return "", fmt.Errorf("close temp file: %w", err)
	}
	// Set the mode explicitly. os.CreateTemp asks for 0600 but the umask still
	// applies, so a restrictive one can leave the file unreadable -- and rename
	// carries that mode onto users.json, after which the BBS and this editor
	// both fail to load it. The BBS-side writer does the same.
	if err := os.Chmod(tmpPath, 0600); err != nil {
		_ = os.Remove(tmpPath) // cleanup on error path
		return "", fmt.Errorf("chmod temp file: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath) // cleanup on error path
		return "", fmt.Errorf("rename temp to %s: %w", path, err)
	}

	// Fingerprint the bytes just written rather than re-reading: identical
	// content, and it cannot pick up a write that landed in between.
	return user.FingerprintBytes(data), nil
}

// CloneUser creates a deep copy of a user record.
func CloneUser(u *user.User) *user.User {
	if u == nil {
		return nil
	}
	c := *u
	// Deep copy map fields
	if u.LastReadMessageIDs != nil {
		c.LastReadMessageIDs = make(map[int]string, len(u.LastReadMessageIDs))
		for k, v := range u.LastReadMessageIDs {
			c.LastReadMessageIDs[k] = v
		}
	}
	// Deep copy slice fields (uuid.UUID is [16]byte value type, simple copy works)
	if u.TaggedFileIDs != nil {
		c.TaggedFileIDs = make([]uuid.UUID, len(u.TaggedFileIDs))
		copy(c.TaggedFileIDs, u.TaggedFileIDs)
	}
	if u.TaggedMessageAreaTags != nil {
		c.TaggedMessageAreaTags = make([]string, len(u.TaggedMessageAreaTags))
		copy(c.TaggedMessageAreaTags, u.TaggedMessageAreaTags)
	}
	if u.DeletedAt != nil {
		t := *u.DeletedAt
		c.DeletedAt = &t
	}
	return &c
}
