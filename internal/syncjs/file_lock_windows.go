//go:build windows

package syncjs

import (
	"golang.org/x/sys/windows"

	"github.com/dop251/goja"
)

// fileLock implements byte-range locking using LockFileEx/UnlockFileEx on Windows.
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

	handle := windows.Handle(jf.f.Fd())
	offsetLow := uint32(offset)
	offsetHigh := uint32(offset >> 32)
	lengthLow := uint32(length)
	lengthHigh := uint32(length >> 32)

	if doLock {
		// LOCKFILE_EXCLUSIVE_LOCK | LOCKFILE_FAIL_IMMEDIATELY
		ol := &windows.Overlapped{Offset: offsetLow, OffsetHigh: offsetHigh}
		err := windows.LockFileEx(handle, windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, lengthLow, lengthHigh, ol)
		return err == nil
	}

	ol := &windows.Overlapped{Offset: offsetLow, OffsetHigh: offsetHigh}
	err := windows.UnlockFileEx(handle, 0, lengthLow, lengthHigh, ol)
	return err == nil
}
