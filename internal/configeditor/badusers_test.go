package configeditor

import (
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"testing"
)

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
	items := securityMenuItems()
	if items[len(items)-1].Label != "Bad User Names" {
		t.Fatal("missing security menu entry")
	}
}
