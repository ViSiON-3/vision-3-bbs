package testterm

import "fmt"

// applySGR updates the pen from a Select Graphic Rendition sequence. Only the
// attributes internal/editor emits are modelled: reset, bold on/off, the eight
// standard and eight bright colours, and the default-colour codes. Any other
// parameter (blink, reverse, 256-colour, ...) is recorded in Unhandled instead
// of being silently dropped; a 256-colour sequence like ESC[48;5;12m records
// each of its three parameters separately rather than as one entry.
func (t *Term) applySGR(params []int) {
	if len(params) == 0 {
		t.pen = blankCell()
		return
	}
	for _, p := range params {
		switch {
		case p <= 0: // 0 or omitted: reset
			t.pen = blankCell()
		case p == 1:
			t.pen.Bold = true
		case p == 22:
			t.pen.Bold = false
		case p >= 30 && p <= 37, p >= 90 && p <= 97:
			t.pen.Fg = p
		case p == 39:
			t.pen.Fg = -1
		case p >= 40 && p <= 47, p >= 100 && p <= 107:
			t.pen.Bg = p
		case p == 49:
			t.pen.Bg = -1
		default:
			t.unhandled = append(t.unhandled, fmt.Sprintf("SGR %d", p))
		}
	}
}
