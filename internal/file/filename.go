package file

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

// validateFilename checks that name is a plain file name that stays inside the
// area directory it will be joined to, and returns it unchanged.
//
// File records are joined against an area path as filepath.Join(base, area, name),
// so anything carrying a separator -- or the literal ".." -- escapes the area.
// filepath.Base is not enough on its own: it returns ".." for "..", which lands
// on the area's parent directory.
//
// Names that merely contain ".." (patch..v2.zip) are fine: once separators are
// rejected there is nothing left for them to traverse.
func validateFilename(name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("filename is empty")
	}
	if name == "." || name == ".." {
		return "", fmt.Errorf("invalid filename %q: refers to a directory", name)
	}
	// Reject both separators regardless of host OS: records may have been
	// written by a Windows node or imported from a foreign file list.
	if strings.ContainsAny(name, `/\`) {
		return "", fmt.Errorf("invalid filename %q: contains a path separator", name)
	}
	if strings.ContainsRune(name, 0) {
		return "", fmt.Errorf("invalid filename %q: contains a NUL byte", name)
	}
	if filepath.Base(name) != name {
		return "", fmt.Errorf("invalid filename %q: not a plain file name", name)
	}
	return name, nil
}

// removeRecordByID returns records without the entry whose ID matches, leaving
// the remaining order intact. Removing by identity rather than by position
// matters during rollback: muFiles is released while records are persisted, so
// the slice may have grown since the index was taken.
func removeRecordByID(records []FileRecord, id uuid.UUID) []FileRecord {
	out := make([]FileRecord, 0, len(records))
	for _, r := range records {
		if r.ID == id {
			continue
		}
		out = append(out, r)
	}
	return out
}
