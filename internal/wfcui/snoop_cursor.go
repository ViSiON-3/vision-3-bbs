package wfcui

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"
)

// cond measures runes the same way whatever the locale: ambiguous-width
// characters take one cell.
var cond = func() *runewidth.Condition {
	c := runewidth.NewCondition()
	c.EastAsianWidth = false
	return c
}()

// sgrState is the caller's current text attributes.
type sgrState struct {
	bits   uint32 // bit n set while SGR attribute n is on
	fg, bg string // colour parameters, "" for the default
}

// seq returns a sequence that sets exactly these attributes.
func (s sgrState) seq() string {
	var b strings.Builder
	b.WriteString("\x1b[0")
	for n := uint(1); n < 30; n++ {
		if s.bits&(1<<n) != 0 {
			b.WriteString(";" + strconv.Itoa(int(n)))
		}
	}
	if s.fg != "" {
		b.WriteString(";" + s.fg)
	}
	if s.bg != "" {
		b.WriteString(";" + s.bg)
	}
	b.WriteString("m")
	return b.String()
}

// apply folds the parameters of an SGR sequence into the state.
func (s *sgrState) apply(params []string) {
	if len(params) == 0 {
		*s = sgrState{}
		return
	}
	for i := 0; i < len(params); i++ {
		p := params[i]
		if strings.Contains(p, ":") {
			// Colon form carries its sub-parameters in one item.
			switch {
			case strings.HasPrefix(p, "38:"):
				s.fg = p
			case strings.HasPrefix(p, "48:"):
				s.bg = p
			}
			continue
		}
		n, err := strconv.Atoi(p)
		if p == "" {
			n, err = 0, nil
		}
		if err != nil {
			continue
		}
		switch {
		case n == 0:
			*s = sgrState{}
		case n >= 1 && n <= 9, n == 21:
			s.bits |= 1 << uint(n)
		case n == 22:
			s.bits &^= 1<<1 | 1<<2
		case n == 23:
			s.bits &^= 1 << 3
		case n == 24:
			s.bits &^= 1<<4 | 1<<21
		case n == 25:
			s.bits &^= 1<<5 | 1<<6
		case n >= 27 && n <= 29:
			s.bits &^= 1 << uint(n-20)
		case n >= 30 && n <= 37, n >= 90 && n <= 97:
			s.fg = p
		case n == 39:
			s.fg = ""
		case n >= 40 && n <= 47, n >= 100 && n <= 107:
			s.bg = p
		case n == 49:
			s.bg = ""
		case n == 38 || n == 48:
			rest := 0
			if i+1 < len(params) {
				switch params[i+1] {
				case "5":
					rest = 2
				case "2":
					rest = 4
				}
			}
			if rest == 0 || i+rest >= len(params) {
				i = len(params)
				continue
			}
			v := strings.Join(params[i:i+rest+1], ";")
			if n == 38 {
				s.fg = v
			} else {
				s.bg = v
			}
			i += rest
		}
	}
}

// cursor follows where the caller's output leaves its cursor on a screen of
// w by h cells, using the sysop terminal's (xterm) rules. known is false when
// the output did something the tracker does not model.
type cursor struct {
	w, h        int
	row, col    int // 1-based
	known       bool
	pendingWrap bool // the last glyph filled the final column
	wrapOff     bool // autowrap is off
	origin      bool // origin mode is on
	top, bot    int  // scroll region
	regionDirty bool // the caller changed the scroll region
	sgr         sgrState

	saved struct {
		row, col    int
		pendingWrap bool
		sgr         sgrState
		ok          bool
	}

	rbuf  []byte // bytes of the UTF-8 rune being read
	rneed int
}

// cutRune ends a rune that an escape sequence interrupted. It draws as one
// replacement glyph.
func (c *cursor) cutRune() {
	if c.rneed > 0 {
		c.rbuf, c.rneed = c.rbuf[:0], 0
		if c.known {
			c.print(1)
		}
	}
}

func (c *cursor) reset(w, h int) {
	*c = cursor{w: w, h: h, row: 1, col: 1, known: w > 0 && h > 0, top: 1, bot: h}
}

// canRestore reports whether the cursor can be put back with an absolute move.
func (c *cursor) canRestore() bool {
	return c.known && !c.pendingWrap && !c.origin
}

// restore returns the sequences that put the cursor and attributes back.
func (c *cursor) restore() string {
	return fmt.Sprintf("\x1b[%d;%dH", c.row, c.col) + c.sgr.seq()
}

func (c *cursor) home() {
	c.row, c.col, c.pendingWrap, c.known = 1, 1, false, !c.origin
}

func (c *cursor) lf() {
	if c.row != c.bot && c.row < c.h {
		c.row++
	}
	c.pendingWrap = false
}

func (c *cursor) print(width int) {
	if width <= 0 {
		return
	}
	if c.pendingWrap {
		c.col = 1
		c.lf()
	}
	if c.col+width-1 > c.w {
		if c.wrapOff {
			c.col = c.w - width + 1
		} else {
			c.col = 1
			c.lf()
		}
	}
	c.col += width
	if c.col > c.w {
		c.col = c.w
		c.pendingWrap = !c.wrapOff
	}
}

// ground handles a byte outside any escape sequence.
func (c *cursor) ground(b byte) {
	if c.rneed > 0 {
		if b >= 0x80 && b < 0xc0 {
			c.rbuf = append(c.rbuf, b)
			if len(c.rbuf) == c.rneed {
				r, _ := utf8.DecodeRune(c.rbuf)
				c.rbuf, c.rneed = c.rbuf[:0], 0
				if c.known {
					c.print(cond.RuneWidth(r))
				}
			}
			return
		}
		// The rune was cut short; it shows as one replacement glyph.
		c.rbuf, c.rneed = c.rbuf[:0], 0
		if c.known {
			c.print(1)
		}
	}
	switch {
	case b >= 0xc0:
		c.rbuf = append(c.rbuf[:0], b)
		switch {
		case b >= 0xf0:
			c.rneed = 4
		case b >= 0xe0:
			c.rneed = 3
		default:
			c.rneed = 2
		}
	case b >= 0x80:
		// A stray continuation byte draws as one replacement glyph.
		if c.known {
			c.print(1)
		}
	case b >= 0x20 && b != 0x7f:
		if c.known {
			c.print(1)
		}
	case !c.known:
	case b == '\r':
		c.col, c.pendingWrap = 1, false
	case b == '\n', b == '\v', b == '\f':
		c.lf()
	case b == '\b':
		if c.col > 1 {
			c.col--
		}
		c.pendingWrap = false
	case b == '\t':
		c.col = min(((c.col-1)/8+1)*8+1, c.w)
		c.pendingWrap = false
	}
}

func (c *cursor) save() {
	c.saved.row, c.saved.col, c.saved.pendingWrap, c.saved.sgr = c.row, c.col, c.pendingWrap, c.sgr
	c.saved.ok = c.known
}

func (c *cursor) unsave() {
	if !c.saved.ok {
		c.known = false
		return
	}
	c.sgr = c.saved.sgr
	c.row, c.col, c.pendingWrap = c.saved.row, c.saved.col, c.saved.pendingWrap
}

// esc handles the final byte of a two-byte escape sequence.
func (c *cursor) esc(b byte) {
	switch b {
	case '7':
		c.save()
	case '8':
		c.unsave()
	case 'D':
		c.lf()
	case 'E':
		c.col = 1
		c.lf()
	case 'M':
		if c.row != c.top && c.row > 1 {
			c.row--
		}
		c.pendingWrap = false
	case 'c':
		w, h := c.w, c.h
		c.reset(w, h)
		c.regionDirty = true
	case 'P', 'X', '^', '_':
		c.known = false
	}
}

// csi handles a complete CSI sequence. params is the text between the
// introducer and the final byte.
func (c *cursor) csi(final byte, params string, interm bool) {
	if interm {
		if final == 'p' {
			c.known = false
		}
		return
	}
	priv := byte(0)
	if params != "" && strings.IndexByte("<=>?", params[0]) >= 0 {
		priv, params = params[0], params[1:]
	}
	parts := strings.Split(params, ";")
	if params == "" {
		parts = nil
	}
	num := func(i int) int { // 0 and missing both mean 1
		if i < len(parts) {
			if v, err := strconv.Atoi(parts[i]); err == nil && v > 0 {
				return v
			}
		}
		return 1
	}
	has := func(v string) bool {
		for _, p := range parts {
			if p == v {
				return true
			}
		}
		return false
	}
	if priv != 0 {
		if priv == '?' && (final == 'h' || final == 'l') {
			if has("7") {
				c.wrapOff = final == 'l'
			}
			if has("6") {
				c.origin = final == 'h'
				c.home()
			}
		}
		return
	}
	if final == 'm' {
		c.sgr.apply(parts)
		return
	}
	switch final {
	case 'J', 'K', 'P', '@', 'X', 'S', 'T', 'n', 'c', 't', 'q', 'x', 'g', 'i', 'y':
		return
	case 'h', 'l':
		if has("20") {
			c.known = false
		}
		return
	case 'H', 'f':
		if c.origin {
			c.known = false
			return
		}
		c.row, c.col = min(num(0), c.h), min(num(1), c.w)
		c.pendingWrap, c.known = false, true
		return
	case 'r':
		c.regionDirty = true
		c.top, c.bot = num(0), c.h
		if len(parts) > 1 {
			c.bot = min(num(1), c.h)
		}
		if c.top >= c.bot {
			c.top, c.bot = 1, c.h
		}
		c.home()
		return
	case 's':
		if len(parts) == 0 {
			c.save()
		}
		return
	case 'u':
		c.unsave()
		return
	}
	if !c.known {
		return
	}
	c.pendingWrap = false
	switch final {
	case 'A':
		lim := 1
		if c.row >= c.top {
			lim = c.top
		}
		c.row = max(c.row-num(0), lim)
	case 'B':
		lim := c.h
		if c.row <= c.bot {
			lim = c.bot
		}
		c.row = min(c.row+num(0), lim)
	case 'C':
		c.col = min(c.col+num(0), c.w)
	case 'D':
		c.col = max(c.col-num(0), 1)
	case 'E':
		c.col, c.row = 1, min(c.row+num(0), c.h)
	case 'F':
		c.col, c.row = 1, max(c.row-num(0), 1)
	case 'G', '`':
		c.col = min(num(0), c.w)
	case 'd':
		c.row = min(num(0), c.h)
	case 'L', 'M':
		c.col = 1
	default:
		c.known = false
	}
}
