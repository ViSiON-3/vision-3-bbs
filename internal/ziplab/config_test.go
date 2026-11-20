package ziplab

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/archiver"
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

// A setting cleared in the config editor must stay cleared. With omitempty
// the empty value was left out of the file and the default came back on the
// next load.
func TestSaveConfig_KeepsClearedSettings(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultConfig()
	cfg.Steps.VirusScan.Command = ""
	cfg.Steps.VirusScan.Args = nil
	cfg.Steps.VirusScan.Timeout = 0
	cfg.Steps.RemoveAds.PatternsFile = ""
	cfg.Steps.AddComment.CommentFile = ""
	cfg.Steps.IncludeFile.FilePath = ""

	if err := SaveConfig(dir, cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	got, err := ReadConfig(dir)
	if err != nil {
		t.Fatalf("ReadConfig: %v", err)
	}
	if !reflect.DeepEqual(got.Steps, cfg.Steps) {
		t.Errorf("cleared settings came back:\n got  %+v\n want %+v", got.Steps, cfg.Steps)
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

// The archive type comes from the file's content: a ZIP named NODELIST.Z75
// is a native ZIP, a .zip that is really a RAR is a RAR, and the name
// decides only when no signature matches or the file can't be read (#594).
func TestGetArchiveTypeByContent(t *testing.T) {
	cfgDir := t.TempDir()
	arc := archiver.DefaultConfig()
	for i := range arc.Archivers {
		if arc.Archivers[i].ID == "rar" {
			arc.Archivers[i].Enabled = true
		}
	}
	data, err := json.Marshal(arc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "archivers.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(cfgDir)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	write := func(name, content string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	for _, tt := range []struct {
		path, want string
	}{
		{write("NODELIST.Z75", "PK\x03\x04rest"), "zip"},
		{write("misnamed.zip", "Rar!\x1a\x07\x00rest"), "rar"},
		{write("plain.zip", "not an archive"), "zip"},
		{write("notes.txt", "PK is not at the start"), ""},
		{filepath.Join(dir, "missing.rar"), "rar"},
	} {
		at, ok := cfg.GetArchiveType(tt.path)
		if ok != (tt.want != "") || at.ID != tt.want {
			t.Errorf("%s: got %q, %v; want %q", filepath.Base(tt.path), at.ID, ok, tt.want)
		}
		if got := cfg.IsArchiveSupported(tt.path); got != (tt.want != "") {
			t.Errorf("%s: IsArchiveSupported = %v", filepath.Base(tt.path), got)
		}
	}
	if at, ok := cfg.GetArchiveType(filepath.Join(dir, "NODELIST.Z75")); !ok || !at.Native {
		t.Errorf("NODELIST.Z75 = %+v, want the native ZIP type", at)
	}
	// Something that isn't a regular file is no archive, whatever its name.
	folder := filepath.Join(dir, "folder.zip")
	if err := os.Mkdir(folder, 0700); err != nil {
		t.Fatal(err)
	}
	if at, ok := cfg.GetArchiveType(folder); ok || cfg.IsArchiveSupported(folder) {
		t.Errorf("folder.zip (a directory) = %q, %v; want unsupported", at.ID, ok)
	}
}

// A pipeline finds the archive's type once and keeps it: ad removal can
// rewrite a ZIP holding only an advert into an empty one, whose first bytes
// (PK\x05\x06) no longer match ZIP's signature, and the later steps must
// still take it for the ZIP it was, whatever it is named.
func TestProcessorKeepsArchiveType(t *testing.T) {
	cfg := DefaultConfig()
	path := filepath.Join(t.TempDir(), "NODELIST.Z75")
	if err := os.WriteFile(path, []byte("PK\x03\x04rest"), 0644); err != nil {
		t.Fatal(err)
	}
	p := NewProcessor(cfg, t.TempDir())
	if at, ok := p.archiveType(path); !ok || at.ID != "zip" {
		t.Fatalf("first look: %+v, %v", at, ok)
	}
	if err := os.WriteFile(path, []byte("PK\x05\x06"+strings.Repeat("\x00", 18)), 0644); err != nil {
		t.Fatal(err)
	}
	if _, ok := cfg.GetArchiveType(path); ok {
		t.Fatal("test premise: an empty ZIP named .Z75 is still detected")
	}
	if at, ok := p.archiveType(path); !ok || at.ID != "zip" {
		t.Errorf("after the rewrite: %+v, %v", at, ok)
	}
}
