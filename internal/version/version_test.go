package version

import (
	"runtime"
	"testing"
)

func TestDisplay(t *testing.T) {
	orig := Number
	t.Cleanup(func() { Number = orig })

	tests := []struct {
		number string
		want   string
	}{
		{"0.8.0", "v0.8.0"},
		{"v1.2.3", "v1.2.3"},
		{" 1.0.0 ", "v1.0.0"},
		{"", ""},
	}
	for _, tt := range tests {
		Number = tt.number
		if got := Display(); got != tt.want {
			t.Errorf("Display() with Number=%q = %q, want %q", tt.number, got, tt.want)
		}
	}
}

func TestPlatform(t *testing.T) {
	got := Platform()
	if got == "" {
		t.Fatal("Platform() returned an empty string")
	}
	if runtime.GOOS == "darwin" && got != "macOS" {
		t.Errorf("Platform() on darwin = %q, want %q", got, "macOS")
	}
}
