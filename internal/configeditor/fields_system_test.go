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
