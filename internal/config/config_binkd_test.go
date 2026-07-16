package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFTNConfigBinkdDefaults(t *testing.T) {
	// Missing ftn.json → defaults still applied.
	cfg, err := LoadFTNConfig(t.TempDir())
	if err != nil {
		t.Fatalf("LoadFTNConfig: %v", err)
	}
	b := cfg.Binkd
	if b.Enabled {
		t.Error("Enabled should default to false")
	}
	if b.Port != 24554 {
		t.Errorf("Port = %d, want 24554", b.Port)
	}
	if b.BinaryPath != "bin/binkd" {
		t.Errorf("BinaryPath = %q, want bin/binkd", b.BinaryPath)
	}
	if b.LogLevel != 4 {
		t.Errorf("LogLevel = %d, want 4", b.LogLevel)
	}
	if b.ExportSecs != 300 {
		t.Errorf("ExportSecs = %d, want 300", b.ExportSecs)
	}
}

func TestLoadFTNConfigBinkdRoundTrip(t *testing.T) {
	dir := t.TempDir()
	body := `{"networks":{},"binkd":{"enabled":true,"port":24555,"binary_path":"/usr/local/sbin/binkd","log_level":6,"export_interval_seconds":60}}`
	if err := os.WriteFile(filepath.Join(dir, "ftn.json"), []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadFTNConfig(dir)
	if err != nil {
		t.Fatalf("LoadFTNConfig: %v", err)
	}
	b := cfg.Binkd
	if !b.Enabled || b.Port != 24555 || b.BinaryPath != "/usr/local/sbin/binkd" || b.LogLevel != 6 || b.ExportSecs != 60 {
		t.Errorf("unexpected Binkd config: %+v", b)
	}
}

func TestLoadFTNConfigBinkdPartialDefaults(t *testing.T) {
	// Enabled set but numeric fields omitted → defaults fill the gaps.
	dir := t.TempDir()
	body := `{"networks":{},"binkd":{"enabled":true}}`
	if err := os.WriteFile(filepath.Join(dir, "ftn.json"), []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadFTNConfig(dir)
	if err != nil {
		t.Fatalf("LoadFTNConfig: %v", err)
	}
	b := cfg.Binkd
	if !b.Enabled || b.Port != 24554 || b.BinaryPath != "bin/binkd" || b.LogLevel != 4 || b.ExportSecs != 300 {
		t.Errorf("unexpected Binkd config: %+v", b)
	}
}

func TestFTNConfigResolvePaths(t *testing.T) {
	cfg := FTNConfig{
		InboundPath:       "data/ftn/in",
		SecureInboundPath: "/abs/secure_in",
		OutboundPath:      "data/ftn/outbound",
		BinkdOutboundPath: "data/ftn/out",
		TempPath:          "data/ftn/temp",
	}
	cfg.ResolvePaths("/bbs")
	if cfg.InboundPath != filepath.Join("/bbs", "data/ftn/in") {
		t.Errorf("InboundPath = %q", cfg.InboundPath)
	}
	if cfg.SecureInboundPath != "/abs/secure_in" {
		t.Errorf("absolute path must be untouched, got %q", cfg.SecureInboundPath)
	}
	if cfg.BinkdOutboundPath != filepath.Join("/bbs", "data/ftn/out") {
		t.Errorf("BinkdOutboundPath = %q", cfg.BinkdOutboundPath)
	}
	// Empty paths stay empty.
	var empty FTNConfig
	empty.ResolvePaths("/bbs")
	if empty.InboundPath != "" {
		t.Errorf("empty path must stay empty, got %q", empty.InboundPath)
	}
}
