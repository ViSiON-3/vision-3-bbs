//go:build windows

package filelock

import (
	"os"

	"golang.org/x/sys/windows"
)

// tryLock takes a non-blocking exclusive lock via LockFileEx, reporting
// whether it was acquired. One byte is enough: every participant locks the
// same range, so the range only has to be consistent, not cover the file.
func tryLock(f *os.File) bool {
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

// unlock releases the LockFileEx lock over the same one-byte range.
func unlock(f *os.File) {
	ol := new(windows.Overlapped)
	_ = windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, ol)
}
