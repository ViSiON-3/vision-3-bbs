package ziplab

import (
	"archive/zip"
	"bufio"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// Removing advertising files from an uploaded archive: which names match, and
// stripping them from the zip or the extracted work directory.

// removeFilesFromZip rewrites a ZIP excluding entries that match any of the patterns (case-insensitive).
func (p *Processor) removeFilesFromZip(zipPath string, patterns []string) (retErr error) {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("failed to open zip: %w", err)
	}
	defer func() { _ = r.Close() }() // read-only zip reader

	tmpPath := zipPath + ".tmp"
	outFile, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("failed to create temp zip: %w", err)
	}
	defer func() {
		_ = outFile.Close() // no-op after the explicit Close on the success path
		if retErr != nil {
			_ = os.Remove(tmpPath) // cleanup on error path
		}
	}()

	w := zip.NewWriter(outFile)
	if r.Comment != "" {
		_ = w.SetComment(r.Comment) // came from a valid zip, so it always fits
	}

	removed := 0
	seen := make(map[string]bool)
	for _, f := range r.File {
		if shouldRemoveFile(f.Name, patterns) {
			slog.Info("removing ad file from archive", "file", f.Name)
			removed++
			continue
		}
		if seen[f.Name] {
			continue
		}
		seen[f.Name] = true

		if err := copyZipEntryRaw(w, f); err != nil {
			_ = w.Close() // cleanup on error path
			return fmt.Errorf("failed to copy entry %s: %w", f.Name, err)
		}
	}

	if err := w.Close(); err != nil {
		return fmt.Errorf("failed to finalize zip: %w", err)
	}

	if removed == 0 {
		// Close before removing: some platforms can't delete open files.
		_ = outFile.Close()    // discarding the file; close error is moot
		_ = os.Remove(tmpPath) // nothing changed; discard temp copy
		retErr = nil
		return nil
	}

	if err := outFile.Close(); err != nil {
		return fmt.Errorf("failed to close temp zip: %w", err)
	}
	return os.Rename(tmpPath, zipPath)
}

// shouldRemoveFile checks if a filename matches any removal pattern (case-insensitive).
func shouldRemoveFile(name string, patterns []string) bool {
	baseName := filepath.Base(name)
	for _, pattern := range patterns {
		if strings.EqualFold(baseName, pattern) {
			return true
		}
	}
	return false
}

// loadRemovePatterns reads filenames to remove from the patterns file.
func (p *Processor) loadRemovePatterns() []string {
	patternsPath := p.resolvePath(p.config.Steps.RemoveAds.PatternsFile)
	if patternsPath == "" {
		return nil
	}

	f, err := os.Open(patternsPath)
	if err != nil {
		slog.Warn("could not open patterns file", "path", patternsPath, "error", err)
		return nil
	}
	defer func() { _ = f.Close() }() // read-only

	var patterns []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" && !strings.HasPrefix(line, ";") {
			patterns = append(patterns, line)
		}
	}
	return patterns
}

// removeMatchingFiles removes files matching a pattern (case-insensitive) from a directory.
func (p *Processor) removeMatchingFiles(dir, pattern string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() && strings.EqualFold(entry.Name(), pattern) {
			target := filepath.Join(dir, entry.Name())
			if err := os.Remove(target); err != nil {
				slog.Warn("failed to remove ad file", "path", target, "error", err)
			} else {
				slog.Info("removed ad file", "file", entry.Name())
			}
		}
	}
}
