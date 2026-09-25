package ziplab

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDefaultConfig_AllStepsEnabled(t *testing.T) {
	cfg := DefaultConfig()

	if !cfg.Enabled {
		t.Error("expected Enabled to be true by default")
	}
	if !cfg.RunOnUpload {
		t.Error("expected RunOnUpload to be true by default")
	}
	if !cfg.Steps.TestIntegrity.Enabled {
		t.Error("expected TestIntegrity to be enabled by default")
	}
	if !cfg.Steps.ExtractToTemp.Enabled {
		t.Error("expected ExtractToTemp to be enabled by default")
	}
	if cfg.Steps.VirusScan.Enabled {
		t.Error("expected VirusScan to be disabled by default (requires external tool)")
	}
	if !cfg.Steps.RemoveAds.Enabled {
		t.Error("expected RemoveAds to be enabled by default")
	}
	if !cfg.Steps.AddComment.Enabled {
		t.Error("expected AddComment to be enabled by default")
	}
	if !cfg.Steps.IncludeFile.Enabled {
		t.Error("expected IncludeFile to be enabled by default")
	}
}

func TestDefaultConfig_ScanFailBehavior(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.ScanFailBehavior != "delete" {
		t.Errorf("expected ScanFailBehavior to be 'delete', got %q", cfg.ScanFailBehavior)
	}
}

func TestDefaultConfig_HasZipArchiveType(t *testing.T) {
	cfg := DefaultConfig()

	if len(cfg.ArchiveTypes) == 0 {
		t.Fatal("expected at least one archive type configured")
	}

	found := false
	for _, at := range cfg.ArchiveTypes {
		if at.Extension == ".zip" {
			found = true
			if !at.Native {
				t.Error("expected .zip to be marked as native")
			}
		}
	}
	if !found {
		t.Error("expected .zip archive type in defaults")
	}
}

func TestLoadConfig_MissingFile_ReturnsDefaults(t *testing.T) {
	cfg, err := LoadConfig(filepath.Join(t.TempDir(), "nonexistent"))
	if err != nil {
		t.Fatalf("expected no error for missing file, got: %v", err)
	}
	if !cfg.Enabled {
		t.Error("expected defaults when file missing")
	}
}

func TestLoadConfig_ValidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	cfgData := Config{
		Enabled:          false,
		RunOnUpload:      false,
		ScanFailBehavior: "quarantine",
		QuarantinePath:   "/tmp/quarantine",
	}

	data, _ := json.MarshalIndent(cfgData, "", "  ")
	os.WriteFile(filepath.Join(tmpDir, "ziplab.json"), data, 0644)

	cfg, err := LoadConfig(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Enabled {
		t.Error("expected Enabled to be false from JSON")
	}
	if cfg.RunOnUpload {
		t.Error("expected RunOnUpload to be false from JSON")
	}
	if cfg.ScanFailBehavior != "quarantine" {
		t.Errorf("expected ScanFailBehavior 'quarantine', got %q", cfg.ScanFailBehavior)
	}
}

func TestLoadConfig_InvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "ziplab.json"), []byte("{invalid json"), 0644)

	_, err := LoadConfig(tmpDir)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestLoadConfig_PartialOverride(t *testing.T) {
	tmpDir := t.TempDir()
	// Only override one field — rest should remain at defaults
	partialJSON := `{"scanFailBehavior": "quarantine"}`
	os.WriteFile(filepath.Join(tmpDir, "ziplab.json"), []byte(partialJSON), 0644)

	cfg, err := LoadConfig(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ScanFailBehavior != "quarantine" {
		t.Errorf("expected overridden field, got %q", cfg.ScanFailBehavior)
	}
	// Defaults should still apply for non-overridden fields
	if !cfg.Enabled {
		t.Error("expected Enabled default to remain true")
	}
}

func TestIsArchiveSupported(t *testing.T) {
	cfg := DefaultConfig()

	tests := []struct {
		filename string
		expected bool
	}{
		{"test.zip", true},
		{"TEST.ZIP", true},
		{"file.txt", false},
		{"archive.rar", false}, // not in defaults
		{"", false},
	}

	for _, tt := range tests {
		result := cfg.IsArchiveSupported(tt.filename)
		if result != tt.expected {
			t.Errorf("IsArchiveSupported(%q) = %v, want %v", tt.filename, result, tt.expected)
		}
	}
}

func TestGetArchiveType(t *testing.T) {
	cfg := DefaultConfig()

	at, ok := cfg.GetArchiveType("test.zip")
	if !ok {
		t.Fatal("expected to find archive type for .zip")
	}
	if at.Extension != ".zip" {
		t.Errorf("expected extension .zip, got %q", at.Extension)
	}

	_, ok = cfg.GetArchiveType("test.rar")
	if ok {
		t.Error("expected no archive type for .rar in defaults")
	}
}

// An editor saves what it read. The archive types merged in from
// archivers.json must never land in ziplab.json, or every archiver would be
// copied there on the first save.
func TestSaveConfig_RoundTripOmitsArchiveTypes(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultConfig()
	cfg.ScanFailBehavior = "quarantine"
	cfg.QuarantinePath = "data/quarantine"
	cfg.Steps.VirusScan.Enabled = true
	cfg.Steps.VirusScan.Args = []string{"--infected", "{WORKDIR}"}
	cfg.ArchiveTypes = []ArchiveType{{Extension: ".rar", ExtractCommand: "unrar"}}

	if err := SaveConfig(dir, cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "ziplab.json"))
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("saved file is not JSON: %v", err)
	}
	if _, ok := raw["archiveTypes"]; ok {
		t.Error("archiveTypes was written to ziplab.json")
	}

	got, err := ReadConfig(dir)
	if err != nil {
		t.Fatalf("ReadConfig: %v", err)
	}
	cfg.ArchiveTypes = DefaultConfig().ArchiveTypes // not persisted
	if !reflect.DeepEqual(got, cfg) {
		t.Errorf("round trip changed the config:\n got  %+v\n want %+v", got, cfg)
	}
}

// Files written before the step settings were trimmed carry a command on
// every step and an archiveTypes list. They must still load, keeping the
// settings that remain.
func TestReadConfig_AcceptsLegacyFields(t *testing.T) {
	dir := t.TempDir()
	legacy := `{
  "enabled": true,
  "runOnUpload": false,
  "steps": {
    "testIntegrity": {"enabled": false, "command": "x", "args": ["y"], "timeoutSeconds": 5},
    "virusScan": {"enabled": true, "command": "clamdscan", "timeoutSeconds": 30}
  },
  "archiveTypes": [{"extension": ".rar", "native": false}]
}`
	if err := os.WriteFile(filepath.Join(dir, "ziplab.json"), []byte(legacy), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := ReadConfig(dir)
	if err != nil {
		t.Fatalf("ReadConfig: %v", err)
	}
	if cfg.RunOnUpload || cfg.Steps.TestIntegrity.Enabled {
		t.Error("legacy settings were not applied")
	}
	if cfg.Steps.VirusScan.Command != "clamdscan" || cfg.Steps.VirusScan.Timeout != 30 {
		t.Errorf("virus scan settings lost: %+v", cfg.Steps.VirusScan)
	}
	if !reflect.DeepEqual(cfg.ArchiveTypes, DefaultConfig().ArchiveTypes) {
		t.Errorf("archiveTypes was read from ziplab.json: %+v", cfg.ArchiveTypes)
	}
}

// The shipped template is what a new board starts with, so it must say the
// same thing as the built-in defaults a board without the file runs on.
func TestTemplateMatchesDefaults(t *testing.T) {
	cfg, err := ReadConfig(filepath.Join("..", "..", "templates", "configs"))
	if err != nil {
		t.Fatalf("reading template: %v", err)
	}
	if want := DefaultConfig(); !reflect.DeepEqual(cfg, want) {
		t.Errorf("templates/configs/ziplab.json differs from DefaultConfig:\n got  %+v\n want %+v", cfg, want)
	}
}
