package menu

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/gliderlabs/ssh"
	"github.com/rivo/uniseg"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
)

// chatInputMaxLen caps the length of one chat message, in characters. It
// applies to the whole message, however many rows it wraps onto.
const chatInputMaxLen = 500

// chatInputMaxRows is the most rows the input area grows to. A longer message
// keeps its last rows on screen.
const chatInputMaxRows = 5

// chatMinRegionRows is the fewest rows the chat scroll region is left with
// when the input area grows.
const chatMinRegionRows = 3

// chatScreen is the part of the full-screen chat layout below the header:
// the scroll region where messages appear, the two status-bar rows under it,
// and the input area on the bottom rows, which grows upward as the typed line
// wraps (#657). Every row position is derived from inputRows here, so the
// rest of the chat code asks the screen where things are instead of keeping
// its own constants.
//
// The fields and methods are guarded by the chat session's rawMu: callers
// hold it, as they do for every other write to the session.
type chatScreen struct {
	height    int // screen rows
	width     int // screen columns
	chatTop   int // first row of the scroll region, below the header
	inputRows int // rows the input area uses, from 1

	// curRow and curCol are where the input cursor belongs. Writes elsewhere
	// on the screen put the cursor back here when they finish.
	curRow, curCol int

	write      func([]byte) // raw write to the session
	drawStatus func()       // redraws the status bars at statusRow; may be nil

	// redrawRegion, when set, redraws the scroll region's contents from the
	// scrollback after a resize, in place of scrolling the terminal.
	redrawRegion func()
	// page, when set, is called with -1 for PgUp and 1 for PgDn while a line
	// is being typed.
	page func(dir int)
}

// scrollBottom returns the last row of the chat scroll region.
func (c *chatScreen) scrollBottom() int {
	return max(c.chatTop, c.height-c.inputRows-2)
}

// statusRow returns the first of the two status-bar rows.
func (c *chatScreen) statusRow() int { return c.height - c.inputRows - 1 }

// inputTop returns the first row of the input area.
func (c *chatScreen) inputTop() int { return c.height - c.inputRows + 1 }

// maxInputRows returns how far the input area may grow on this screen: up to
// chatInputMaxRows, leaving the scroll region at least chatMinRegionRows.
func (c *chatScreen) maxInputRows() int {
	return max(1, min(chatInputMaxRows, c.height-c.chatTop-1-chatMinRegionRows))
}

// setRegion writes the DECSTBM sequence for the current scroll region.
func (c *chatScreen) setRegion() {
	c.write(fmt.Appendf(nil, "\x1B[%d;%dr", c.chatTop, c.scrollBottom()))
}

// restoreCursor puts the cursor back in the input area.
func (c *chatScreen) restoreCursor() {
	if c.curRow > 0 {
		c.write([]byte(ansi.MoveCursor(c.curRow, c.curCol)))
	} else {
		c.write([]byte(ansi.MoveCursor(c.height, 1)))
	}
}

// resize changes the input area to n rows, keeping the most recent messages
// in view. With redrawRegion set, the region is redrawn from the scrollback.
// Without it, growing scrolls the chat region up by the rows it gives away,
// before the region shrinks, so the newest lines stay just above the status
// bars; shrinking widens the region and scrolls it down by the rows it gains,
// which pushes the old status bars and input rows out of the bottom of the
// region. The status bars are then redrawn at their new rows. The caller
// redraws the input rows.
func (c *chatScreen) resize(n int) {
	n = max(1, min(n, c.maxInputRows()))
	if n == c.inputRows {
		return
	}
	oldBottom := c.scrollBottom()
	c.inputRows = n
	newBottom := c.scrollBottom()
	switch {
	case c.redrawRegion != nil:
		c.setRegion()
		c.redrawRegion()
	case newBottom < oldBottom:
		c.write([]byte(ansi.MoveCursor(oldBottom, 1)))
		c.write([]byte(strings.Repeat("\r\n", oldBottom-newBottom)))
		c.setRegion()
	case newBottom > oldBottom:
		c.setRegion()
		c.write([]byte(ansi.MoveCursor(c.chatTop, 1)))
		c.write([]byte(strings.Repeat("\x1BM", newBottom-oldBottom)))
	}
	if c.drawStatus != nil {
		c.drawStatus()
	}
}

// chatWrapInput splits typed text into the rows the input area shows, each at
// most width terminal cells; a wide character takes two. Words are never
// split: a word that does not fit on the current row moves whole to the next
// one. Only a word longer than a whole row is cut, at the row's edge, and it
// starts on a row of its own. Spaces that fall at a wrap are dropped from the
// display; a trailing space that does not fit starts an empty last row, where
// the cursor goes. The result always has at least one row. The text itself is
// not changed; this is display only.
//
// For example, "hello world" at width 10 is ["hello ", "world"].
func chatWrapInput(text []rune, width int) [][]rune {
	width = max(width, 1)
	var rows [][]rune
	var cur []rune
	for i := 0; i < len(text); {
		j := i
		space := text[i] == ' '
		for j < len(text) && (text[j] == ' ') == space {
			j++
		}
		tok := text[i:j]
		i = j
		switch {
		case chatCells(cur)+chatCells(tok) <= width:
			cur = append(cur, tok...)
		case space:
			rows = append(rows, cur)
			cur = nil
		case chatCells(tok) <= width:
			rows = append(rows, cur)
			cur = append([]rune(nil), tok...)
		default:
			if len(cur) > 0 {
				rows = append(rows, cur)
			}
			for len(tok) > 1 && chatCells(tok) > width {
				n := chatFitRunes(tok, width)
				rows = append(rows, tok[:n])
				tok = tok[n:]
			}
			cur = append([]rune(nil), tok...)
		}
	}
	return append(rows, cur)
}

// chatCells is the number of terminal cells text takes, measured by
// grapheme cluster, so an emoji joined from several runes counts once.
func chatCells(text []rune) int {
	return uniseg.StringWidth(string(text))
}

// chatFitRunes is how many leading runes of text fit in width cells without
// splitting a grapheme cluster, and at least one whole cluster, so a wide
// character on a one-cell row still makes progress.
func chatFitRunes(text []rune, width int) int {
	cells, n := 0, 0
	g := uniseg.NewGraphemes(string(text))
	for g.Next() {
		size := len(g.Runes())
		if cells += g.Width(); cells > width {
			if n == 0 {
				return size
			}
			return n
		}
		n += size
	}
	return n
}

// chatLastGrapheme is how many runes the last grapheme cluster of text takes,
// so Backspace removes a joined emoji whole.
func chatLastGrapheme(text []rune) int {
	last := 0
	g := uniseg.NewGraphemes(string(text))
	for g.Next() {
		last = len(g.Runes())
	}
	return last
}

// chatReadLine reads one chat message using the session's InputHandler (the
// same path as all other BBS input), echoing it in scr's input area, which
// grows a row at a time as the text wraps and returns to one row on Enter.
// PgUp and PgDn are passed to scr.page, under mu.
// The message can be up to chatInputMaxLen characters; further keys are
// ignored. It returns the text with surrounding spaces trimmed.
//
// Every terminal write takes mu, so it is serialised with the event
// goroutine's writes into the chat region. Ctrl-C, Ctrl-A, ESC and a closed
// connection all return io.EOF.
func chatReadLine(s ssh.Session, mu *sync.Mutex, scr *chatScreen, prompt string) (string, error) {
	ih := getSessionIH(s)
	promptLen := uniseg.StringWidth(prompt)
	indent := strings.Repeat(" ", promptLen)

	var buf []rune
	mode := sessionOutputMode(s)
	var utf8Pending []byte // bytes of a UTF-8 character still arriving

	// redrawLocked lays the text out, resizes the input area to fit and draws
	// it. When there are more rows than the area holds, the last ones show.
	redrawLocked := func() {
		rows := chatWrapInput(buf, scr.width-promptLen-1)
		scr.resize(len(rows))
		first := len(rows) - scr.inputRows
		var b strings.Builder
		for i := 0; i < scr.inputRows; i++ {
			fmt.Fprintf(&b, "\x1B[%d;1H\x1B[2K", scr.inputTop()+i)
			if idx := first + i; idx >= 0 {
				if idx == 0 {
					b.WriteString(prompt)
				} else {
					b.WriteString(indent)
				}
				b.WriteString(string(rows[idx]))
			}
		}
		last := rows[len(rows)-1]
		scr.curRow, scr.curCol = scr.height, promptLen+chatCells(last)+1
		fmt.Fprintf(&b, "\x1B[%d;%dH", scr.curRow, scr.curCol)
		scr.write([]byte(b.String()))
	}
	redraw := func() {
		mu.Lock()
		defer mu.Unlock()
		redrawLocked()
	}

	redraw()

	for {
		key, err := ih.ReadKey()
		if err != nil {
			return "", io.EOF
		}
		if key < 128 || key > 255 {
			utf8Pending = nil // a partial sequence must not absorb the next key
		}

		switch key {
		case editor.KeyEnter:
			line := strings.TrimSpace(string(buf))
			buf = buf[:0]
			redraw()
			return line, nil

		case editor.KeyBackspace:
			if len(buf) > 0 {
				buf = buf[:len(buf)-chatLastGrapheme(buf)]
				redraw()
			}

		case editor.KeyEsc, editor.KeyCtrlC, editor.KeyCtrlA:
			return "", io.EOF

		case editor.KeyPageUp, editor.KeyPageDown:
			if scr.page != nil {
				dir := 1
				if key == editor.KeyPageUp {
					dir = -1
				}
				mu.Lock()
				scr.page(dir)
				mu.Unlock()
			}

		default:
			// Bytes 128-255 are part of a UTF-8 character or a CP437
			// character, depending on the terminal; decode them to a rune.
			if key >= 128 && key <= 255 {
				var decoded []byte
				decoded, _, utf8Pending = ansi.DecodeExtendedKey(nil, mode, byte(key), utf8Pending)
				if len(decoded) > 0 && len(buf) < chatInputMaxLen {
					r, _ := utf8.DecodeRune(decoded)
					buf = append(buf, r)
					redraw()
				}
				continue
			}
			if editor.IsPrintable(key) && len(buf) < chatInputMaxLen {
				buf = append(buf, rune(key))
				redraw()
			}
		}
	}
}
