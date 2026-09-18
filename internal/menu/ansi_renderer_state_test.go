package menu

import (
	"regexp"
	"strings"
	"testing"
)

var reCellStyle = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// styleAt returns the style in force at the first character of row n.
func styleAt(lines []string, n int) string {
	if n >= len(lines) {
		return ""
	}
	if m := reCellStyle.FindString(lines[n]); m != "" {
		return m
	}
	return ""
}

// ANSI art sets one attribute at a time and expects the rest to persist.
// Keeping only the last sequence dropped everything it did not mention, so a
// background-only change reverted the foreground to the terminal default -
// which is how dark-grey-on-black shading turned into black on black.
func TestANSIRendererAccumulatesGraphicState(t *testing.T) {
	for _, tc := range []struct {
		name string
		art  string
		want string
	}{
		{
			name: "background-only keeps the foreground",
			art:  "\x1b[1;30m\x1b[40mX",
			want: "\x1b[0;1;30;40m",
		},
		{
			name: "foreground-only keeps the background",
			art:  "\x1b[46m\x1b[31mX",
			want: "\x1b[0;31;46m",
		},
		{
			name: "later foreground replaces the earlier one",
			art:  "\x1b[31m\x1b[36mX",
			want: "\x1b[0;36m",
		},
		{
			name: "reset clears everything",
			art:  "\x1b[1;31;47m\x1b[0mX",
			want: "\x1b[0m", // the default state, written out explicitly
		},
		{
			name: "bold persists across a colour change",
			art:  "\x1b[1m\x1b[32mX",
			want: "\x1b[0;1;32m",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lines := RenderANSIArtToLines(tc.art, 79, 5)
			if got := styleAt(lines, 0); got != tc.want {
				t.Errorf("style = %q, want %q (line %q)", got, tc.want, lines[0])
			}
		})
	}
}

// Art in this echo reaches us truncated at 79 bytes, cutting escape sequences
// in half. The parser consumed only "ESC[" and left the parameter bytes to be
// drawn, so rows ended in visible text like "1;30" or "[37;46".
func TestANSIRendererSwallowsTruncatedEscapes(t *testing.T) {
	for _, tc := range []struct {
		name string
		art  string
	}{
		{"cut inside the parameters", "AB\x1b[1;30\nCD"},
		{"cut after the bracket", "AB\x1b[\nCD"},
		{"cut at the escape", "AB\x1b\nCD"},
		{"cut at end of input", "AB\x1b[37;46"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lines := RenderANSIArtToLines(tc.art, 79, 5)
			for i, l := range lines {
				plain := reCellStyle.ReplaceAllString(l, "")
				for _, junk := range []string{"1;30", "37;46", "["} {
					if strings.Contains(plain, junk) {
						t.Errorf("line %d drew escape debris %q: %q", i, junk, plain)
					}
				}
			}
			// The real text either side must survive.
			if !strings.Contains(reCellStyle.ReplaceAllString(lines[0], ""), "AB") {
				t.Errorf("text before the truncated escape was lost: %q", lines[0])
			}
		})
	}
}

// A complete sequence must still be honoured after all this.
func TestANSIRendererStillHandlesCompleteSequences(t *testing.T) {
	lines := RenderANSIArtToLines("\x1b[5;3HX\x1b[1;33mY", 79, 10)
	if got := reCellStyle.ReplaceAllString(lines[4], ""); !strings.HasSuffix(strings.TrimRight(got, " "), "XY") {
		t.Errorf("positioned write lost: %q", got)
	}
	if !strings.Contains(lines[4], "\x1b[0;1;33m") {
		t.Errorf("colour not applied: %q", lines[4])
	}
}
