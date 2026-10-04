// Package vt keeps a text picture of a BBS screen from the ANSI stream that draws it.
// This is forked from the v3agents terminal MCP's virtual-screen parser.
package vt

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"
)

// Encoding selects how bytes from the board become terminal characters.
type Encoding int

const (
	// CP437 decodes bytes with the IBM code page used by classic BBSes.
	CP437 Encoding = iota
	// UTF8 decodes the screen as UTF-8.
	UTF8
)

// EncodingFor maps the profile's encoding name to its screen decoder.
func EncodingFor(name string) Encoding {
	if name == "utf8" {
		return UTF8
	}
	return CP437
}

// Snapshot is the rendered screen and current zero-based cursor position.
type Snapshot struct {
	Text     string
	Row, Col int
}

type cell struct {
	r  rune
	hl bool
}

const (
	ground = iota
	escape
	csi
	skip1   // one byte after ESC ( ) * + - . /
	strBody // OSC/DCS/SOS/PM/APC payload, ended by BEL or ST
	strEsc  // ESC seen inside a string payload
)

const maxString = 4096

// Screen consumes ANSI output and keeps the resulting text screen.
type Screen struct {
	mu                 sync.Mutex
	cols, rows         int
	cells              [][]cell
	row, col           int
	savedRow, savedCol int
	top, bottom        int
	reverse            bool
	bg                 int // 0 means default or black
	wrapPending        bool
	enc                Encoding
	reply              func([]byte)
	state              int
	params             []byte
	pending            []byte // incomplete UTF-8 rune
	strLen             int
}

// New creates a terminal screen with the requested geometry and encoding.
func New(cols, rows int, enc Encoding, reply func([]byte)) *Screen {
	s := &Screen{cols: cols, rows: rows, enc: enc, reply: reply, bottom: rows - 1}
	s.cells = make([][]cell, rows)
	for i := range s.cells {
		s.cells[i] = blankRow(cols)
	}
	return s
}

func (s *Screen) reset() {
	for i := range s.cells {
		s.cells[i] = blankRow(s.cols)
	}
	s.row, s.col, s.savedRow, s.savedCol = 0, 0, 0, 0
	s.top, s.bottom = 0, s.rows-1
	s.reverse, s.bg, s.wrapPending = false, 0, false
}

func blankRow(cols int) []cell {
	r := make([]cell, cols)
	for i := range r {
		r[i] = cell{r: ' '}
	}
	return r
}

// Write applies terminal bytes to the screen and implements io.Writer.
func (s *Screen) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, b := range p {
		s.feed(b)
	}
	return len(p), nil
}

func (s *Screen) feed(b byte) {
	switch s.state {
	case escape:
		s.escapeByte(b)
		return
	case skip1:
		s.state = ground
		return
	case strBody:
		switch b {
		case 0x07:
			s.state = ground
		case 0x1b:
			s.state = strEsc
		default:
			if s.strLen++; s.strLen >= maxString {
				s.state = ground
			}
		}
		return
	case strEsc:
		switch b {
		case '\\':
			s.state = ground
		case 0x1b:
			// stay
		default:
			s.state = escape
			s.escapeByte(b)
		}
		return
	case csi:
		if b >= 0x40 && b <= 0x7e {
			s.state = ground
			s.dispatch(b, string(s.params))
			s.params = s.params[:0]
		} else if len(s.params) < 64 {
			s.params = append(s.params, b)
		}
		return
	}
	switch b {
	case 0x1b:
		s.pending = s.pending[:0]
		s.state = escape
	case '\r':
		s.col, s.wrapPending = 0, false
	case '\n', 0x0b:
		s.lineFeed()
	case 0x08:
		if s.col > 0 {
			s.col--
		}
		s.wrapPending = false
	case '\t':
		s.col = min((s.col/8+1)*8, s.cols-1)
	case 0x0c:
		s.eraseDisplay(2)
	case 0x07, 0x00:
	default:
		if b < 0x20 {
			return
		}
		s.decode(b)
	}
}

func (s *Screen) decode(b byte) {
	if s.enc == CP437 {
		if b < 0x80 {
			s.put(rune(b))
		} else {
			s.put(charmap.CodePage437.DecodeByte(b))
		}
		return
	}
	s.pending = append(s.pending, b)
	if !utf8.FullRune(s.pending) {
		if len(s.pending) >= utf8.UTFMax {
			s.pending = s.pending[:0]
			s.put(utf8.RuneError)
		}
		return
	}
	r, size := utf8.DecodeRune(s.pending)
	if r == utf8.RuneError && size == 1 {
		rest := append([]byte(nil), s.pending[1:]...)
		s.pending = s.pending[:0]
		s.put(utf8.RuneError)
		for _, c := range rest {
			s.feed(c)
		}
		return
	}
	s.pending = s.pending[:0]
	s.put(r)
}

func (s *Screen) escapeByte(b byte) {
	s.state = ground
	switch b {
	case '[':
		s.state = csi
		s.params = s.params[:0]
	case '7':
		s.savedRow, s.savedCol = s.row, s.col
	case '8':
		s.row, s.col = s.savedRow, s.savedCol
	case 'D':
		s.lineFeed()
	case 'E':
		s.col = 0
		s.lineFeed()
	case 'M':
		if s.row == s.top {
			s.scrollDown()
		} else if s.row > 0 {
			s.row--
		}
	case 'c':
		s.reset()
	case '(', ')', '*', '+', '-', '.', '/':
		s.state = skip1
	case ']', 'P', 'X', '^', '_':
		s.state = strBody
		s.strLen = 0
	}
}

func (s *Screen) put(r rune) {
	if s.wrapPending {
		s.col = 0
		s.lineFeed()
	}
	s.cells[s.row][s.col] = cell{r: r, hl: s.reverse || s.bg != 0}
	if s.col == s.cols-1 {
		s.wrapPending = true
	} else {
		s.col++
	}
}

func (s *Screen) lineFeed() {
	s.wrapPending = false
	if s.row == s.bottom {
		s.scrollUp()
	} else if s.row < s.rows-1 {
		s.row++
	}
}

func (s *Screen) scrollUp() {
	copy(s.cells[s.top:s.bottom], s.cells[s.top+1:s.bottom+1])
	s.cells[s.bottom] = blankRow(s.cols)
}

func (s *Screen) scrollDown() {
	copy(s.cells[s.top+1:s.bottom+1], s.cells[s.top:s.bottom])
	s.cells[s.top] = blankRow(s.cols)
}

func numbers(params string) []int {
	params = strings.TrimPrefix(params, "?")
	if params == "" {
		return nil
	}
	parts := strings.Split(params, ";")
	out := make([]int, len(parts))
	for i, p := range parts {
		out[i], _ = strconv.Atoi(p)
	}
	return out
}

func arg(n []int, i, def int) int {
	if i < len(n) && n[i] > 0 {
		return n[i]
	}
	return def
}

func clamp(v, lo, hi int) int { return max(lo, min(v, hi)) }

func (s *Screen) dispatch(final byte, params string) {
	n := numbers(params)
	switch final {
	case 'A', 'B', 'C', 'D', 'H', 'f', 'G', 'd', 's', 'u', 'r':
		s.wrapPending = false
	}
	switch final {
	case 'A':
		s.row = clamp(s.row-arg(n, 0, 1), 0, s.rows-1)
	case 'B':
		s.row = clamp(s.row+arg(n, 0, 1), 0, s.rows-1)
	case 'C':
		s.col = clamp(s.col+arg(n, 0, 1), 0, s.cols-1)
	case 'D':
		s.col = clamp(s.col-arg(n, 0, 1), 0, s.cols-1)
	case 'H', 'f':
		s.row = clamp(arg(n, 0, 1)-1, 0, s.rows-1)
		s.col = clamp(arg(n, 1, 1)-1, 0, s.cols-1)
	case 'G':
		s.col = clamp(arg(n, 0, 1)-1, 0, s.cols-1)
	case 'd':
		s.row = clamp(arg(n, 0, 1)-1, 0, s.rows-1)
	case 'J':
		s.eraseDisplay(arg(n, 0, 0))
	case 'K':
		s.eraseLine(arg(n, 0, 0))
	case 'L':
		for i := 0; i < arg(n, 0, 1) && s.row >= s.top && s.row <= s.bottom; i++ {
			copy(s.cells[s.row+1:s.bottom+1], s.cells[s.row:s.bottom])
			s.cells[s.row] = blankRow(s.cols)
		}
	case 'M':
		for i := 0; i < arg(n, 0, 1) && s.row >= s.top && s.row <= s.bottom; i++ {
			copy(s.cells[s.row:s.bottom], s.cells[s.row+1:s.bottom+1])
			s.cells[s.bottom] = blankRow(s.cols)
		}
	case 'm':
		s.sgr(params)
	case 's':
		s.savedRow, s.savedCol = s.row, s.col
	case 'u':
		s.row, s.col = s.savedRow, s.savedCol
	case 'r':
		top, bottom := arg(n, 0, 1)-1, arg(n, 1, s.rows)-1
		if top < bottom && bottom < s.rows {
			s.top, s.bottom = top, bottom
		} else {
			s.top, s.bottom = 0, s.rows-1
		}
		s.row, s.col = 0, 0
	case 'n':
		if arg(n, 0, 0) == 6 && s.reply != nil {
			s.reply([]byte(fmt.Sprintf("\x1b[%d;%dR", s.row+1, s.col+1)))
		}
	}
}

func (s *Screen) sgr(params string) {
	if params == "" {
		params = "0"
	}
	parts := strings.Split(params, ";")
	for i := 0; i < len(parts); i++ {
		if strings.Contains(parts[i], ":") {
			s.sgrColon(parts[i])
			continue
		}
		v, _ := strconv.Atoi(parts[i])
		if v == 38 || v == 48 {
			black := false
			mode := 0
			if i+1 < len(parts) {
				mode, _ = strconv.Atoi(parts[i+1])
			}
			switch mode {
			case 5:
				if i+2 < len(parts) {
					n, _ := strconv.Atoi(parts[i+2])
					black = n == 0
				}
				i += 2
			case 2:
				black = true
				for k := 2; k <= 4; k++ {
					if i+k < len(parts) {
						if c, _ := strconv.Atoi(parts[i+k]); c != 0 {
							black = false
						}
					}
				}
				i += 4
			default:
				i++
			}
			s.extColor(v, black)
			continue
		}
		s.sgrCode(v)
	}
}

// sgrColon handles colon-separated sub-parameters such as 38:5:n and 48:2::r:g:b.
func (s *Screen) sgrColon(part string) {
	sub := strings.Split(part, ":")
	v, _ := strconv.Atoi(sub[0])
	if v != 38 && v != 48 || len(sub) < 3 {
		return
	}
	mode, _ := strconv.Atoi(sub[1])
	switch mode {
	case 5:
		n, _ := strconv.Atoi(sub[2])
		s.extColor(v, n == 0)
	case 2:
		black := true
		for _, c := range sub[max(2, len(sub)-3):] {
			if n, _ := strconv.Atoi(c); n != 0 {
				black = false
			}
		}
		s.extColor(v, black)
	}
}

func (s *Screen) extColor(code int, black bool) {
	if code != 48 {
		return
	}
	if black {
		s.bg = 0
	} else {
		s.bg = 16
	}
}

func (s *Screen) sgrCode(v int) {
	switch {
	case v == 0:
		s.reverse, s.bg = false, 0
	case v == 7:
		s.reverse = true
	case v == 27:
		s.reverse = false
	case v >= 40 && v <= 47:
		s.bg = v - 40
	case v == 49:
		s.bg = 0
	case v >= 100 && v <= 107:
		s.bg = v - 100 + 8
	}
}

func (s *Screen) eraseLine(mode int) {
	from, to := s.col, s.cols
	switch mode {
	case 1:
		from, to = 0, s.col+1
	case 2:
		from = 0
	}
	for i := from; i < to; i++ {
		s.cells[s.row][i] = cell{r: ' '}
	}
}

func (s *Screen) eraseDisplay(mode int) {
	switch mode {
	case 0:
		s.eraseLine(0)
		for r := s.row + 1; r < s.rows; r++ {
			s.cells[r] = blankRow(s.cols)
		}
	case 1:
		s.eraseLine(1)
		for r := 0; r < s.row; r++ {
			s.cells[r] = blankRow(s.cols)
		}
	default:
		for r := range s.cells {
			s.cells[r] = blankRow(s.cols)
		}
		s.wrapPending = false
		// ANSI.SYS homes the cursor on a full clear, and BBS screens rely on it.
		s.row, s.col = 0, 0
	}
}

// Snapshot renders the screen as text. Highlighted runs are wrapped in «»
// unless more than half of the visible text is highlighted, which means the
// board painted the whole screen rather than marking a selection.
// Snapshot returns the visible rows and current cursor position.
func (s *Screen) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	visible, lit := 0, 0
	for _, row := range s.cells {
		for _, c := range row {
			if c.r != ' ' {
				visible++
				if c.hl {
					lit++
				}
			}
		}
	}
	mark := lit > 0 && lit*2 <= visible
	out := make([]string, s.rows)
	for i, row := range s.cells {
		var b strings.Builder
		for j := 0; j < len(row); {
			if !mark || !row[j].hl {
				b.WriteRune(row[j].r)
				j++
				continue
			}
			k := j
			for k < len(row) && row[k].hl {
				k++
			}
			var run strings.Builder
			for _, c := range row[j:k] {
				run.WriteRune(c.r)
			}
			text := run.String()
			trimmed := strings.TrimRight(text, " ")
			if trimmed == "" {
				b.WriteString(text)
			} else {
				b.WriteString("«" + trimmed + "»" + text[len(trimmed):])
			}
			j = k
		}
		out[i] = strings.TrimRight(b.String(), " ")
	}
	return Snapshot{Text: strings.Join(out, "\n"), Row: s.row, Col: s.col}
}
