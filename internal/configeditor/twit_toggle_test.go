package configeditor

import (
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

// TestTwitListsField checks the Default Settings switch reads the default and
// writes an explicit value.
func TestTwitListsField(t *testing.T) {
	var cfg config.ServerConfig
	var field *fieldDef
	for _, f := range sysFieldsDefaults(&cfg) {
		if f.Label == "Twit Lists" {
			field = &f
		}
	}
	if field == nil {
		t.Fatal("no Twit Lists field on Default Settings")
	}
	if got := field.Get(); got != "Y" {
		t.Fatalf("default = %q, want Y", got)
	}
	if err := field.Set("N"); err != nil || cfg.TwitListsOn() || field.Get() != "N" {
		t.Fatalf("set N: on=%v err=%v", cfg.TwitListsOn(), err)
	}
}
