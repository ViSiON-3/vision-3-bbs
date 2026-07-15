package configeditor

import (
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

func TestSysFieldsRegistration_QWKIDNormalizesOnSet(t *testing.T) {
	cfg := &config.ServerConfig{}
	fields := sysFieldsRegistration(cfg)

	var f *fieldDef
	for i := range fields {
		if fields[i].Label == "QWK ID" {
			f = &fields[i]
			break
		}
	}
	if f == nil {
		t.Fatal("QWK ID field not found in registration screen")
	}
	if err := f.Set("my id!"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if cfg.QWKID != "MYID" {
		t.Errorf("Set should normalize: want cfg.QWKID == MYID, got %q", cfg.QWKID)
	}
	if got := f.Get(); got != "MYID" {
		t.Errorf("Get: want MYID, got %q", got)
	}
}

func TestSysFieldsQWKAPI(t *testing.T) {
	cfg := &config.ServerConfig{}
	fields := sysFieldsQWKAPI(cfg)

	find := func(label string) *fieldDef {
		for i := range fields {
			if fields[i].Label == label {
				return &fields[i]
			}
		}
		t.Fatalf("%s field not found in QWK API screen", label)
		return nil
	}

	enabled := find("Enabled")
	if got := enabled.Get(); got != "N" {
		t.Errorf("Enabled default: want N, got %q", got)
	}
	if err := enabled.Set("Y"); err != nil {
		t.Fatalf("Set Enabled: %v", err)
	}
	if !cfg.QWKAPI.Enabled {
		t.Error("Set(Y) should enable cfg.QWKAPI.Enabled")
	}

	if got := find("Port").Get(); got != "8666" {
		t.Errorf("Port default: want 8666, got %q", got)
	}
	if got := find("Token TTL Hrs").Get(); got != "24" {
		t.Errorf("Token TTL default: want 24, got %q", got)
	}
}
