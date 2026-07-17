package menu

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

func TestDropfileName(t *testing.T) {
	tests := []struct {
		name         string
		dropfileType string
		dropfileCase string
		want         string
	}{
		{"default empty is upper", "DOOR32.SYS", "", "DOOR32.SYS"},
		{"explicit upper", "DOOR32.SYS", "upper", "DOOR32.SYS"},
		{"lower", "DOOR32.SYS", "lower", "door32.sys"},
		{"lower case-insensitive key", "DOOR32.SYS", "Lower", "door32.sys"},
		{"unknown case defaults upper", "DOOR32.SYS", "weird", "DOOR32.SYS"},
		{"door.sys lower", "DOOR.SYS", "lower", "door.sys"},
		{"chain.txt lower", "CHAIN.TXT", "lower", "chain.txt"},
		{"dorinfo lower", "DORINFO1.DEF", "lower", "dorinfo1.def"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := dropfileName(tt.dropfileType, tt.dropfileCase); got != tt.want {
				t.Errorf("dropfileName(%q, %q) = %q, want %q", tt.dropfileType, tt.dropfileCase, got, tt.want)
			}
		})
	}
}

func newTestDoorCtx() *DoorCtx {
	return &DoorCtx{
		Executor:    &MenuExecutor{ServerCfg: config.ServerConfig{BoardName: "Test BBS"}},
		User:        doorUserInfo{ID: 1, Handle: "Neo", RealName: "Thomas Anderson", AccessLevel: 50, ScreenWidth: 80, ScreenHeight: 25},
		NodeNumStr:  "1",
		UserIDStr:   "1",
		TimeLeftMin: 30,
	}
}

func TestGenerateDoor32SysCase(t *testing.T) {
	dir := t.TempDir()
	ctx := newTestDoorCtx()

	if err := generateDoor32Sys(ctx, dir, "door32.sys"); err != nil {
		t.Fatalf("generateDoor32Sys: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "door32.sys")); err != nil {
		t.Errorf("expected lowercase door32.sys to exist: %v", err)
	}
}
