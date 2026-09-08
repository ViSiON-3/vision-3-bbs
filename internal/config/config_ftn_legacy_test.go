package config

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestLoadFTNConfig_LegacyPollInterval verifies old configs still load after
// retiring the unused per-network timer, without changing mailer settings.
func TestLoadFTNConfig_LegacyPollInterval(t *testing.T) {
	const legacy = `{
		"binkd": {"enabled": true, "export_interval_seconds": 600},
		"networks": {
			"fsxnet": {
				"internal_tosser_enabled": true,
				"own_address": "21:4/158",
				"poll_interval_seconds": 900,
				"origin": "My BBS",
				"links": [{"address": "21:1/100", "hostname": "hub.example"}]
			}
		}
	}`
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ftn.json"), []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadFTNConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	net := cfg.Networks["fsxnet"]
	if !net.InternalTosserEnabled || net.OwnAddress != "21:4/158" || net.Origin != "My BBS" {
		t.Fatalf("network settings changed: %+v", net)
	}
	if len(net.Links) != 1 || net.Links[0].Address != "21:1/100" || net.Links[0].Hostname != "hub.example" {
		t.Fatalf("hub link changed: %+v", net.Links)
	}
	if !cfg.Binkd.Enabled || cfg.Binkd.ExportSecs != 600 {
		t.Fatalf("integrated mailer settings changed: %+v", cfg.Binkd)
	}
	saved, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(saved, []byte(`"poll_interval_seconds"`)) {
		t.Error("saved config still contains the obsolete poll interval")
	}
}
