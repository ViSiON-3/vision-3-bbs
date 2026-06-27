package usereditor

import "testing"

const esc = "\x1b"

func TestApproximateVisibleLen(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want int
	}{
		{"plain", "hello", 5},
		{"empty", "", 0},
		{"with color codes", esc + "[31mhi" + esc + "[0m", 2},
		{"only escape", esc + "[1;32m", 0},
	}
	for _, tc := range cases {
		if got := approximateVisibleLen(tc.in); got != tc.want {
			t.Errorf("%s: approximateVisibleLen(%q) = %d, want %d", tc.name, tc.in, got, tc.want)
		}
	}
}

func TestTruncateToVisual_PreservesANSI(t *testing.T) {
	in := esc + "[31mhello" + esc + "[0m"
	got := truncateToVisual(in, 3)
	want := esc + "[31mhel"
	if got != want {
		t.Errorf("truncateToVisual = %q, want %q", got, want)
	}
	if approximateVisibleLen(got) != 3 {
		t.Errorf("truncated visible len = %d, want 3", approximateVisibleLen(got))
	}
}

func TestTruncateToVisual_ShorterThanLimit(t *testing.T) {
	if got := truncateToVisual("ab", 10); got != "ab" {
		t.Errorf("truncateToVisual(short) = %q, want ab", got)
	}
}

func TestPadToCol(t *testing.T) {
	if got := padToCol("abc", 5); got != "abc  " {
		t.Errorf("padToCol pad = %q, want %q", got, "abc  ")
	}
	if got := padToCol("abcdef", 3); got != "abc" {
		t.Errorf("padToCol truncate = %q, want abc", got)
	}
}
