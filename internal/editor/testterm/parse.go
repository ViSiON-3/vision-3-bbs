package testterm

import (
	"unicode/utf8"
)

// Write feeds terminal output to the screen. It never returns an error; the
// io.Writer signature is what the code under test expects.
//
// Bytes held back mid-sequence or mid-rune are carried over to the next call,
// so a sequence split across writes is still parsed as one sequence.
func (t *Term) Write(p []byte) (int, error) {
	n := len(p)
	buf := p
	if len(t.pending) > 0 {
		buf = append(t.pending, p...)
		t.pending = nil
	}

	for len(buf) > 0 {
		b := buf[0]

		if b == 0x1b {
			seq, rest, complete := scanEscape(buf)
			if !complete {
				t.pending = append([]byte(nil), buf...)
				return n, nil
			}
			t.applyEscape(seq)
			buf = rest
			continue
		}

		if b < 0x20 {
			t.applyControl(b)
			buf = buf[1:]
			continue
		}

		r, size := t.decodeRune(buf)
		if size == 0 { // incomplete UTF-8 rune at the end of the chunk
			t.pending = append([]byte(nil), buf...)
			return n, nil
		}
		t.putRune(r)
		buf = buf[size:]
	}
	return n, nil
}

// decodeRune reads one character from the head of buf. In CP437 mode a high
// byte is one character; otherwise the stream is UTF-8. A size of 0 means the
// buffer ends mid-rune.
func (t *Term) decodeRune(buf []byte) (rune, int) {
	if t.cp437 {
		return cp437Rune(buf[0]), 1
	}
	r, size := utf8.DecodeRune(buf)
	if r == utf8.RuneError && size <= 1 && !utf8.FullRune(buf) {
		return 0, 0
	}
	return r, size
}

// scanEscape splits a CSI sequence off the head of buf. complete is false when
// buf ends before the sequence's final byte.
func scanEscape(buf []byte) (seq, rest []byte, complete bool) {
	if len(buf) < 2 {
		return nil, buf, false
	}
	if buf[1] != '[' {
		// Not CSI; consume ESC plus one byte so it cannot be printed as text.
		return buf[:2], buf[2:], true
	}
	for i := 2; i < len(buf); i++ {
		c := buf[i]
		if c >= '@' && c <= '~' {
			return buf[:i+1], buf[i+1:], true
		}
	}
	return nil, buf, false
}

// csiParams splits a CSI sequence into its numeric parameters and final byte.
// private reports a "?" prefix (as in ESC[?25l). Empty parameters are returned
// as -1 so a caller can tell "omitted" from "zero".
func csiParams(seq []byte) (params []int, final byte, private bool) {
	if len(seq) < 3 || seq[1] != '[' {
		return nil, 0, false
	}
	final = seq[len(seq)-1]
	body := seq[2 : len(seq)-1]
	if len(body) > 0 && body[0] == '?' {
		private = true
		body = body[1:]
	}
	if len(body) == 0 {
		return nil, final, private
	}
	cur, has := 0, false
	for _, c := range body {
		switch {
		case c >= '0' && c <= '9':
			cur = cur*10 + int(c-'0')
			has = true
		case c == ';':
			if has {
				params = append(params, cur)
			} else {
				params = append(params, -1)
			}
			cur, has = 0, false
		}
	}
	if has {
		params = append(params, cur)
	} else {
		params = append(params, -1)
	}
	return params, final, private
}

// param returns the i-th parameter, or def when it is missing or omitted.
func param(params []int, i, def int) int {
	if i >= len(params) || params[i] < 0 {
		return def
	}
	return params[i]
}

func (t *Term) applyEscape(seq []byte) {
	params, final, private := csiParams(seq)
	if final == 0 {
		t.unhandled = append(t.unhandled, string(seq))
		return
	}

	if private {
		if final == 'h' || final == 'l' {
			if param(params, 0, 0) == 25 {
				t.cursorHidden = final == 'l'
				return
			}
		}
		t.unhandled = append(t.unhandled, string(seq))
		return
	}

	switch final {
	case 'H', 'f':
		t.row = param(params, 0, 1)
		t.col = param(params, 1, 1)
		t.clampCursor()
	case 'A':
		t.row -= param(params, 0, 1)
		t.clampCursor()
	case 'B':
		t.row += param(params, 0, 1)
		t.clampCursor()
	case 'C':
		t.col += param(params, 0, 1)
		t.clampCursor()
	case 'D':
		t.col -= param(params, 0, 1)
		t.clampCursor()
	case 's':
		t.savedRow, t.savedCol = t.row, t.col
	case 'u':
		t.row, t.col = t.savedRow, t.savedCol
		t.clampCursor()
	case 'K':
		t.eraseLine(param(params, 0, 0))
	case 'J':
		t.eraseDisplay(param(params, 0, 0))
	default:
		t.unhandled = append(t.unhandled, string(seq))
	}
}

// clampCursor keeps the cursor inside the screen. Writing past the last column
// is handled at write time (Task 5), not here.
func (t *Term) clampCursor() {
	if t.row < 1 {
		t.row = 1
	}
	if t.row > t.height {
		t.row = t.height
	}
	if t.col < 1 {
		t.col = 1
	}
	if t.col > t.width {
		t.col = t.width
	}
}

// applyControl handles the C0 control characters the editor emits.
func (t *Term) applyControl(b byte) {
	switch b {
	case '\r':
		t.col = 1
	case '\n':
		t.row++
	case '\b':
		if t.col > 1 {
			t.col--
		}
	case '\t':
		next := ((t.col-1)/8+1)*8 + 1
		t.col = next
	default:
		t.unhandled = append(t.unhandled, string([]byte{b}))
	}
}

// eraseLine clears part of the cursor's row: 0 = cursor to end, 1 = start to
// cursor, 2 = the whole row. Erased cells take the current pen's colours, which
// is how a coloured background survives a clear on a real terminal.
func (t *Term) eraseLine(mode int) {
	if t.row < 1 || t.row > t.height {
		return
	}
	from, to := 1, t.width
	switch mode {
	case 0:
		from = t.col
	case 1:
		to = t.col
	case 2:
	default:
		return
	}
	for c := from; c <= to; c++ {
		if c >= 1 && c <= t.width {
			t.cells[t.row-1][c-1] = Cell{Rune: ' ', Fg: t.pen.Fg, Bg: t.pen.Bg}
		}
	}
}

// eraseDisplay clears part of the screen: 0 = cursor to end, 1 = start to
// cursor, 2 = everything.
func (t *Term) eraseDisplay(mode int) {
	switch mode {
	case 0:
		t.eraseLine(0)
		t.eraseRows(t.row+1, t.height)
	case 1:
		t.eraseLine(1)
		t.eraseRows(1, t.row-1)
	case 2:
		t.eraseRows(1, t.height)
	}
}

func (t *Term) eraseRows(from, to int) {
	for r := from; r <= to; r++ {
		if r < 1 || r > t.height {
			continue
		}
		for c := range t.cells[r-1] {
			t.cells[r-1][c] = Cell{Rune: ' ', Fg: t.pen.Fg, Bg: t.pen.Bg}
		}
	}
}

// cp437Rune is replaced with a real decode in Task 6.
func cp437Rune(b byte) rune { return rune(b) }
