package testterm

import (
	"fmt"
	"strings"
)

// applySGR updates the pen from a Select Graphic Rendition sequence. Only the
// attributes the BBS emits are modelled: reset, bold on/off, blink on/off (BBS
// art uses blink for its bright backgrounds), the eight standard and eight
// bright colours, and the default-colour codes. Any other parameter (reverse,
// 256-colour, ...) is recorded in Unhandled instead of being silently dropped;
// a 256-colour or truecolour sequence like ESC[48;5;12m is recorded whole, so
// the 5 in it is not taken for blink.
func (t *Term) applySGR(params []int) {
	if len(params) == 0 {
		t.pen = blankCell()
		return
	}
	for i := 0; i < len(params); i++ {
		p := params[i]
		if p == 38 || p == 48 {
			// Extended colour: 38;5;n or 38;2;r;g;b. Take the whole form.
			n := 1
			if i+1 < len(params) {
				switch params[i+1] {
				case 5:
					n = 3
				case 2:
					n = 5
				}
			}
			end := min(i+n, len(params))
			parts := make([]string, 0, end-i)
			for _, q := range params[i:end] {
				parts = append(parts, fmt.Sprint(q))
			}
			t.unhandled = append(t.unhandled, "SGR "+strings.Join(parts, ";"))
			i = end - 1
			continue
		}
		switch {
		case p <= 0: // 0 or omitted: reset
			t.pen = blankCell()
		case p == 1:
			t.pen.Bold = true
		case p == 22:
			t.pen.Bold = false
		case p == 5:
			t.pen.Blink = true
		case p == 25:
			t.pen.Blink = false
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
