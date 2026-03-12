package menu

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// ErrDoorBusy is returned when a single-instance door is already in use.
var ErrDoorBusy = errors.New("door is currently in use by another node")

// doorLocksMu protects the doorLockFiles and doorLockNodes maps.
// File-based locking provides cross-process safety; the maps track
// open file handles so they can be unlocked and closed on release.
var (
	doorLocksMu   sync.Mutex
	doorLockFiles = make(map[string]*os.File) // door name (uppercase) -> lock file handle
	doorLockNodes = make(map[string]int)      // door name (uppercase) -> node number
)

// doorLockDir returns the directory used for door lock files.
func doorLockDir() string {
	return filepath.Join(os.TempDir(), "vision3_doorlocks")
}

// acquireDoorLock attempts to acquire an exclusive file-based lock for the given door.
// Returns nil on success, ErrDoorBusy if another node/process holds the lock,
// or a different error for filesystem/permission failures.
// Door names are normalized to uppercase for consistent locking.
func acquireDoorLock(doorName string, nodeNumber int) error {
	doorLocksMu.Lock()
	defer doorLocksMu.Unlock()

	key := strings.ToUpper(doorName)

	// If this node already holds the lock, allow re-entry
	if holder, exists := doorLockNodes[key]; exists && holder == nodeNumber {
		return nil
	}

	lockDir := doorLockDir()
	if err := os.MkdirAll(lockDir, 0700); err != nil {
		return fmt.Errorf("failed to create door lock directory %s: %w", lockDir, err)
	}

	lockPath := filepath.Join(lockDir, key+".lock")
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return fmt.Errorf("failed to open door lock file %s: %w", lockPath, err)
	}

	if !tryFileLock(f) {
		f.Close()
		return ErrDoorBusy
	}

	// Write node/pid info for debugging stale locks
	f.Truncate(0)
	f.Seek(0, 0)
	fmt.Fprintf(f, "node=%d\npid=%d\n", nodeNumber, os.Getpid())
	f.Sync()

	doorLockFiles[key] = f
	doorLockNodes[key] = nodeNumber
	log.Printf("DEBUG: Acquired file lock for door %s (node %d, pid %d)", key, nodeNumber, os.Getpid())
	return nil
}

// releaseDoorLock releases the file-based lock for the given door if held by the specified node.
// Door names are normalized to uppercase for consistent locking.
func releaseDoorLock(doorName string, nodeNumber int) {
	doorLocksMu.Lock()
	defer doorLocksMu.Unlock()

	key := strings.ToUpper(doorName)
	if holder, exists := doorLockNodes[key]; exists && holder == nodeNumber {
		if f, ok := doorLockFiles[key]; ok {
			releaseFileLock(f)
			f.Close()
			// Lock file is intentionally NOT deleted: removing it after unlock
			// creates a race where another process can lock the same path between
			// our unlock and remove, then our remove unlinks their lock file.
			delete(doorLockFiles, key)
		}
		delete(doorLockNodes, key)
		log.Printf("DEBUG: Released file lock for door %s (node %d)", key, nodeNumber)
	}
}
