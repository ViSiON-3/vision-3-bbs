// Package testterm provides an ANSI-interpreting virtual terminal for testing
// terminal output, plus a scripted session for driving interactive code.
//
// A Term is an io.Writer: hand it to the code under test in place of the real
// connection, then assert on the resulting screen with Row, Cell, Cursor and
// Snapshot instead of on raw escape sequences.
//
// Term is not a terminal emulator. It models the subset of ANSI that
// internal/editor emits and records everything else in Unhandled, so a test can
// catch output the double does not understand rather than silently passing.
package testterm

import "strings"

// Cell is one character position on the screen.
//
// Fg and Bg hold the SGR parameter as written (30-37 and 90-97 for foreground,
// 40-47 and 100-107 for background); -1 means the terminal default.
type Cell struct {
	Rune   rune
	Fg, Bg int
	Bold   bool
}

func blankCell() Cell { return Cell{Rune: ' ', Fg: -1, Bg: -1} }

// Option configures a Term at construction.
type Option func(*Term)

// Term is a virtual screen that records what was drawn on it.
type Term struct {
	width, height int
	cells         [][]Cell

	row, col           int // cursor, 1-based
	savedRow, savedCol int
	cursorHidden       bool

	pen   Cell // current SGR state; Rune is unused
	cp437 bool

	pending   []byte // bytes held back mid-sequence or mid-rune
	unhandled []string
}

// New creates a Term of the given size with an empty screen and the cursor at
// row 1, column 1.
func New(width, height int, opts ...Option) *Term {
	t := &Term{
		width:  width,
		height: height,
		row:    1,
		col:    1,
		pen:    blankCell(),
	}
	t.cells = make([][]Cell, height)
	for r := range t.cells {
		t.cells[r] = make([]Cell, width)
		for c := range t.cells[r] {
			t.cells[r][c] = blankCell()
		}
	}
	t.savedRow, t.savedCol = 1, 1
	for _, opt := range opts {
		opt(t)
	}
	return t
}

// Row returns the text of a 1-based row with trailing blanks removed.
// An out-of-range row returns "".
func (t *Term) Row(row int) string {
	if row < 1 || row > t.height {
		return ""
	}
	var sb strings.Builder
	for _, c := range t.cells[row-1] {
		sb.WriteRune(c.Rune)
	}
	return strings.TrimRight(sb.String(), " ")
}

// Cell returns the cell at a 1-based row and column. Out-of-range coordinates
// return a blank cell so that a mistaken assertion fails rather than panics.
func (t *Term) Cell(row, col int) Cell {
	if row < 1 || row > t.height || col < 1 || col > t.width {
		return blankCell()
	}
	return t.cells[row-1][col-1]
}

// Cursor returns the 1-based cursor position.
func (t *Term) Cursor() (row, col int) { return t.row, t.col }

// CursorVisible reports whether the cursor is shown (ESC[?25h / ESC[?25l).
func (t *Term) CursorVisible() bool { return !t.cursorHidden }

// Snapshot returns the whole screen as text: rows joined by "\n", with trailing
// blanks on each row and trailing blank rows removed. Colour is deliberately
// excluded — assert colour through Cell.
func (t *Term) Snapshot() string {
	rows := make([]string, t.height)
	last := -1
	for i := 1; i <= t.height; i++ {
		rows[i-1] = t.Row(i)
		if rows[i-1] != "" {
			last = i - 1
		}
	}
	if last < 0 {
		return ""
	}
	return strings.Join(rows[:last+1], "\n")
}

// Unhandled returns the escape sequences this Term consumed but did not model,
// in the order they arrived. A test that wants to be sure the double kept up
// with the code under test asserts this is empty.
func (t *Term) Unhandled() []string {
	out := make([]string, len(t.unhandled))
	copy(out, t.unhandled)
	return out
}

// putRune writes r at the cursor using the current pen and advances the cursor,
// wrapping at the right margin and scrolling at the bottom of the screen.
func (t *Term) putRune(r rune) {
	if t.col > t.width {
		t.col = 1
		t.row++
	}
	if t.row > t.height {
		t.scrollUp()
		t.row = t.height
	}
	if t.row >= 1 && t.row <= t.height && t.col >= 1 && t.col <= t.width {
		t.cells[t.row-1][t.col-1] = Cell{Rune: r, Fg: t.pen.Fg, Bg: t.pen.Bg, Bold: t.pen.Bold}
	}
	t.col++
}

// scrollUp discards the top row and appends a blank row at the bottom.
func (t *Term) scrollUp() {
	if t.height == 0 {
		return
	}
	copy(t.cells, t.cells[1:])
	last := make([]Cell, t.width)
	for c := range last {
		last[c] = blankCell()
	}
	t.cells[t.height-1] = last
}
