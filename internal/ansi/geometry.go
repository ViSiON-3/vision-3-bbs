package ansi

// ArtGeometry reports the screen geometry ANSI art occupies when written to a
// terminal `width` columns wide.
//
// rows is the lowest 1-based screen row the art actually reaches — one that
// receives a character, or that the cursor is pushed onto by the autowrap
// following a character in the final column, or by a line feed. Bare cursor
// positioning is deliberately not counted, because terminals clamp a CUP past
// the bottom row instead of scrolling to it. lastRowCols is the number of
// columns occupied on the last row that received a printable character.
//
// Art authored without CR/LF relies entirely on autowrap for its line breaks, so
// filling the last column of its last row advances the cursor one row further
// and scrolls the screen on a terminal exactly that tall. When that happens the
// whole image shifts up a row while absolute-positioned overlays (BAR lightbars)
// stay put, desynchronising the two. Callers compare rows against the session's
// negotiated height to catch it — see ArtOverflowsHeight.
func ArtGeometry(data []byte, width int) (rows, lastRowCols int) {
	if width <= 0 {
		width = 80
	}

	x, y := 1, 1
	maxRow := 1
	lastPrintedRow, lastPrintedCol := 1, 0
	savedX, savedY := 1, 1

	// param parses the CSI parameter list, substituting def for omitted values.
	param := func(params []byte, idx, def int) int {
		field := 0
		val, hasVal := 0, false
		for _, b := range params {
			if b == ';' {
				if field == idx {
					if hasVal {
						return val
					}
					return def
				}
				field++
				val, hasVal = 0, false
				continue
			}
			if b >= '0' && b <= '9' {
				val = val*10 + int(b-'0')
				hasVal = true
			}
		}
		if field == idx && hasVal {
			return val
		}
		return def
	}

	clamp := func() {
		if x < 1 {
			x = 1
		}
		if x > width {
			x = width // terminals clamp the cursor at the right margin
		}
		if y < 1 {
			y = 1
		}
	}

	// touch records a row as reached. Only output and the cursor motion that
	// scrolls a terminal — printing, autowrap, line feed — counts. Bare
	// positioning does not: terminals clamp a CUP past the bottom row rather
	// than scrolling to it, so counting it would warn about art that renders
	// perfectly well.
	touch := func() {
		if y > maxRow {
			maxRow = y
		}
	}

	for i := 0; i < len(data); i++ {
		b := data[i]

		switch {
		case b == 0x1a: // DOS EOF — SAUCE and anything past it never reaches the screen
			return maxRow, lastPrintedCol

		case b == 0x1b && i+1 < len(data) && data[i+1] == '[':
			// CSI: collect parameter/intermediate bytes up to the final byte.
			j := i + 2
			for j < len(data) && ((data[j] >= '0' && data[j] <= '9') || data[j] == ';' || data[j] == '?' || data[j] == ' ') {
				j++
			}
			if j >= len(data) {
				return maxRow, lastPrintedCol // truncated sequence
			}
			params := data[i+2 : j]
			switch data[j] {
			case 'H', 'f': // CUP
				y = param(params, 0, 1)
				x = param(params, 1, 1)
			case 'A': // CUU
				y -= param(params, 0, 1)
			case 'B': // CUD
				y += param(params, 0, 1)
			case 'C': // CUF
				x += param(params, 0, 1)
			case 'D': // CUB
				x -= param(params, 0, 1)
			case 'E': // CNL
				y += param(params, 0, 1)
				x = 1
			case 'F': // CPL
				y -= param(params, 0, 1)
				x = 1
			case 'G', '`': // CHA
				x = param(params, 0, 1)
			case 's': // SCO save
				savedX, savedY = x, y
			case 'u': // SCO restore
				x, y = savedX, savedY
			}
			clamp()
			i = j

		case b == 0x1b && i+1 < len(data) && data[i+1] == '7': // DECSC
			savedX, savedY = x, y
			i++

		case b == 0x1b && i+1 < len(data) && data[i+1] == '8': // DECRC
			x, y = savedX, savedY
			clamp()
			i++

		case b == 0x1b && i+1 < len(data): // any other two-byte escape
			i++

		case b == '\n':
			y++
			clamp()
			touch()

		case b == '\r':
			x = 1

		case b == '\b':
			x--
			clamp()

		case b == '\t': // advance to the next 8-column tab stop, never past the margin
			x = ((x-1)/8+1)*8 + 1
			clamp()

		case b < 0x20: // remaining control characters do not advance the cursor
			// no-op

		default: // printable — CP437 art is single-width, one byte per cell
			clamp()
			touch()
			if y > lastPrintedRow {
				lastPrintedRow, lastPrintedCol = y, x
			} else if x > lastPrintedCol {
				lastPrintedCol = x
			}
			x++
			if x > width { // autowrap
				x = 1
				y++
				clamp()
				touch()
			}
		}
	}

	return maxRow, lastPrintedCol
}

// ArtOverflowsHeight reports whether drawing this art on a width x height
// terminal would take the cursor past the bottom row, so the art either scrolls
// the screen or is clipped at the bottom.
//
// The answer is a lower bound: the art is measured from the home position, so
// art drawn without clearing first starts lower down and can overflow without
// being reported. That direction is deliberate — a diagnostic that cries wolf
// is one sysops learn to ignore.
func ArtOverflowsHeight(data []byte, width, height int) bool {
	if height <= 0 {
		return false // height unknown — nothing to check against
	}
	rows, _ := ArtGeometry(data, width)
	return rows > height
}
