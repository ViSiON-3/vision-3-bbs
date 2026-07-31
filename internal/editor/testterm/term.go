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

import (
	"fmt"
	"strings"
	"sync"
)

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

// CP437 makes the Term treat each byte 0x80-0xFF as one CP437 character,
// matching what ansi.OutputModeCP437 puts on the wire. Without it the stream is
// read as UTF-8.
func CP437() Option { return func(t *Term) { t.cp437 = true } }

// Term is a virtual screen that records what was drawn on it.
//
// Term is safe for concurrent use: Write and every accessor take an internal
// mutex. That matters when a test drives interactive code through a Session —
// the code under test writes to the Term on its own goroutine while the test
// goroutine reads Row, Cell, Cursor or Snapshot to make assertions.
type Term struct {
	mu sync.Mutex

	width, height int
	cells         [][]Cell

	row, col           int // cursor, 1-based
	savedRow, savedCol int
	cursorHidden       bool
	wrapPending        bool // set when a write landed exactly on the last column; see putRune

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
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.rowLocked(row)
}

// rowLocked is Row's implementation, callable from methods that already hold
// t.mu so they do not recursively lock it.
func (t *Term) rowLocked(row int) string {
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
	t.mu.Lock()
	defer t.mu.Unlock()
	if row < 1 || row > t.height || col < 1 || col > t.width {
		return blankCell()
	}
	return t.cells[row-1][col-1]
}

// Cursor returns the 1-based cursor position. After a write that lands
// exactly on the last column, the column stays at width — never past it — while
// the wrap to the next row is deferred until the next character arrives (see
// putRune's wrapPending flag), matching real terminal behaviour.
func (t *Term) Cursor() (row, col int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.row, t.col
}

// CursorVisible reports whether the cursor is shown (ESC[?25h / ESC[?25l).
func (t *Term) CursorVisible() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return !t.cursorHidden
}

// Snapshot returns the whole screen as text: rows joined by "\n", with trailing
// blanks on each row and trailing blank rows removed. Colour is deliberately
// excluded — assert colour through Cell.
func (t *Term) Snapshot() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	rows := make([]string, t.height)
	last := -1
	for i := 1; i <= t.height; i++ {
		rows[i-1] = t.rowLocked(i)
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
// in the order they arrived, plus a trailing entry when the stream ended
// mid-sequence (bytes are still sitting in the pending buffer, waiting for a
// Write that never came). A test that wants to be sure the double kept up
// with the code under test asserts this is empty.
func (t *Term) Unhandled() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]string, len(t.unhandled))
	copy(out, t.unhandled)
	if len(t.pending) > 0 {
		out = append(out, fmt.Sprintf("pending: %q", string(t.pending)))
	}
	return out
}

// putRune writes r at the cursor using the current pen and advances the cursor,
// wrapping at the right margin and scrolling at the bottom of the screen.
//
// Wrapping is deferred: a write that lands exactly on the last column sets
// wrapPending and leaves col at width rather than stepping it past the right
// margin. The next putRune call consumes that flag first, moving to column 1
// of the next row before it writes. That mirrors real terminal behaviour,
// where the cursor visibly sits on the last column — not one past it — until
// something is actually written there.
func (t *Term) putRune(r rune) {
	if t.wrapPending {
		t.wrapPending = false
		t.col = 1
		t.row++
	}
	// row > height, not >=: a bare '\n' (applyControl) advances the row itself
	// and must scroll as soon as it reaches the last row, but putRune only
	// wrapped col back to 1 above and needs the increment to have already
	// landed one row past the bottom before it scrolls.
	if t.row > t.height {
		t.scrollUp()
		t.row = t.height
	}
	if t.row >= 1 && t.row <= t.height && t.col >= 1 && t.col <= t.width {
		t.cells[t.row-1][t.col-1] = Cell{Rune: r, Fg: t.pen.Fg, Bg: t.pen.Bg, Bold: t.pen.Bold}
	}
	if t.col >= t.width {
		t.wrapPending = true
	} else {
		t.col++
	}
}

// scrollUp discards the top row and appends a blank row at the bottom. The new
// row is filled with default colours rather than the current pen's background;
// a real terminal paints it in the pen's background, but this Term deliberately
// does not model that.
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
