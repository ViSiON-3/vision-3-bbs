package testterm

// applyControl handles the C0 control characters the editor emits.
func (t *Term) applyControl(b byte) {
	switch b {
	case '\r':
		t.col = 1
		t.wrapPending = false
	case '\n':
		// A '\n' arriving with a wrap pending consumes it: clear the flag and
		// reset the column, so the row advances once rather than twice.
		//
		// Deliberate divergence: a real terminal clears the pending-wrap flag
		// but leaves the column where it was, so text after a full row plus a
		// bare '\n' would resume under the right margin. Column 1 is what a
		// test author means by "\n", and the editor always emits "\r\n", where
		// the '\r' makes the two models identical. Without a pending wrap,
		// '\n' leaves the column untouched, as a real terminal does.
		if t.wrapPending {
			t.wrapPending = false
			t.col = 1
		}
		// row >= height, not >: applyControl advances the row itself, unlike
		// putRune, which only wraps col back to 1 and lets row run one past
		// height before scrolling. A bare '\n' must scroll as soon as the
		// cursor is already on the last row, not after stepping past it.
		if t.row >= t.height {
			t.scrollUp()
			t.row = t.height
		} else {
			t.row++
		}
	case '\b':
		if t.col > 1 {
			t.col--
		}
		t.wrapPending = false
	case '\t':
		next := ((t.col-1)/8+1)*8 + 1
		if next > t.width {
			next = t.width
		}
		t.col = next
		t.wrapPending = false
	default:
		t.unhandled = append(t.unhandled, string([]byte{b}))
	}
}

// eraseLine clears part of the cursor's row: 0 = cursor to end, 1 = start to
// cursor, 2 = the whole row. Erased cells take the current pen's colours,
// which is how a coloured background survives a clear on a real terminal. It
// reports whether mode was recognised; the caller records the sequence in
// Unhandled when it was not.
func (t *Term) eraseLine(mode int) bool {
	if t.row < 1 || t.row > t.height {
		return true
	}
	from, to := 1, t.width
	switch mode {
	case 0:
		from = t.col
	case 1:
		to = t.col
	case 2:
	default:
		return false
	}
	for c := from; c <= to; c++ {
		// t.col cannot exceed t.width — putRune's deferred wrap holds it at
		// width rather than stepping past the margin — so this guard should
		// never trigger in practice. It stays as cheap defensive bounds
		// checking rather than something load-bearing.
		if c >= 1 && c <= t.width {
			t.cells[t.row-1][c-1] = Cell{Rune: ' ', Fg: t.pen.Fg, Bg: t.pen.Bg, Bold: t.pen.Bold}
		}
	}
	return true
}

// eraseDisplay clears part of the screen: 0 = cursor to end, 1 = start to
// cursor, 2 = everything. It reports whether mode was recognised; the caller
// records the sequence in Unhandled when it was not.
func (t *Term) eraseDisplay(mode int) bool {
	switch mode {
	case 0:
		t.eraseLine(0)
		t.eraseRows(t.row+1, t.height)
	case 1:
		t.eraseLine(1)
		t.eraseRows(1, t.row-1)
	case 2:
		t.eraseRows(1, t.height)
	default:
		return false
	}
	return true
}

// eraseRows blanks every cell in rows from..to (inclusive, 1-based) using the
// current pen's colours. Rows outside the screen are skipped, so callers do
// not need to clamp their range before calling it.
func (t *Term) eraseRows(from, to int) {
	for r := from; r <= to; r++ {
		if r < 1 || r > t.height {
			continue
		}
		for c := range t.cells[r-1] {
			t.cells[r-1][c] = Cell{Rune: ' ', Fg: t.pen.Fg, Bg: t.pen.Bg, Bold: t.pen.Bold}
		}
	}
}
