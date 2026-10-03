package menu

import (
	"regexp"
	"strings"
	"testing"
)

// splitArtBody builds a message body the way some systems post autowrapped
// ANSI art: one continuous stream that relies on the terminal wrapping at
// column 80, cut into short message lines with ESC[s CR ESC[u so each line
// stays under the FTN length limit while the art resumes where it was cut.
// Line ends are LF, as the message manager hands bodies to the reader.
func splitArtBody() string {
	stream := "\x1b[0;37m" + strings.Repeat("A", 80) + "\x1b[1;34m" + strings.Repeat("B", 80)
	var b strings.Builder
	for len(stream) > 0 {
		n := min(37, len(stream))
		b.WriteString(stream[:n])
		stream = stream[n:]
		if len(stream) > 0 {
			b.WriteString("\x1b[s\n\x1b[u")
		}
	}
	b.WriteString("\n--- tosser\n * Origin: Example (21:1/1)\n")
	return b.String()
}

func TestDetectAnsiArt_CursorSaveRestoreOnly(t *testing.T) {
	if !detectAnsiArtInMessage(splitArtBody()) {
		t.Fatal("art using only ESC[s/ESC[u not detected as ANSI art; it would be split at every CR and break on scroll")
	}
	if detectAnsiArtInMessage("Plain text with \x1b[1;31mcolour\x1b[0m only.\n") {
		t.Error("colour-only text detected as ANSI art")
	}
}

func TestRenderSplitArt_RowsAreWholeAndSelfContained(t *testing.T) {
	strip := regexp.MustCompile("\x1b\\[[0-9;?]*[A-Za-z]")
	lines := RenderANSIArtToLines(splitArtBody(), 80, 500)
	if len(lines) < 2 {
		t.Fatalf("got %d lines, want at least 2", len(lines))
	}
	for i, want := range []string{strings.Repeat("A", 80), strings.Repeat("B", 80)} {
		if got := strings.TrimRight(strip.ReplaceAllString(lines[i], ""), " "); got != want {
			t.Errorf("row %d = %q, want %q", i+1, got, want)
		}
	}
	for i, l := range lines {
		if strings.ContainsAny(l, "\r\n") || strings.Contains(l, "\x1b[s") || strings.Contains(l, "\x1b[u") {
			t.Errorf("row %d carries cursor control, so it cannot be redrawn on its own when scrolling: %q", i+1, l)
		}
	}
}

// ESC 7 / ESC 8 split art the same way ESC[s / ESC[u does, with or without
// any CSI sequence in the message, and DECRC brings back the colour too.
func TestSplitArt_DECSaveRestore(t *testing.T) {
	for name, body := range map[string]string{
		"no CSI":   "AB\x1b7\n\x1b8CD\n",
		"with CSI": "\x1b[31mAB\x1b7\n\x1b8CD\n",
	} {
		t.Run(name, func(t *testing.T) {
			if !detectAnsiArtInMessage(body) {
				t.Fatal("not detected as ANSI art")
			}
			lines := RenderANSIArtToLines(body, 80, 10)
			if got := strings.TrimRight(regexp.MustCompile("\x1b\\[[0-9;?]*[A-Za-z]").ReplaceAllString(lines[0], ""), " "); got != "ABCD" {
				t.Errorf("row 1 = %q, want ABCD", got)
			}
		})
	}

	// The colour saved with the cursor comes back with it.
	r := NewANSIRenderer(20, 3)
	r.Render("\x1b[31m\x1b7\x1b[34mA\x1b8B")
	if c := r.Buffer[0][0]; c.Char != 'B' || !strings.Contains(c.Style, "31") {
		t.Errorf("cell 0 = %q in %q; want B in the red saved by ESC 7", c.Char, c.Style)
	}
}
