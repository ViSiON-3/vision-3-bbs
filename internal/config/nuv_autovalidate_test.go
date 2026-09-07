package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func writeCfg(t *testing.T, m map[string]any) string {
	t.Helper()
	dir := t.TempDir()
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), b, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return dir
}

// The two settings are mutually exclusive. The config editor refuses to enable
// either while the other is on, but a hand-edited file can still set both, so
// the loader must not hand the BBS an impossible state.
func TestBothNUVAndAutoValidateResolvesToNUV(t *testing.T) {
	dir := writeCfg(t, map[string]any{"useNuv": true, "autoValidateNewUsers": true})

	cfg, err := LoadServerConfig(dir)
	if err != nil {
		t.Fatalf("LoadServerConfig: %v", err)
	}
	if !cfg.UseNUV {
		t.Error("UseNUV was disabled; the review process should be the one that survives")
	}
	if cfg.AutoValidateNewUsers {
		t.Error("AutoValidateNewUsers survived alongside UseNUV; they are mutually exclusive")
	}
}

// Each on its own is untouched.
func TestEitherSettingAloneIsPreserved(t *testing.T) {
	nuvOnly := writeCfg(t, map[string]any{"useNuv": true, "autoValidateNewUsers": false})
	cfg, err := LoadServerConfig(nuvOnly)
	if err != nil {
		t.Fatalf("LoadServerConfig: %v", err)
	}
	if !cfg.UseNUV || cfg.AutoValidateNewUsers {
		t.Errorf("NUV alone was altered: useNuv=%v autoValidate=%v", cfg.UseNUV, cfg.AutoValidateNewUsers)
	}

	autoOnly := writeCfg(t, map[string]any{"useNuv": false, "autoValidateNewUsers": true})
	cfg, err = LoadServerConfig(autoOnly)
	if err != nil {
		t.Fatalf("LoadServerConfig: %v", err)
	}
	if cfg.UseNUV || !cfg.AutoValidateNewUsers {
		t.Errorf("auto-validate alone was altered: useNuv=%v autoValidate=%v", cfg.UseNUV, cfg.AutoValidateNewUsers)
	}
}

// Off by default, so an upgrading system keeps its current behaviour.
func TestAutoValidateDefaultsOff(t *testing.T) {
	cfg, err := LoadServerConfig(t.TempDir()) // no config.json at all
	if err != nil {
		t.Fatalf("LoadServerConfig: %v", err)
	}
	if cfg.AutoValidateNewUsers {
		t.Error("autoValidateNewUsers defaulted on; upgrading systems would start validating every signup")
	}
}
