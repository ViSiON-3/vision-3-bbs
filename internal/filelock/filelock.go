// Package filelock provides an advisory cross-process lock, used to serialise
// read-modify-write cycles on a shared data file between the BBS and the
// separate editor binaries.
//
// The lock is advisory: it only excludes processes that ask for it. It is not
// a permission check and does not stop an unrelated program from writing the
// file underneath us.
//
// internal/menu keeps its own lock helpers for single-instance doors. That is
// a different job -- a reservation held for as long as a door runs, carrying
// node and pid metadata inside the lock file for diagnosing stale ones -- and
// folding it in here would mean exposing the file handle just for that. These
// two are deliberately separate rather than an oversight.
package filelock

import (
	"fmt"
	"os"
	"time"
)

// DefaultTimeout is a sensible wait for a data-file lock. Holders take the
// lock across a read-merge-write of a small JSON file, which is milliseconds,
// so anything approaching this means a wedged or stopped process rather than
// ordinary contention.
const DefaultTimeout = 5 * time.Second

// retryInterval is how often a blocked acquire re-tries. The underlying
// primitives are non-blocking so that Acquire can honour a timeout at all;
// polling is what buys that. Short enough to be imperceptible, long enough not
// to spin a core while waiting.
const retryInterval = 20 * time.Millisecond

// Lock is a held advisory lock. Release it when the critical section ends.
type Lock struct {
	f *os.File
}

// SidecarPath returns the lock file path guarding dataPath.
//
// The lock deliberately lives on a sidecar rather than on the data file
// itself. Writers here update the data file atomically, by writing a temp file
// and renaming it into place, which replaces the inode. A lock taken on the
// data file is attached to the open file description behind the *old* inode,
// so the instant a writer renamed over it the two processes would be holding
// locks on different objects and both would proceed. The sidecar is never
// renamed, so every participant contends on the same inode.
func SidecarPath(dataPath string) string {
	return dataPath + ".lock"
}

// Acquire takes an exclusive lock guarding dataPath, waiting up to timeout.
// A timeout of zero or less tries exactly once.
//
// The returned error distinguishes nothing about why the wait failed on
// purpose: to a caller, "someone else holds it" and "we waited long enough"
// are the same decision.
func Acquire(dataPath string, timeout time.Duration) (*Lock, error) {
	lockPath := SidecarPath(dataPath)
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("open lock file %s: %w", lockPath, err)
	}

	deadline := time.Now().Add(timeout)
	for {
		if tryLock(f) {
			return &Lock{f: f}, nil
		}
		if !time.Now().Before(deadline) {
			_ = f.Close() // not holding the lock; nothing to release
			return nil, fmt.Errorf("timed out after %s waiting for lock %s", timeout, lockPath)
		}
		time.Sleep(retryInterval)
	}
}

// Release drops the lock. Safe to call on a nil Lock so callers can defer it
// next to an acquire whose error they handle by carrying on unlocked.
//
// The lock file is deliberately left on disk. Removing it after unlocking
// opens a race where another process locks the same path between our unlock
// and our remove, and we then unlink the file they are holding — after which a
// third process creates a fresh one and two writers believe they hold the same
// lock. An empty sidecar file costs nothing.
func (l *Lock) Release() {
	if l == nil || l.f == nil {
		return
	}
	unlock(l.f)
	_ = l.f.Close() // lock already dropped above
	l.f = nil
}
