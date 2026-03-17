package v3net

import (
	"strings"
	"testing"
)

func TestAppendV3NetOrigin(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		tearline string
		origin   string
		wantTear bool
		wantOrig string
	}{
		{
			name:     "tearline and origin",
			body:     "Hello world",
			tearline: "--- ViSiON/3 0.1.0/linux",
			origin:   "My Cool BBS",
			wantTear: true,
			wantOrig: " * Origin: My Cool BBS",
		},
		{
			name:     "origin only",
			body:     "Hello world",
			tearline: "",
			origin:   "My Cool BBS",
			wantTear: false,
			wantOrig: " * Origin: My Cool BBS",
		},
		{
			name:     "tearline only",
			body:     "Hello world",
			tearline: "--- ViSiON/3 0.1.0/linux",
			origin:   "",
			wantTear: true,
			wantOrig: "",
		},
		{
			name:     "both empty",
			body:     "Hello world",
			tearline: "",
			origin:   "",
			wantTear: false,
			wantOrig: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := appendV3NetOrigin(tt.body, tt.tearline, tt.origin)

			if tt.wantTear && !strings.Contains(got, tt.tearline) {
				t.Errorf("want tearline %q in result %q", tt.tearline, got)
			}
			if !tt.wantTear && strings.Contains(got, "---") {
				t.Errorf("got unexpected tearline in result %q", got)
			}
			if tt.wantOrig != "" && !strings.Contains(got, tt.wantOrig) {
				t.Errorf("want origin %q in result %q", tt.wantOrig, got)
			}
			if tt.wantOrig == "" && strings.Contains(got, " * Origin:") {
				t.Errorf("got unexpected origin in result %q", got)
			}
		})
	}
}

func TestAppendV3NetOriginOrder(t *testing.T) {
	got := appendV3NetOrigin("body", "--- ViSiON/3 0.1.0/linux", "My BBS")
	tearIdx := strings.Index(got, "---")
	origIdx := strings.Index(got, " * Origin:")
	if tearIdx >= origIdx {
		t.Errorf("tearline should appear before origin line; got %q", got)
	}
}
