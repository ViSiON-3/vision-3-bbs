package menu

import (
	"sync"
	"sync/atomic"
	"unicode/utf8"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"golang.org/x/term"
)

// terminalPhysicalWidths remembers the column count each client actually
// reported (PTY request, telnet NAWS/CPR, resize events), keyed by the
// session's *term.Terminal. It is tracked separately from the termWidth passed
// around the menu code because that follows the user's saved screen-size
// preference: a caller who keeps an 80-column preference on a 120-column
// window still has a terminal that autowraps at 120, so art must be
// hard-wrapped for it (see artWidth). Keyed by terminal rather than session
// because that is what every art-writing call site already has in hand.
var terminalPhysicalWidths sync.Map // *term.Terminal -> *atomic.Int32

// RegisterTerminalPhysicalWidth associates t with the width the session
// keeps up to date from the client. The caller owns width and stores new
// values into it on resize, so updates never touch the map and cannot re-add
// an entry after ClearTerminalPhysicalWidth.
func RegisterTerminalPhysicalWidth(t *term.Terminal, width *atomic.Int32) {
	terminalPhysicalWidths.Store(t, width)
}

// ClearTerminalPhysicalWidth drops the remembered width when a session ends.
func ClearTerminalPhysicalWidth(t *term.Terminal) {
	terminalPhysicalWidths.Delete(t)
}

// artWidth returns the width art should be fitted to on t: the larger of the
// session's logical termWidth and the client's physical width. Art only needs
// hard-wrapping when the real terminal is wider than it, whatever width the
// user asked the BBS to lay screens out for.
func artWidth(t *term.Terminal, termWidth int) int {
	if v, ok := terminalPhysicalWidths.Load(t); ok {
		if w := int(v.(*atomic.Int32).Load()); w > termWidth {
			return w
		}
	}
	return termWidth
}

// fitArt is ansi.FitArtToWidth against t's art width (see artWidth).
func fitArt(t *term.Terminal, data []byte, termWidth int, utf8Spans bool) []byte {
	return ansi.FitArtToWidth(data, artWidth(t, termWidth), utf8Spans)
}

// artForOutput returns art in the encoding the terminal expects. On a UTF-8
// terminal, art that is not valid UTF-8 as a whole is CP437 and is converted
// byte for byte; anything else is returned unchanged.
//
// The decision has to be made for the whole file. Left to
// terminalio.WriteProcessedBytes, it is made per span between escape
// sequences, and short CP437 runs are often valid UTF-8 on their own — █▓
// (DB B2) decodes as U+06F2 — so they went out raw: one wrong glyph in place
// of two cells, shifting the rest of the row and its colours.
func artForOutput(data []byte, outputMode ansi.OutputMode) []byte {
	if outputMode != ansi.OutputModeUTF8 || utf8.Valid(data) {
		return data
	}
	return ansi.CP437BytesToUTF8(data)
}
