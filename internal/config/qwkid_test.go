package config

import "testing"

func TestNormalizeQWKID(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "VISION3", "VISION3"},
		{"lowercased", "vision3", "VISION3"},
		{"spaces stripped", "My Cool BBS", "MYCOOLBB"},
		{"symbols stripped", "V!S!O#N/3", "VSON3"},
		{"truncated to 8", "LongBoardName123", "LONGBOAR"},
		{"empty", "", ""},
		{"all symbols", "!@#$%^&*()", ""},
		{"unicode stripped", "Café BBS", "CAFBBS"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeQWKID(tt.in); got != tt.want {
				t.Errorf("NormalizeQWKID(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
