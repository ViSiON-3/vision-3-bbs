package menu

import (
	"errors"
	"fmt"
	"io"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
)

// newsViewerMinBody is the smallest body window worth drawing. Below this the
// terminal is too short to page anything usefully, and the viewer falls back to
// writing the item out in sequence.
const newsViewerMinBody = 3

// showNewsItem draws a news item and lets the reader scroll it.
//
// News items can be longer than the terminal, and a "press any key" pause at
// the end simply loses whatever scrolled past. The body is drawn into a window
// under the header and moved with the arrow and page keys, the same bindings
// the message reader uses, with ESC, Q or ENTER to move on (#372).
//
// Returns true if the reader asked to stop rather than continue to the next
// item.
func showNewsItem(c *cmdCtx, item *NewsItem, idx int) (quit bool, err error) {
	terminal := c.terminal
	outputMode := c.outputMode

	hdr, headerWidth := renderNewsHeader(c.e, item, idx, outputMode)
	body := renderNewsBody(item, headerWidth, c.termWidth, outputMode)

	headerRows := findHeaderEndRow(hdr)
	bodyStartRow := headerRows + 1
	// One row for the status line at the foot of the screen.
	availRows := c.termHeight - bodyStartRow - 1

	if availRows < newsViewerMinBody {
		// Nothing sensible to page in: write it out and fall back to the
		// ordinary pause so the reader still gets to see it.
		displayNewsItem(c.e, terminal, item, idx, outputMode, c.termWidth)
		c.e.holdScreen(c.s, terminal, outputMode, c.termWidth, c.termHeight)
		return false, nil
	}

	// A line's colour can be set by a line above it, so a window that starts
	// part-way down has to restore the state it inherits - the same fold the
	// message reader does (#362).
	//
	// Seeded from the header rather than from grey: trimTrailingBlankRows
	// deliberately keeps the colour the header ends on so the body inherits
	// it, and starting the fold at the default would throw that away for any
	// header that does not end grey.
	headerState := ansi.NewSGRState()
	headerState.Write(bodyDefaultColour)
	headerState.Write(string(hdr))
	entryState := buildBodyEntryStatesFrom(body, headerState.Escape())

	offset := 0
	maxOffset := len(body) - availRows
	if maxOffset < 0 {
		maxOffset = 0
	}
	scrollable := maxOffset > 0

	drawBody := func() {
		for i := 0; i < availRows; i++ {
			row := bodyStartRow + i
			terminalio.WriteProcessedBytes(terminal, []byte(ansi.MoveCursor(row, 1)), outputMode)
			// Reset before clearing: erase-in-line paints with the current
			// background, so without this a line that set one would smear it
			// across the rest of the row.
			terminalio.WriteProcessedBytes(terminal, []byte("\x1b[0m\x1b[K"), outputMode)
			if idx := offset + i; idx < len(body) {
				terminalio.WriteProcessedBytes(terminal, []byte(entryState[idx]), outputMode)
				terminalio.WriteProcessedBytes(terminal, []byte(body[idx]), outputMode)
			}
		}
	}

	drawStatus := func() {
		terminalio.WriteProcessedBytes(terminal, []byte(ansi.MoveCursor(c.termHeight, 1)), outputMode)
		terminalio.WriteProcessedBytes(terminal, []byte("\x1b[0m\x1b[K"), outputMode)
		var status string
		if scrollable {
			shown := offset + availRows
			if shown > len(body) {
				shown = len(body)
			}
			status = fmt.Sprintf("|08[|07%d-%d of %d|08] |07Arrows/PgUp/PgDn |08scroll  |07ESC|08/|07Q|08/|07ENTER|08 continue ",
				offset+1, shown, len(body))
		} else {
			status = "|07Press |15ENTER|07 to continue... "
		}
		wv(terminal, status, outputMode)
	}

	drawAll := func() {
		terminalio.WriteProcessedBytes(terminal, []byte(ansi.ClearScreen()), outputMode)
		terminalio.WriteProcessedBytes(terminal, hdr, outputMode)
		drawBody()
		drawStatus()
	}

	// restore puts the terminal back in a sane state for whatever the caller
	// writes next. drawStatus leaves the cursor parked on the last row with a
	// colour still in force, and the pause prompt that may follow only moves
	// horizontally - it would be written over the status line in that colour.
	// Not called when input fails: the connection is gone and there is nothing
	// to tidy.
	restore := func() {
		terminalio.WriteProcessedBytes(terminal, []byte(ansi.MoveCursor(c.termHeight, 1)), outputMode)
		terminalio.WriteProcessedBytes(terminal, []byte("\x1b[0m\x1b[K"), outputMode)
	}

	pageSize := availRows - 1
	if pageSize < 1 {
		pageSize = 1
	}

	sessionIH := getSessionIH(c.s)
	drawAll()

	for {
		key, keyErr := sessionIH.ReadKey()
		if keyErr != nil {
			if errors.Is(keyErr, io.EOF) {
				return true, io.EOF
			}
			return false, keyErr
		}

		prev := offset
		switch key {
		case editor.KeyEsc, 'q', 'Q':
			restore()
			return true, nil
		case editor.KeyEnter, ' ':
			restore()
			return false, nil

		case editor.KeyArrowUp, editor.KeyCtrlE:
			offset--
		case editor.KeyArrowDown, editor.KeyCtrlX:
			offset++
		case editor.KeyPageUp, editor.KeyCtrlR:
			offset -= pageSize
		case editor.KeyPageDown, editor.KeyCtrlC:
			offset += pageSize
		case editor.KeyHome:
			offset = 0
		case editor.KeyEnd:
			offset = maxOffset
		default:
			// Any other key moves on, which keeps the old "press a key"
			// reflex working for readers who are not scrolling.
			restore()
			return false, nil
		}

		if offset > maxOffset {
			offset = maxOffset
		}
		if offset < 0 {
			offset = 0
		}
		if offset != prev {
			drawBody()
			drawStatus()
		}
	}
}
