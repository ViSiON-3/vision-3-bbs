// Package atomicfile replaces a file in one step, so a reader sees either the
// whole old contents or the whole new ones and never a partial write.
//
// The write half is the familiar temp-file-then-rename dance, which several
// packages had each grown their own copy of. The replace half is the part that
// needed a home: os.Rename is not the whole story on Windows, where the call
// fails outright while any other handle to the destination is open.
package atomicfile

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Retry budget for a rename blocked by another handle. Only the Windows
// implementation waits, but the values live here so the shared test can state
// how long it has to hold a file for the retry to be under test at all.
//
// A reader holds a file only for as long as it takes to read it, so the block
// is brief. Half a second is far longer than any read of a config or user file,
// and still short enough that a genuine permission problem surfaces promptly
// rather than hanging a save.
const (
	replaceAttempts = 20
	replacePause    = 25 * time.Millisecond
)

// Replace moves src onto dst, replacing dst if it exists.
//
// On unix this is os.Rename. On Windows it retries briefly, because a rename
// there fails while another process holds the destination open -- see the
// commentary in replace_windows.go.
func Replace(src, dst string) error {
	return replace(src, dst)
}

// WriteFile writes data to path by way of a temp file in the same directory,
// then replaces path with it.
//
// os.WriteFile truncates the target and then writes, so a crash -- or a full
// disk -- between the two leaves a truncated file, and whatever did not make it
// is simply gone.
//
// The temp file has to live in the destination directory: a rename is only
// atomic within a filesystem, and os.TempDir may well be on another one.
func WriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file in %s: %w", dir, err)
	}
	tmpPath := tmp.Name()

	// Any failure from here on leaves the original untouched; clean up the
	// temp file so a run of failed saves does not litter the directory.
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}

	if _, err := tmp.Write(data); err != nil {
		cleanup()
		return fmt.Errorf("write temp file %s: %w", tmpPath, err)
	}
	// Flush to the device before the rename. Without this the rename can reach
	// disk while the contents have not, and a crash in that window leaves a
	// correctly-named file full of zeroes -- worse than the truncation this is
	// meant to prevent, because it looks intact.
	if err := tmp.Sync(); err != nil {
		cleanup()
		return fmt.Errorf("sync temp file %s: %w", tmpPath, err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close temp file %s: %w", tmpPath, err)
	}
	// CreateTemp makes the file 0600; set the caller's mode explicitly so the
	// result does not depend on that default. Windows has no mode bits to set,
	// and Chmod there only toggles the read-only attribute, which is harmless.
	if err := os.Chmod(tmpPath, perm); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("chmod temp file %s: %w", tmpPath, err)
	}
	if err := Replace(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename %s to %s: %w", tmpPath, path, err)
	}
	return nil
}
