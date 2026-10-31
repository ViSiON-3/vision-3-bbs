package configeditor

import (
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"testing"
)

// TestBadUserNamesPathField checks rules-path editing and its security menu entry.
func TestBadUserNamesPathField(t *testing.T) {
	cfg := config.ServerConfig{BadUsersPath: "configs/badusers.txt"}
	f := sysFieldsBadUserNames(&cfg)[0]
	if f.Get() != cfg.BadUsersPath {
		t.Fatal("wrong initial path")
	}
	if err := f.Set("custom/names.txt"); err != nil {
		t.Fatal(err)
	}
	if cfg.BadUsersPath != "custom/names.txt" {
		t.Fatal("path edit not applied")
	}
	found := false
	for _, it := range securityMenuItems() {
		found = found || it.Label == "Bad User Names"
	}
	if !found {
		t.Fatal("missing security menu entry")
	}
}
