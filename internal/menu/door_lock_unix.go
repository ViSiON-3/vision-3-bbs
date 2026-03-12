//go:build !windows

package menu

import (
	"os"
	"syscall"
)

// tryFileLock attempts a non-blocking exclusive flock on the file.
// Returns true if the lock was acquired, false if another process holds it.
func tryFileLock(f *os.File) bool {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) == nil
}

// releaseFileLock releases the flock on the file.
func releaseFileLock(f *os.File) {
	syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}
