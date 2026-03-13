//go:build !windows

package syncjs

import (
	"io"
	"syscall"

	"github.com/dop251/goja"
)

// fileLock implements byte-range locking using fcntl on Unix systems.
func (jf *jsFile) fileLock(call goja.FunctionCall, doLock bool) bool {
	if jf.f == nil {
		return false
	}
	offset := int64(0)
	length := int64(0)
	if len(call.Arguments) > 0 {
		offset = call.Arguments[0].ToInteger()
	}
	if len(call.Arguments) > 1 {
		length = call.Arguments[1].ToInteger()
	}

	lockType := syscall.F_UNLCK
	if doLock {
		lockType = syscall.F_WRLCK
	}

	flock := syscall.Flock_t{
		Type:   int16(lockType),
		Whence: io.SeekStart,
		Start:  offset,
		Len:    length,
	}
	err := syscall.FcntlFlock(jf.f.Fd(), syscall.F_SETLKW, &flock)
	return err == nil
}
