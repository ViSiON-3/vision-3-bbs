package ziplab

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// Locating and reading FILE_ID.DIZ from an extracted archive.

// findAndReadDIZ searches for FILE_ID.ANS (preferred) or FILE_ID.DIZ
// (case-insensitive) in the work directory and one level of subdirectories.
func (p *Processor) findAndReadDIZ(workDir string) string {
	var ansPath, dizPath string
	_ = filepath.WalkDir(workDir, func(path string, d os.DirEntry, err error) error { // callback never returns an error
		if err != nil {
			return nil
		}
		// Descend into workDir's own subdirectories ("a") but no further
		// ("a/b"): a DIZ deeper down belongs to something bundled inside.
		rel, _ := filepath.Rel(workDir, path)
		if d.IsDir() && strings.Contains(rel, string(filepath.Separator)) {
			return filepath.SkipDir
		}
		if !d.IsDir() {
			switch {
			case strings.EqualFold(d.Name(), "FILE_ID.ANS"):
				ansPath = path
			case strings.EqualFold(d.Name(), "FILE_ID.DIZ") && ansPath == "":
				dizPath = path
			}
		}
		return nil
	})

	target := ansPath
	if target == "" {
		target = dizPath
	}
	if target == "" {
		return ""
	}

	data, readErr := os.ReadFile(target)
	if readErr != nil {
		slog.Warn("found diz file but failed to read", "file", filepath.Base(target), "error", readErr)
		return ""
	}
	return cleanDIZ(string(stripSauceMetadata(data)))
}
