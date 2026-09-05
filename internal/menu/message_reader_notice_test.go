package menu

import (
	"strings"
	"testing"
)

func TestTrimNoticeLeadingBlank(t *testing.T) {
	cases := []struct{ in, want string }{
		// The shipped strings: each opens with a newline for scrolling output,
		// which is exactly what scrolls the screen on a positioned row.
		{"\r\n|07End of messages.|07", "|07End of messages.|07"},
		{"\r\n|07Already at first message.|07", "|07Already at first message.|07"},
		{"\r\n|07Mail reply not yet implemented.|07", "|07Mail reply not yet implemented.|07"},
		// Multiple leading breaks, in any order.
		{"\n\r\n\r\nText", "Text"},
		// Nothing to strip.
		{"|07Plain", "|07Plain"},
		{"", ""},
		// Trailing breaks are left alone: harmless on the last row, and
		// stripping them would change how the strings render elsewhere.
		{"\r\nBoth ends\r\n", "Both ends\r\n"},
	}
	for _, c := range cases {
		if got := trimNoticeLeadingBlank(c.in); got != c.want {
			t.Errorf("trimNoticeLeadingBlank(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// Every notice routed through the helper must begin with a break in its
// stored form — that is what made them scroll — and must have none once
// trimmed. If a string is ever rewritten without the leading newline this
// still passes; the point is that the trimmed form never starts with one.
func TestNoticeStringsCarryNoLeadingBreakAfterTrim(t *testing.T) {
	for _, s := range []string{
		"\r\n|07End of messages.|07",
		"\r\n|07Already at first message.|07",
		"\r\n|07Mail reply not yet implemented.|07",
	} {
		got := trimNoticeLeadingBlank(s)
		if strings.HasPrefix(got, "\r") || strings.HasPrefix(got, "\n") {
			t.Errorf("trimmed notice still starts with a line break: %q", got)
		}
		if got == "" {
			t.Errorf("notice %q trimmed to nothing", s)
		}
	}
}

// The erase-line sequence must not move the cursor: the notice is written
// immediately after it, on the row the helper just positioned to.
func TestEraseLineDoesNotMoveTheCursor(t *testing.T) {
	if eraseLine != "\x1b[2K" {
		t.Errorf("eraseLine = %q, want the CSI 2K erase-in-line sequence", eraseLine)
	}
	if strings.ContainsAny(eraseLine, "Hf") {
		t.Errorf("eraseLine %q contains a cursor-positioning final byte", eraseLine)
	}
}
