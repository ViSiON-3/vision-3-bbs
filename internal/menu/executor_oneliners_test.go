package menu

import "testing"

func TestPipeCodeLenAt(t *testing.T) {
	cases := []struct {
		name  string
		value string
		want  int
	}{
		{"foreground 2-digit", "|05", 3},
		{"foreground 15", "|15x", 3},
		{"background single digit", "|B5", 3},
		{"background single digit followed by text", "|B5hello", 3},
		{"background single digit zero", "|B0", 3},
		{"background two digit", "|B10", 4},
		{"background two digit max", "|B15", 4},
		{"pause code", "|Pmore", 2},
		{"named CR", "|CR", 3},
		{"not a pipe code", "x", 0},
		{"bare pipe at end", "|", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := pipeCodeLenAt(tc.value, 0); got != tc.want {
				t.Errorf("pipeCodeLenAt(%q, 0) = %d, want %d", tc.value, got, tc.want)
			}
		})
	}
}

func TestTruncatePipeCodedText_DoesNotEatCharAfterBackgroundCode(t *testing.T) {
	// Regression: pipeCodeLenAt over-counted |B codes by one, so truncation
	// consumed the first visible character after a background code.
	// "|B5AB" has visible payload "AB" (the |B5 code is zero-width).
	got := truncatePipeCodedText("|B5AB", 2)
	if got != "|B5AB" {
		t.Errorf("truncatePipeCodedText(%q, 2) = %q, want %q", "|B5AB", got, "|B5AB")
	}
	// Truncating to 1 visible char keeps the code + first char only.
	if got := truncatePipeCodedText("|B5AB", 1); got != "|B5A" {
		t.Errorf("truncatePipeCodedText(%q, 1) = %q, want %q", "|B5AB", got, "|B5A")
	}
}

func TestContainsDisallowedOnelinerColorCode(t *testing.T) {
	cases := []struct {
		value string
		want  bool
	}{
		{"plain text", false},
		{"|05allowed foreground", false},
		{"|15 highest foreground", false},
		{"escaped pipe || is fine", false},
		{"|B5 background disallowed", true},
		{"|B10 background disallowed", true},
		{"|00 reserved is disallowed", true}, // |00 < |01
	}
	for _, tc := range cases {
		if got := containsDisallowedOnelinerColorCode(tc.value); got != tc.want {
			t.Errorf("containsDisallowedOnelinerColorCode(%q) = %v, want %v", tc.value, got, tc.want)
		}
	}
}
