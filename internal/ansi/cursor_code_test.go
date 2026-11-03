package ansi

import "testing"

func TestCursorPipeCode(t *testing.T) {
	cases := []struct{ in, want string }{
		{"|XY15,12", "\x1b[12;15H"},
		{"|XY01,01", "\x1b[1;1H"},
		{"|XY00,00", "\x1b[1;1H"}, // 0 means 1, as in the escape sequence
		{"|XY99,99", "\x1b[99;99H"},
		{"|15|XY10,05Hi", "\x1b[1;37m\x1b[5;10HHi"},
		// Not cursor codes: printed as written.
		{"|XY5,12", "|XY5,12"},
		{"|XY15;12", "|XY15;12"},
		{"|XYab,cd", "|XYab,cd"},
		{"|XY15,1", "|XY15,1"},
		{"|xy15,12", "|xy15,12"},
		// The literal-pipe escape wins.
		{"||XY15,12", "|XY15,12"},
	}
	for _, c := range cases {
		if got := string(ReplacePipeCodes([]byte(c.in))); got != c.want {
			t.Errorf("ReplacePipeCodes(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestCursorPipeCodeIsAScreenCode(t *testing.T) {
	// Text from a caller or another system must not move the cursor.
	if got := EscapeScreenPipeCodes("|15Neo|XY01,01"); got != "|15Neo||XY01,01" {
		t.Errorf("EscapeScreenPipeCodes = %q", got)
	}
	if got := DefangScreenCodes("|15Neo|XY01,01"); got != "|15Neo/XY01,01" {
		t.Errorf("DefangScreenCodes = %q", got)
	}
	// Colour-only expansion leaves it alone.
	if got := string(ReplaceColorPipeCodes([]byte("|XY01,01"))); got != "|XY01,01" {
		t.Errorf("ReplaceColorPipeCodes = %q", got)
	}
}
