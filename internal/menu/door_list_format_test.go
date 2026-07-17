package menu

import (
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

func TestFormatDoorListLine(t *testing.T) {
	d := config.DoorConfig{Code: "LORD", Name: "Legend of the Red Dragon"}
	line := formatDoorListLine("^ID ^CO ^NA ^TY", 3, "LORD", d)

	if !strings.Contains(line, "LORD") {
		t.Errorf("line missing code: %q", line)
	}
	if !strings.Contains(line, "Legend of the Red Dragon") {
		t.Errorf("line should show display Name, not the code: %q", line)
	}
	if !strings.Contains(line, "3") {
		t.Errorf("line missing index: %q", line)
	}
	if !strings.Contains(line, "Native") {
		t.Errorf("line missing type: %q", line)
	}
}

func TestFormatDoorListLineTypes(t *testing.T) {
	tests := []struct {
		door config.DoorConfig
		want string
	}{
		{config.DoorConfig{Type: "v3_script"}, "VPL"},
		{config.DoorConfig{Type: "synchronet_js"}, "Synchronet JS"},
		{config.DoorConfig{IsDOS: true}, "DOS"},
		{config.DoorConfig{}, "Native"},
	}
	for _, tt := range tests {
		if got := formatDoorListLine("^TY", 1, "X", tt.door); !strings.Contains(got, tt.want) {
			t.Errorf("type %q/isDOS=%v: got %q, want contains %q", tt.door.Type, tt.door.IsDOS, got, tt.want)
		}
	}
}
