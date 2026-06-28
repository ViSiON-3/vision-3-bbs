package configeditor

import "testing"

func TestMaskValue(t *testing.T) {
	if got := maskValue(""); got != "" {
		t.Errorf("maskValue(empty) = %q, want empty", got)
	}
	if got := maskValue("secret"); got != "******" {
		t.Errorf("maskValue = %q, want ******", got)
	}
	// Counts runes, not bytes.
	if got := maskValue("café"); got != "****" {
		t.Errorf("maskValue(café) = %q, want 4 stars", got)
	}
}

func TestPadRightLeft_RuneAware(t *testing.T) {
	if got := padRight("ab", 5); got != "ab   " {
		t.Errorf("padRight short = %q", got)
	}
	if got := padRight("abcdef", 3); got != "abc" {
		t.Errorf("padRight truncate = %q", got)
	}
	if got := padLeft("ab", 5); got != "   ab" {
		t.Errorf("padLeft short = %q", got)
	}
	// Multibyte: 4 runes already >= width 4, returned as-is (rune-counted).
	if got := padRight("café", 4); got != "café" {
		t.Errorf("padRight(café,4) = %q, want café", got)
	}
}

func TestCenterText(t *testing.T) {
	if got := centerText("hi", 6); got != "  hi  " {
		t.Errorf("centerText = %q, want %q", got, "  hi  ")
	}
	// Odd remainder: extra space goes on the right.
	if got := centerText("hi", 5); got != " hi  " {
		t.Errorf("centerText odd = %q, want %q", got, " hi  ")
	}
	if got := centerText("toolong", 3); got != "toolong" {
		t.Errorf("centerText overflow = %q, want toolong", got)
	}
}
