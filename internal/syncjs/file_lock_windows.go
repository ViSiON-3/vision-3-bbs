//go:build windows

package syncjs

import (
	"log"

	"github.com/dop251/goja"
)

// fileLock is a stub on Windows; fcntl byte-range locks are not available.
func (jf *jsFile) fileLock(call goja.FunctionCall, doLock bool) bool {
	if jf.f == nil {
		return false
	}
	log.Printf("WARN: SyncJS: file locking is not supported on Windows")
	return false
}
