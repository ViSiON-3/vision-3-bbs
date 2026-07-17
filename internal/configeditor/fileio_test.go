package configeditor

import (
	"os"
	"path/filepath"
	"testing"
)

// A doors.json that fails to load (e.g. old format without the required
// "code" field) must surface an error — not silently present an empty door
// list that would wipe the file on the next save.
func TestLoadAllConfigsPropagatesDoorsError(t *testing.T) {
	dir := t.TempDir()
	bad := `[{"name": "LORD", "commands": ["/usr/bin/lord"]}]`
	if err := os.WriteFile(filepath.Join(dir, "doors.json"), []byte(bad), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	if _, err := loadAllConfigs(dir); err == nil {
		t.Error("expected error for invalid doors.json, got nil")
	}
}

// A missing doors.json is fine (doors are optional): loadAllConfigs must
// succeed with an empty map.
func TestLoadAllConfigsMissingDoorsOK(t *testing.T) {
	dir := t.TempDir()
	ac, err := loadAllConfigs(dir)
	if err != nil {
		t.Fatalf("loadAllConfigs with no doors.json: %v", err)
	}
	if ac.Doors == nil || len(ac.Doors) != 0 {
		t.Errorf("Doors = %v, want empty non-nil map", ac.Doors)
	}
}
