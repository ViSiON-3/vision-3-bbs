package file

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/archiver"
)

func TestArchiveDetectionUsesConfiguredDirectory(t *testing.T) {
	dir := t.TempDir()
	fm := &FileManager{configPath: filepath.Join(dir, "file_areas.json")}
	path := filepath.Join(t.TempDir(), "NODELIST.Z75")
	if err := os.WriteFile(path, []byte("PK\x03\x04"), 0600); err != nil {
		t.Fatal(err)
	}
	if !fm.IsSupportedArchive(path) {
		t.Fatal("misnamed ZIP not detected with defaults")
	}
	if err := os.WriteFile(filepath.Join(dir, "archivers.json"), []byte(`{"archivers":[{"id":"zip","extension":".zip","magic":"504B0304","enabled":false}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if fm.IsSupportedArchive(path) {
		t.Fatal("disabled ZIP detected; configured directory ignored")
	}
	if err := archiver.SaveDefaultConfig(dir); err != nil {
		t.Fatal(err)
	}
	if !fm.IsSupportedArchive(path) {
		t.Fatal("configured ZIP not detected")
	}
}
