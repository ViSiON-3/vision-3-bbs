package file

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/archiver"
)

// TestArchiveDetectionUsesConfiguredDirectory checks that View detection uses
// the manager's archiver configuration, including disabled ZIP definitions.
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

// TestArchiveViewRejectsNonZIP keeps generic recognition separate from the
// ZIP-only viewer, including when a non-ZIP signature overrides a .zip suffix.
func TestArchiveViewRejectsNonZIP(t *testing.T) {
	dir := t.TempDir()
	cfg := archiver.DefaultConfig()
	for i := range cfg.Archivers {
		cfg.Archivers[i].Enabled = true
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "archivers.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	fm := &FileManager{configPath: filepath.Join(dir, "file_areas.json")}
	for _, tt := range []struct {
		name, content, id string
		view              bool
	}{
		{"NODELIST.Z75", "Rar!\x1a\x07payload", "rar", false},
		{"MISNAMED.ZIP", "Rar!\x1a\x07payload", "rar", false},
		{"LEGACY.LHA", "legacy", "lha", false},
		{"ZIP.Z75", "PK\x03\x04payload", "zip", true},
		{"FALLBACK.ZIP", "unmatched", "zip", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), tt.name)
			if err := os.WriteFile(path, []byte(tt.content), 0600); err != nil {
				t.Fatal(err)
			}
			a, ok, err := cfg.DetectFile(path)
			if err != nil || !ok || a.ID != tt.id {
				t.Fatalf("generic detector: %q, %v, %v", a.ID, ok, err)
			}
			if got := fm.IsSupportedArchive(path); got != tt.view {
				t.Fatalf("View support = %v, want %v", got, tt.view)
			}
		})
	}
}
