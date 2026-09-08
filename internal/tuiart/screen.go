package tuiart

import "strings"

// Screen accumulates full-width terminal rows for one editor screen, tracking
// the row it is about to write so background fill can be sourced from the
// backdrop at the matching offset.
//
// It exists because every editor screen has the same shape — a header row, top
// background padding, a centered box, bottom padding, a help bar — and each one
// that hand-rolled that shape from its own constants got it wrong in a
// different way. Deriving the padding from a single declared fixed-row count
// (see Split) is what keeps a screen exactly as tall as its terminal.
//
// internal/configeditor has an equivalent scaffold of its own, listBox, which
// predates this type and additionally owns that editor's box styling. It was
// left alone deliberately: migrating it would change no pixels but would put
// its byte-identical golden captures at risk for no gain.
type Screen struct {
	b      strings.Builder
	width  int
	bd     *Backdrop
	rowIdx int
}

// NewScreen returns a Screen that writes width-column rows against bd.
// A nil bd is valid and renders the legacy shaded fill.
func NewScreen(width int, bd *Backdrop) *Screen {
	return &Screen{width: width, bd: bd}
}

// Split returns the top and bottom background padding that vertically centers
// fixedRows of content in a terminal of the given height.
//
// Neither value is floored at a minimum. A floor turns a miscounted fixedRows
// into content that overflows the terminal and scrolls it, which is far harder
// to see than the blank row a plain subtraction leaves behind.
func Split(height, fixedRows int) (top, bottom int) {
	extra := height - fixedRows
	if extra < 0 {
		extra = 0
	}
	top = extra / 2
	return top, extra - top
}

// Row reports the screen row that the next Line will occupy.
func (s *Screen) Row() int { return s.rowIdx }

// Rows reports how many rows have been written so far.
func (s *Screen) Rows() int { return s.rowIdx }

// Line writes one full-width row and advances to the next.
func (s *Screen) Line(content string) {
	s.b.WriteString(content)
	s.b.WriteByte('\n')
	s.rowIdx++
}

// BgLine writes one full-width row of backdrop.
func (s *Screen) BgLine() { s.Line(s.bd.Segment(s.rowIdx, 0, s.width)) }

// BgRows writes n full-width rows of backdrop.
func (s *Screen) BgRows(n int) {
	for i := 0; i < n; i++ {
		s.BgLine()
	}
}

// Pad surrounds box content with backdrop on both sides, sourced from the row
// Line is about to write. Call it as s.Line(s.Pad(padL, padR, content)) so the
// backdrop offset matches the row the content lands on.
func (s *Screen) Pad(padL, padR int, content string) string {
	if padL < 0 {
		padL = 0
	}
	if padR < 0 {
		padR = 0
	}
	return s.bd.Segment(s.rowIdx, 0, padL) + content +
		s.bd.Segment(s.rowIdx, s.width-padR, padR)
}

// Last writes the final row without a trailing newline, which is what keeps a
// screen exactly height rows tall rather than height plus an empty one.
func (s *Screen) Last(content string) {
	s.b.WriteString(content)
	s.rowIdx++
}

// String returns the assembled screen.
func (s *Screen) String() string { return s.b.String() }
