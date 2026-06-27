package configeditor

import "testing"

const esc = "\x1b"

func TestApproximateVisibleLen(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"hello", 5},
		{"", 0},
		{esc + "[31mhi" + esc + "[0m", 2},
		{esc + "[1;32m", 0},
	}
	for _, tc := range cases {
		if got := approximateVisibleLen(tc.in); got != tc.want {
			t.Errorf("approximateVisibleLen(%q) = %d, want %d", tc.in, got, tc.want)
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
		t.Errorf("visible len after truncate = %d, want 3", approximateVisibleLen(got))
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
