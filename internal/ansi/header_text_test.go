package ansi

import "testing"

func TestHeaderText(t *testing.T) {
	for _, tc := range []struct{ name, in, want string }{
		{"plain", "Hello |15 there", "Hello |15 there"},
		{"utf-8 text", "Café ─ news", "Café ─ news"},
		{"escape", "a\x1b[2Jb", "a [2Jb"},
		{"osc and bell", "\x1b]0;t\x07x", " ]0;t x"},
		{"line ends and del", "a\r\nb\x7f", "a  b "},
		{"8-bit CSI as UTF-8", "a\u009b2J", "a 2J"},
		{"raw CP437 is kept", "Caf\x82 \x9b5", "Caf\x82 \x9b5"},
		{"raw CP437 with an escape", "Caf\x82\x1b[2J", "Caf\x82 [2J"},
		{"8-bit CSI beside a bad byte", "\xff\u009b2J", "\xff 2J"},
	} {
		if got := HeaderText(tc.in); got != tc.want {
			t.Errorf("%s: HeaderText(%q) = %q, want %q", tc.name, tc.in, got, tc.want)
		}
	}
}
