//go:build windows

package menu

import (
	"os"

	"golang.org/x/sys/windows"
)

// tryFileLock attempts a non-blocking exclusive lock on the file using LockFileEx.
// Returns true if the lock was acquired, false if another process holds it.
func tryFileLock(f *os.File) bool {
	ol := new(windows.Overlapped)
	err := windows.LockFileEx(
		windows.Handle(f.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0,
		1, 0,
		ol,
	)
	return err == nil
}

// releaseFileLock releases the LockFileEx lock on the file.
func releaseFileLock(f *os.File) {
	ol := new(windows.Overlapped)
	windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, ol)
}
