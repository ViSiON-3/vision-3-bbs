package config

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

// A ramp reads as 1 to 8 colours 0-15, commas or spaces between; anything
// else is refused, and FadeColors then falls back to the default.
func TestToastFadeColors(t *testing.T) {
	for in, want := range map[string][]int{
		"8,7,15":    {8, 7, 15},
		" 1, 9 11 ": {1, 9, 11},
		"0":         {0},
	} {
		if got, ok := ParseToastFadeColors(in); !ok || !slices.Equal(got, want) {
			t.Errorf("%q: got %v %v", in, got, ok)
		}
	}
	for _, bad := range []string{"", "16", "-1", "8,x", "1,2,3,4,5,6,7,8,9", ","} {
		if _, ok := ParseToastFadeColors(bad); ok {
			t.Errorf("%q accepted", bad)
		}
		if got := (ThemeConfig{ToastFadeColors: bad}).FadeColors(); !slices.Equal(got, []int{8, 7, 15}) {
			t.Errorf("%q: FadeColors %v, want the default", bad, got)
		}
	}
	if got := FormatToastFadeColors([]int{8, 7, 15}); got != DefaultToastFadeColors {
		t.Errorf("format: %q", got)
	}
}

// A theme.json from before the ramp gets the default; one with its own
// keeps it.
func TestThemeToastFadeColorsLoad(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "theme.json"), []byte(`{"yesNoRegularColor": 13}`), 0o644); err != nil {
		t.Fatal(err)
	}
	th, err := LoadThemeConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if th.ToastFadeColors != DefaultToastFadeColors {
		t.Errorf("old theme: %q", th.ToastFadeColors)
	}
	if err := os.WriteFile(filepath.Join(dir, "theme.json"), []byte(`{"toastFadeColors": "1,9,11,15"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if th, _ = LoadThemeConfig(dir); !slices.Equal(th.FadeColors(), []int{1, 9, 11, 15}) {
		t.Errorf("own ramp: %v", th.FadeColors())
	}
}

// The step time defaults when unset and is held to its range.
func TestToastFadeStep(t *testing.T) {
	for ms, want := range map[int]int{0: 80, 5: 20, 120: 120, 9000: 500} {
		if got := (ServerConfig{ToastFadeMs: ms}).ToastFadeStep(); got != time.Duration(want)*time.Millisecond {
			t.Errorf("%d: %s", ms, got)
		}
	}
}
