package user

import (
	"fmt"
	"os"
	"path/filepath"
)

// writeFileAtomic writes data to path by way of a temp file in the same
// directory, then renames it into place.
//
// os.WriteFile truncates the target and then writes, so a crash — or a full
// disk — between the two leaves a truncated or half-written users.json, and
// the accounts that did not make it are simply gone. Rename is atomic on both
// unix and Windows, so a reader sees either the whole old file or the whole
// new one and never a partial write.
//
// The temp file has to live in the destination directory: rename is only
// atomic within a filesystem, and os.TempDir may well be on another one.
//
// ./ue has written this way since it was built (internal/usereditor/fileio.go);
// this brings the BBS into line with it.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file in %s: %w", dir, err)
	}
	tmpPath := tmp.Name()

	// Any failure from here on leaves the original untouched; clean up the
	// temp file so a run of failed saves does not litter the data directory.
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
	// correctly-named file full of zeroes — worse than the truncation this is
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
	// result does not depend on that default.
	if err := os.Chmod(tmpPath, perm); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("chmod temp file %s: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename %s to %s: %w", tmpPath, path, err)
	}
	return nil
}
