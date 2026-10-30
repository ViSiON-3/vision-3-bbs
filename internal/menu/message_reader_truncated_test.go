package menu

import (
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
)

func TestDropTruncatedEscapes(t *testing.T) {
	for name, tc := range map[string]struct{ in, want string }{
		"plain text":          {"no escapes\n", "no escapes\n"},
		"complete kept":       {"\x1b[1;30mA\x1b[0m\n", "\x1b[1;30mA\x1b[0m\n"},
		"cut params":          {"A\x1b[1;30\nB", "A\nB"},
		"cut after semicolon": {"A\x1b[1;\nB", "A\nB"},
		"cut after bracket":   {"A\x1b[\nB", "A\nB"},
		"bare ESC at line":    {"A\x1b\nB", "A\nB"},
		"CR kept":             {"A\x1b[37;46\r\nB", "A\r\nB"},
		"at end of body":      {"A\x1b[0;35", "A"},
		"bare ESC at end":     {"A\x1b", "A"},
		"cut by another ESC":  {"A\x1b[1\x1b[31mB", "A\x1b[31mB"},
		"bare ESC before ESC": {"A\x1b\x1b[31mB", "A\x1b[31mB"},
		"cut by CP437 byte":   {"A\x1b[1\xdcB", "A\xdcB"},
		"other escape kept":   {"\x1b(BA\x1b7", "\x1b(BA\x1b7"},
		"private mode kept":   {"\x1b[?7hA", "\x1b[?7hA"},
	} {
		t.Run(name, func(t *testing.T) {
			if got := dropTruncatedEscapes(tc.in); got != tc.want {
				t.Errorf("dropTruncatedEscapes(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// Colour-only art cut at 79 bytes, like qUantUm RaDio! in Agoranet's BBS and
// Network Ads: no cursor movement, so it takes the text path, not the renderer.
func TestMessageBodyLines_ColourOnlyArtCutMidEscape(t *testing.T) {
	body := "\xdc\xdc\x1b[1;30\n  \x1b[1;30m\xdf\xdf\x1b\n\x1b[36m\xdd\x1b[37;46\n\xdb\x1b[0m\n"
	if detectAnsiArtInMessage(body) {
		t.Fatal("colour-only body detected as ANSI art; this test no longer covers the text path")
	}
	for _, mode := range []ansi.OutputMode{ansi.OutputModeUTF8, ansi.OutputModeCP437} {
		lines := messageBodyLines(body, "", 80, mode)
		if len(lines) < 4 {
			t.Fatalf("got %d lines, want at least 4: %q", len(lines), lines)
		}
		for i, line := range lines {
			if strings.HasSuffix(line, "\x1b") || strings.Contains(line, "\x1b[1;30\x1b") ||
				strings.HasSuffix(line, "\x1b[1;30") || strings.HasSuffix(line, "\x1b[37;46") {
				t.Errorf("line %d still carries a cut-off escape: %q", i+1, line)
			}
		}
		if !strings.Contains(lines[1], "\x1b[1;30m") {
			t.Errorf("line 2 lost its complete colour code: %q", lines[1])
		}
	}
}
