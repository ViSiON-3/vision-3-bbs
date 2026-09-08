//go:build windows

package atomicfile

import (
	"errors"
	"os"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

// replace renames src onto dst, retrying while another handle blocks it.
//
// Windows refuses to replace a file that anyone else has open unless every
// holder opened it with FILE_SHARE_DELETE, and Go's os.Open and os.ReadFile do
// not ask for that. So a reader that merely reads users.json for a few
// microseconds is enough to make a concurrent save fail with "Access is
// denied" -- on a multi-node BBS, routine traffic rather than a rare race.
//
// Retrying is a treatment rather than a cure: the cure is for every reader to
// open with FILE_SHARE_DELETE, which needs a Windows-specific open path that
// os.ReadFile does not offer. What retrying buys is that a save no longer fails
// because a reader happened to be mid-read, which is the failure that actually
// bites. A destination held open indefinitely still fails, and should.
func replace(src, dst string) error {
	var err error
	for attempt := 0; attempt < replaceAttempts; attempt++ {
		err = os.Rename(src, dst)
		if err == nil || !blockedByAnotherHandle(err) {
			return err
		}
		time.Sleep(replacePause)
	}
	return err
}

// blockedByAnotherHandle reports whether err is Windows refusing the rename
// because someone else has the file open, as opposed to a real permission or
// path problem that retrying cannot help.
func blockedByAnotherHandle(err error) bool {
	var errno syscall.Errno
	if !errors.As(err, &errno) {
		return false
	}
	switch errno {
	case syscall.Errno(windows.ERROR_ACCESS_DENIED),
		syscall.Errno(windows.ERROR_SHARING_VIOLATION),
		syscall.Errno(windows.ERROR_LOCK_VIOLATION):
		return true
	}
	return false
}
