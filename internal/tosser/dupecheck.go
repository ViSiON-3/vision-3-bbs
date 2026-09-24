package tosser

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/atomicfile"
	"github.com/ViSiON-3/vision-3-bbs/internal/filelock"
)

// DupeDB tracks seen MSGIDs to prevent duplicate message tossing.
// It persists to a JSON file on disk.
type DupeDB struct {
	mu      sync.Mutex
	path    string
	entries map[string]int64 // MSGID -> Unix timestamp when first seen
	maxAge  time.Duration    // How long to keep entries
}

// dupeFile is the on-disk representation.
type dupeFile struct {
	Entries map[string]int64 `json:"entries"`
}

// NewDupeDB creates or loads a duplicate database.
func NewDupeDB(path string, maxAge time.Duration) (*DupeDB, error) {
	db := &DupeDB{
		path:    path,
		entries: make(map[string]int64),
		maxAge:  maxAge,
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return db, nil // Fresh database
		}
		return nil, err
	}

	if len(data) > 0 {
		var f dupeFile
		if err := json.Unmarshal(data, &f); err != nil {
			slog.Warn("corrupt dupe DB, starting fresh", "path", path, "error", err)
			return db, nil
		}
		if f.Entries != nil {
			db.entries = f.Entries
		}
	}

	return db, nil
}

// IsDupe returns true if the MSGID has been seen before.
func (db *DupeDB) IsDupe(msgID string) bool {
	if msgID == "" {
		return false // No MSGID = can't dupe-check
	}
	db.mu.Lock()
	defer db.mu.Unlock()
	_, exists := db.entries[msgID]
	return exists
}

// Add records a MSGID as seen. Returns true if it was already a dupe.
func (db *DupeDB) Add(msgID string) bool {
	if msgID == "" {
		return false
	}
	db.mu.Lock()
	defer db.mu.Unlock()
	if _, exists := db.entries[msgID]; exists {
		return true
	}
	db.entries[msgID] = time.Now().Unix()
	return false
}

// Purge removes entries older than maxAge and saves to disk.
func (db *DupeDB) Purge() error {
	db.mu.Lock()
	defer db.mu.Unlock()

	cutoff := time.Now().Add(-db.maxAge).Unix()
	for id, ts := range db.entries {
		if ts < cutoff {
			delete(db.entries, id)
		}
	}

	return db.saveLocked()
}

// Reload folds in entries other processes have saved since this one
// loaded the file. A caller that serialises its own work with other
// processes (a lock around a whole toss) calls it after taking that lock,
// so its dupe checks see everything already imported.
func (db *DupeDB) Reload() {
	db.mu.Lock()
	defer db.mu.Unlock()
	db.mergeFromDiskLocked()
}

// Save persists the database to disk.
func (db *DupeDB) Save() error {
	db.mu.Lock()
	defer db.mu.Unlock()
	return db.saveLocked()
}

// saveLocked writes the database, first folding in whatever another process
// saved since this one loaded it. Several processes can hold the same file
// (one v3mail run per QWK network, polled on the same schedule), and a plain
// rewrite from memory would drop every entry the others added. The merge runs
// under a cross-process lock so no save lands between the read and the write.
func (db *DupeDB) saveLocked() error {
	// The lock sidecar sits beside the file, so the directory must exist.
	if err := os.MkdirAll(filepath.Dir(db.path), 0o755); err != nil {
		return err
	}
	lock, lockErr := filelock.Acquire(db.path, filelock.DefaultTimeout)
	if lockErr != nil {
		slog.Warn("saving dupe DB without the cross-process lock; a concurrent save could be lost",
			"path", db.path, "error", lockErr)
	}
	defer lock.Release() // nil-safe: no-op when the acquire above failed

	db.mergeFromDiskLocked()

	f := dupeFile{Entries: db.entries}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return atomicWriteFile(db.path, data, 0644)
}

// mergeFromDiskLocked adds the on-disk entries this process does not have,
// skipping any older than maxAge so a Purge here is not undone by the merge.
// Where both sides know an ID, the earlier first-seen time is kept.
func (db *DupeDB) mergeFromDiskLocked() {
	data, err := os.ReadFile(db.path)
	if err != nil {
		if !os.IsNotExist(err) {
			slog.Warn("could not re-read dupe DB before saving; entries saved by other processes may be lost",
				"path", db.path, "error", err)
		}
		return
	}
	if len(data) == 0 {
		return
	}
	var f dupeFile
	if err := json.Unmarshal(data, &f); err != nil {
		slog.Warn("corrupt dupe DB on disk, overwriting it", "path", db.path, "error", err)
		return
	}
	var cutoff int64
	if db.maxAge > 0 {
		cutoff = time.Now().Add(-db.maxAge).Unix()
	}
	for id, ts := range f.Entries {
		if ts < cutoff {
			continue
		}
		if cur, ok := db.entries[id]; !ok || ts < cur {
			db.entries[id] = ts
		}
	}
}

// atomicWriteFile writes data to a temp file then renames it to path,
// ensuring the target file is never left in a partially-written state.
func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	return atomicfile.WriteFile(path, data, perm)
}

// Count returns the number of entries in the database.
func (db *DupeDB) Count() int {
	db.mu.Lock()
	defer db.mu.Unlock()
	return len(db.entries)
}
