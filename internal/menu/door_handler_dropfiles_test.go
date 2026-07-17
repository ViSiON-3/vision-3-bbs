package menu

import "testing"

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
