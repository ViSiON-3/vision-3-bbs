package menu

import (
	"os"
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// scanDirectoryFiles tests
// ---------------------------------------------------------------------------

func TestScanDirectoryFiles_NonExistentDir(t *testing.T) {
	_, err := scanDirectoryFiles("/nonexistent/path/that/does/not/exist")
	if err == nil {
		t.Error("expected error for non-existent directory, got nil")
	}
}

func TestScanDirectoryFiles_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	files, err := scanDirectoryFiles(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("expected empty map for empty dir, got %v", files)
	}
}

func TestScanDirectoryFiles_ExcludesMetadataJSON(t *testing.T) {
	dir := t.TempDir()
	// Create metadata.json — should be excluded.
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), []byte("{}"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	// Create a regular file — should be included.
	if err := os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("hello"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	files, err := scanDirectoryFiles(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, found := files["metadata.json"]; found {
		t.Error("metadata.json should be excluded from results")
	}
	if _, found := files["readme.txt"]; !found {
		t.Error("readme.txt should be included in results")
	}
}

func TestScanDirectoryFiles_ExcludesSubdirectories(t *testing.T) {
	dir := t.TempDir()
	// Create a subdirectory — should be excluded.
	if err := os.Mkdir(filepath.Join(dir, "subdir"), 0755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	// Create a file inside the subdir — should not appear.
	if err := os.WriteFile(filepath.Join(dir, "subdir", "nested.txt"), []byte("x"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	// Create a file at the top level — should appear.
	if err := os.WriteFile(filepath.Join(dir, "top.txt"), []byte("y"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	files, err := scanDirectoryFiles(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, found := files["subdir"]; found {
		t.Error("subdirectory should be excluded from results")
	}
	if _, found := files["nested.txt"]; found {
		t.Error("file inside subdirectory should not appear in results")
	}
	if _, found := files["top.txt"]; !found {
		t.Error("top-level file should be included in results")
	}
}

func TestScanDirectoryFiles_ReturnsCorrectSizes(t *testing.T) {
	dir := t.TempDir()
	content := []byte("hello world")
	if err := os.WriteFile(filepath.Join(dir, "file.bin"), content, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	files, err := scanDirectoryFiles(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	size, found := files["file.bin"]
	if !found {
		t.Fatal("file.bin not found in results")
	}
	if size != int64(len(content)) {
		t.Errorf("file.bin size = %d, want %d", size, len(content))
	}
}

func TestScanDirectoryFiles_MultipleFiles(t *testing.T) {
	dir := t.TempDir()
	wantFiles := map[string]int64{
		"alpha.txt": 5,
		"beta.bin":  10,
		"gamma.dat": 3,
	}
	for name, size := range wantFiles {
		data := make([]byte, size)
		if err := os.WriteFile(filepath.Join(dir, name), data, 0644); err != nil {
			t.Fatalf("WriteFile(%q): %v", name, err)
		}
	}
	// Also write metadata.json to ensure it is excluded.
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), []byte("{}"), 0644); err != nil {
		t.Fatalf("WriteFile metadata.json: %v", err)
	}

	files, err := scanDirectoryFiles(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != len(wantFiles) {
		t.Errorf("got %d files, want %d", len(files), len(wantFiles))
	}
	for name, wantSize := range wantFiles {
		gotSize, found := files[name]
		if !found {
			t.Errorf("file %q missing from results", name)
			continue
		}
		if gotSize != wantSize {
			t.Errorf("file %q size = %d, want %d", name, gotSize, wantSize)
		}
	}
}

func TestScanDirectoryFiles_ZeroByteFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "empty.txt"), []byte{}, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	files, err := scanDirectoryFiles(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	size, found := files["empty.txt"]
	if !found {
		t.Fatal("empty.txt not found in results")
	}
	if size != 0 {
		t.Errorf("empty.txt size = %d, want 0", size)
	}
}