package ziplab

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// Locating and reading FILE_ID.DIZ from an extracted archive.

// findAndReadDIZ reads the file description from an extracted archive in
// workDir, choosing among FILE_ID.ANS and FILE_ID.DIZ (case-insensitive) in
// workDir and its immediate subdirectories as dizRank does. It returns "" if
// there is none.
func (p *Processor) findAndReadDIZ(workDir string) string {
	var target string
	bestRank := 0
	_ = filepath.WalkDir(workDir, func(path string, d os.DirEntry, err error) error { // callback never returns an error
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(workDir, path)
		depth := strings.Count(rel, string(filepath.Separator))
		if d.IsDir() {
			// Descend into workDir's own subdirectories ("a") but no further.
			if depth > 0 {
				return filepath.SkipDir
			}
			return nil
		}
		if rank, ok := dizRank(d.Name(), depth); ok && (target == "" || rank < bestRank) {
			target, bestRank = path, rank
		}
		return nil
	})

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
