package menu

import (
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
)

// #854: a body the filter leaves a clear-screen or cursor move in goes through
// the art renderer, so nothing in it reaches the terminal to act on the
// reader's own screen. Before, only moves with a count were noticed.
func TestBodyCursorAndEraseSequencesStayInTheMessageArea(t *testing.T) {
	for _, seq := range []string{
		"\x1b[2J", "\x1b[J", "\x1b[1J",
		"\x1b[A", "\x1b[B", "\x1b[E", "\x1b[F", "\x1b[d",
		"\x1b[C", "\x1b[D", "\x1b[G",
	} {
		body := "Hello there\r\n" + seq + "and goodbye\r\n"
		lines := messageBodyLines(bodyForDisplay(body, ""), "", 80, ansi.OutputModeUTF8)
		for _, line := range lines {
			if strings.Contains(line, seq) {
				t.Errorf("%q reached the terminal in row %q", seq, line)
			}
		}
		if !detectAnsiArtInMessage(body) {
			t.Errorf("%q: body not sent to the art renderer", seq)
		}
	}
}

// Colour and erase-to-end-of-line act only within the body's own rows, so a
// body holding nothing else is still text: it word wraps and its quotes
// reflow.
func TestColourAndEraseLineBodyStaysText(t *testing.T) {
	for _, body := range []string{
		"plain \x1b[1;33mcolour\x1b[0m text",
		"a line\x1b[K\r\nanother\x1b[0K",
	} {
		if detectAnsiArtInMessage(body) {
			t.Errorf("%q taken for art", body)
		}
	}
}
