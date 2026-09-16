package wfcui

import (
	"strings"

	"github.com/mattn/go-runewidth"
)

// cell is one terminal character with its palette attributes.
type cell struct {
	ch rune
	fg uint8
	bg uint8
}

// screen is a fixed-size character grid the view paints into before
// serialising it to a string. Painting into cells rather than concatenating
// styled strings is what makes the layout robust: every row is exactly the
// terminal width, overlays can be drawn on top of anything, and there is no
// way for a long value to wrap a row and push the frame off the bottom.
type screen struct {
	w, h  int
	cells []cell
}

func newScreen(w, h int) *screen {
	s := &screen{w: w, h: h, cells: make([]cell, w*h)}
	s.fill(0, 0, w, h, ' ', cLightGray, cBlack)
	return s
}

// set writes one cell; writes outside the grid are dropped.
func (s *screen) set(x, y int, ch rune, fg, bg uint8) {
	if x < 0 || y < 0 || x >= s.w || y >= s.h {
		return
	}
	s.cells[y*s.w+x] = cell{ch: ch, fg: fg, bg: bg}
}

// fill paints a rectangle with one glyph and attribute pair.
func (s *screen) fill(x, y, w, h int, ch rune, fg, bg uint8) {
	for yy := y; yy < y+h; yy++ {
		for xx := x; xx < x+w; xx++ {
			s.set(xx, yy, ch, fg, bg)
		}
	}
}

// text writes str starting at (x, y), clipping at maxX (exclusive; pass
// s.w for no clip) and returns the x after the last glyph written. Runes that
// would not occupy exactly one terminal cell (wide, zero-width, unprintable)
// are replaced by '?' so a hostile or exotic handle cannot shift the columns
// to its right.
func (s *screen) text(x, y int, str string, fg, bg uint8, maxX int) int {
	if maxX > s.w {
		maxX = s.w
	}
	for _, r := range str {
		if x >= maxX {
			break
		}
		if runewidth.RuneWidth(r) != 1 {
			r = '?'
		}
		s.set(x, y, r, fg, bg)
		x++
	}
	return x
}

// textPad writes str left-aligned in a field of width w, padding with
// spaces in the same attributes and clipping to the field.
func (s *screen) textPad(x, y, w int, str string, fg, bg uint8) {
	end := s.text(x, y, str, fg, bg, x+w)
	for xx := end; xx < x+w && xx < s.w; xx++ {
		s.set(xx, y, ' ', fg, bg)
	}
}

// textCenter writes str centred in a field of width w (no padding).
func (s *screen) textCenter(x, y, w int, str string, fg, bg uint8) {
	n := runeCount(str)
	if n > w {
		n = w
	}
	s.text(x+(w-n)/2, y, str, fg, bg, x+w)
}

// textRight writes str right-aligned so it ends at x+w (exclusive).
func (s *screen) textRight(x, y, w int, str string, fg, bg uint8) {
	n := runeCount(str)
	if n > w {
		n = w
	}
	s.text(x+w-n, y, str, fg, bg, x+w)
}

// boxColors lets a box shade its edges the way the mockup does: the top and
// left edges in the base colour and the right and bottom edges in a brighter
// one, as if lit from the top-left.
type boxColors struct {
	dim, bright uint8
}

// box draws a single-line frame with its top-left corner at (x, y). The
// interior is not touched.
func (s *screen) box(x, y, w, h int, c boxColors) {
	if w < 2 || h < 2 {
		return
	}
	s.set(x, y, gTL, c.dim, cBlack)
	s.set(x+w-1, y, gTR, c.bright, cBlack)
	s.set(x, y+h-1, gBL, c.dim, cBlack)
	s.set(x+w-1, y+h-1, gBR, c.bright, cBlack)
	for xx := x + 1; xx < x+w-1; xx++ {
		s.set(xx, y, gH, c.dim, cBlack)
		s.set(xx, y+h-1, gH, c.bright, cBlack)
	}
	for yy := y + 1; yy < y+h-1; yy++ {
		s.set(x, yy, gV, c.dim, cBlack)
		s.set(x+w-1, yy, gV, c.bright, cBlack)
	}
}

// tab draws the "▄▀▄ Title ▄▀▄" caption centred across width w on row y.
func (s *screen) tab(x, y, w int, title string, wing, titleFg, titleBg uint8) {
	label := " " + title + " "
	total := runeCount(label) + 6 // "▄▀▄" + label + "▄▀▄"
	if total > w {
		total = w
	}
	start := x + (w-total)/2
	wings := string([]rune{gLower, gUpper, gLower})
	xx := s.text(start, y, wings, wing, cBlack, x+w)
	xx = s.text(xx, y, label, titleFg, titleBg, x+w)
	s.text(xx, y, wings, wing, cBlack, x+w)
}

// render serialises the grid: one line per row, runs of identical attributes
// wrapped in a single style. With NoColor the output is the bare text.
func (s *screen) render(st *styler) string {
	var b strings.Builder
	b.Grow(s.w*s.h + s.h*64)
	var run strings.Builder
	for y := 0; y < s.h; y++ {
		row := s.cells[y*s.w : (y+1)*s.w]
		if st.noColor {
			for _, c := range row {
				b.WriteRune(st.glyph(c.ch))
			}
		} else {
			run.Reset()
			cur := row[0]
			for _, c := range row {
				if c.fg != cur.fg || c.bg != cur.bg {
					b.WriteString(st.style(cur.fg, cur.bg).Render(run.String()))
					run.Reset()
					cur = c
				}
				run.WriteRune(st.glyph(c.ch))
			}
			b.WriteString(st.style(cur.fg, cur.bg).Render(run.String()))
		}
		if y < s.h-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// runeCount is len([]rune(s)) without the allocation.
func runeCount(s string) int {
	n := 0
	for range s {
		n++
	}
	return n
}
