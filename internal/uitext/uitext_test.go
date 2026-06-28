package uitext

import "testing"

const esc = "\x1b"

func TestBoolYN(t *testing.T) {
	if BoolToYN(true) != "Y" || BoolToYN(false) != "N" {
		t.Errorf("BoolToYN: got %q/%q", BoolToYN(true), BoolToYN(false))
	}
	if !YNToBool("Y") || !YNToBool("y") {
		t.Error("YNToBool should accept Y/y")
	}
	if YNToBool("N") || YNToBool("") || YNToBool("yes") {
		t.Error("YNToBool should only accept exactly Y/y")
	}
}

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
		if got := ApproximateVisibleLen(tc.in); got != tc.want {
			t.Errorf("ApproximateVisibleLen(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestTruncateToVisual(t *testing.T) {
	in := esc + "[31mhello" + esc + "[0m"
	if got := TruncateToVisual(in, 3); got != esc+"[31mhel" {
		t.Errorf("TruncateToVisual = %q, want %q", got, esc+"[31mhel")
	}
	if got := TruncateToVisual(in, 3); ApproximateVisibleLen(got) != 3 {
		t.Errorf("truncated visible len = %d, want 3", ApproximateVisibleLen(got))
	}
	if got := TruncateToVisual("ab", 10); got != "ab" {
		t.Errorf("TruncateToVisual(short) = %q, want ab", got)
	}
}
