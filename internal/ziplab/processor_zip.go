package ziplab

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Zip primitives: integrity testing, extraction, and the rewrite helpers used
// to add a comment or a file to an existing archive.

// testZipIntegrity opens a ZIP and reads every entry to verify integrity.
func (p *Processor) testZipIntegrity(zipPath string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("failed to open zip %s: %w", zipPath, err)
	}
	defer func() { _ = r.Close() }() // read-only zip reader

	for _, f := range r.File {
		rc, err := f.Open()
		if err != nil {
			return fmt.Errorf("corrupt entry %s: %w", f.Name, err)
		}
		if _, err := io.Copy(io.Discard, rc); err != nil {
			_ = rc.Close() // read-only
			return fmt.Errorf("corrupt data in %s: %w", f.Name, err)
		}
		_ = rc.Close() // read-only
	}
	return nil
}

// extractZip extracts all files from a ZIP archive to destDir.
func (p *Processor) extractZip(zipPath, destDir string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("failed to open zip %s: %w", zipPath, err)
	}
	defer func() { _ = r.Close() }() // read-only zip reader

	for _, f := range r.File {
		targetPath := filepath.Join(destDir, f.Name)

		// Prevent zip slip
		if !strings.HasPrefix(filepath.Clean(targetPath), filepath.Clean(destDir)+string(os.PathSeparator)) {
			return fmt.Errorf("illegal file path in zip: %s", f.Name)
		}

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(targetPath, 0755); err != nil {
				return fmt.Errorf("failed to create directory %s: %w", targetPath, err)
			}
			continue
		}

		// Ensure parent directory exists
		if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
			return fmt.Errorf("failed to create parent directory for %s: %w", targetPath, err)
		}

		outFile, err := os.Create(targetPath)
		if err != nil {
			return fmt.Errorf("failed to create %s: %w", targetPath, err)
		}

		rc, err := f.Open()
		if err != nil {
			_ = outFile.Close() // cleanup on error path
			return fmt.Errorf("failed to open zip entry %s: %w", f.Name, err)
		}

		if _, err := io.Copy(outFile, rc); err != nil {
			_ = rc.Close()      // cleanup on error path
			_ = outFile.Close() // cleanup on error path
			return fmt.Errorf("failed to extract %s: %w", f.Name, err)
		}

		_ = rc.Close() // read side
		if err := outFile.Close(); err != nil {
			return fmt.Errorf("failed to finalize %s: %w", targetPath, err)
		}
	}
	return nil
}

// copyZipEntryRaw copies a ZIP entry without decompressing/recompressing.
// This preserves entries exactly as-is, avoiding checksum errors on entries
// with symlinks, resource forks, or other platform-specific features.
func copyZipEntryRaw(w *zip.Writer, f *zip.File) error {
	fh := f.FileHeader
	fw, err := w.CreateRaw(&fh)
	if err != nil {
		return err
	}
	rc, err := f.OpenRaw()
	if err != nil {
		return err
	}
	_, err = io.Copy(fw, rc)
	return err
}

// setZipComment rewrites a ZIP file with the given comment.
func (p *Processor) setZipComment(zipPath, comment string) (retErr error) {
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
	if err := w.SetComment(comment); err != nil {
		_ = w.Close() // cleanup on error path
		return fmt.Errorf("failed to set zip comment: %w", err)
	}

	for _, f := range r.File {
		if err := copyZipEntryRaw(w, f); err != nil {
			_ = w.Close() // cleanup on error path
			return fmt.Errorf("failed to copy entry %s: %w", f.Name, err)
		}
	}

	if err := w.Close(); err != nil {
		return fmt.Errorf("failed to finalize zip: %w", err)
	}

	if err := outFile.Close(); err != nil {
		return fmt.Errorf("failed to close temp zip: %w", err)
	}
	return os.Rename(tmpPath, zipPath)
}

// addFileToZip rewrites a ZIP adding a new file entry.
func (p *Processor) addFileToZip(zipPath, name string, data []byte) (retErr error) {
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

	seen := make(map[string]bool)
	for _, f := range r.File {
		if seen[f.Name] {
			continue
		}
		seen[f.Name] = true

		if err := copyZipEntryRaw(w, f); err != nil {
			_ = w.Close() // cleanup on error path
			return fmt.Errorf("failed to copy entry %s: %w", f.Name, err)
		}
	}

	if seen[name] {
		_ = w.Close() // cleanup on error path
		return fmt.Errorf("entry %s already exists in archive", name)
	}
	fw, err := w.Create(name)
	if err != nil {
		_ = w.Close() // cleanup on error path
		return fmt.Errorf("failed to add %s: %w", name, err)
	}
	if _, err := fw.Write(data); err != nil {
		_ = w.Close() // cleanup on error path
		return fmt.Errorf("failed to write %s: %w", name, err)
	}

	if err := w.Close(); err != nil {
		return fmt.Errorf("failed to finalize zip: %w", err)
	}

	if err := outFile.Close(); err != nil {
		return fmt.Errorf("failed to close temp zip: %w", err)
	}
	return os.Rename(tmpPath, zipPath)
}
