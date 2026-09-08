//go:build !windows

package atomicfile

import "os"

// replace is a plain rename. On unix an open handle does not block one: the
// directory entry is swapped and any reader keeps the inode it already opened.
func replace(src, dst string) error {
	return os.Rename(src, dst)
}
