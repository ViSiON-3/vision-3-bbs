//go:build !windows

package filelock

import (
	"os"
	"syscall"
)

// tryLock takes a non-blocking exclusive flock, reporting whether it was
// acquired. Non-blocking so the caller can impose its own timeout.
func tryLock(f *os.File) bool {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) == nil
}

// unlock drops the flock. The kernel would also drop it on close, but doing it
// explicitly keeps the release ordering the same on both platforms.
func unlock(f *os.File) {
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}
