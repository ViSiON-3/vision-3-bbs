//go:build !windows

package atomicfile

import "os"

// replace is a plain rename. On unix an open handle does not block one: the
// directory entry is swapped and any reader keeps the inode it already opened.
func replace(src, dst string) error {
	return os.Rename(src, dst)
}

// syncDir flushes dir's entries, so a rename into it survives a power loss.
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	syncErr := d.Sync()
	if err := d.Close(); syncErr == nil {
		syncErr = err
	}
	return syncErr
}
