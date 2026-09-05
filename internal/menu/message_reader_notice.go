package menu

import (
	"strings"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"golang.org/x/term"
)

// readerNoticePause is how long a notice stays on the lightbar row before the
// reader moves on. Long enough to read a short line, short enough not to feel
// like a stall.
const readerNoticePause = 900 * time.Millisecond

// showReaderNotice writes a short message onto the lightbar row, replacing the
// menu rather than being appended below it.
//
// The reader parks the cursor on the last row after drawing the lightbar, so
// writing a notice that begins with a newline — as every one of these strings
// does — makes the terminal scroll the whole screen up a line to make room.
// The message body shifts under the reader's feet on every "no more messages",
// which is the jarring bump this avoids.
//
// Replacing the menu is also the right thing semantically: once the notice is
// showing, the reader is leaving or reloading, so the options underneath it are
// no longer selectable.
func showReaderNotice(terminal *term.Terminal, outputMode ansi.OutputMode, text string, termHeight int) {
	if termHeight > 0 {
		terminalio.WriteProcessedBytes(terminal, []byte(ansi.MoveCursor(termHeight, 1)), outputMode)
		terminalio.WriteProcessedBytes(terminal, []byte(eraseLine), outputMode)
	}
	terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(trimNoticeLeadingBlank(text))), outputMode)
	time.Sleep(readerNoticePause)
}

// eraseLine clears the current row without moving the cursor, so the notice
// lands on a clean line rather than on top of the menu it replaces.
const eraseLine = "\x1b[2K"

// trimNoticeLeadingBlank strips the leading CR/LF that these strings carry for
// use in scrolling output. Placed on a positioned row, that newline is exactly
// what causes the scroll.
//
// Trailing whitespace is left alone: a trailing newline is harmless on the last
// row, and stripping it would change how the strings render elsewhere.
func trimNoticeLeadingBlank(text string) string {
	return strings.TrimLeft(text, "\r\n")
}
