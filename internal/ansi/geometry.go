package ansi

import (
	"bytes"
	"strconv"
	"unicode/utf8"
)

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

// ArtWidth is the column width stock ANSI art is drawn for.
const ArtWidth = 80

// FitArtToWidth prepares ANSI art for a terminal termWidth columns wide.
//
// Art drawn for 80 columns often leaves out line breaks and relies on the
// terminal autowrapping after column 80. On a wider terminal those rows run
// together and the image shears, so when termWidth exceeds ArtWidth the art is
// passed through HardWrap to make every wrap explicit. At ArtWidth or narrower
// the data is returned unchanged: an 80-column terminal already wraps in the
// right place, and a CR/LF after column 80 would add a blank line on terminals
// that wrap immediately rather than deferring the wrap.
//
// utf8Spans must match how the bytes will be written (see HardWrap): true for
// output that goes through terminalio.WriteProcessedBytes in CP437 or UTF-8
// mode, false for bytes written to the terminal raw.
func FitArtToWidth(data []byte, termWidth int, utf8Spans bool) []byte {
	if termWidth <= ArtWidth {
		return data
	}
	return HardWrap(data, ArtWidth, utf8Spans)
}

// HardWrap returns art with an explicit CR/LF inserted wherever a terminal
// width columns wide would autowrap, so it renders identically on a wider one.
//
// Wrapping is deferred, as on xterm and ANSI.SYS-compatible BBS terminals: a
// character in the last column leaves the cursor there, and the break is only
// inserted if another printable character follows before a CR, LF or cursor
// positioning sequence. Cursor-forward moves are clamped at the right margin,
// the way a width-column terminal would clamp them. Everything else passes
// through unchanged.
//
// With utf8Spans set, each run of text between escape sequences is measured
// in runes when it is valid UTF-8 and in bytes otherwise, the way the
// terminalio output writers decide a span's encoding; without it every byte is
// one cell, as when CP437 art is written raw.
func HardWrap(data []byte, width int, utf8Spans bool) []byte {
	if width <= 0 {
		width = ArtWidth
	}

	out := make([]byte, 0, len(data)+len(data)/40)
	x := 1 // column the next character lands in; width+1 means a wrap is pending
	savedX, savedValid := 1, false
	autoWrap := true // DECAWM; art can turn it off with ESC[?7l

	param := func(params []byte, idx, def int) int {
		field := 0
		val, hasVal := 0, false
		for _, b := range params {
			if b == ';' {
				if field == idx {
					break
				}
				field++
				val, hasVal = 0, false
				continue
			}
			if field == idx && b >= '0' && b <= '9' {
				val = val*10 + int(b-'0')
				hasVal = true
			}
		}
		if field == idx && hasVal && val > 0 {
			return val
		}
		return def
	}

	clampCol := func(c int) int {
		if c < 1 {
			return 1
		}
		if c > width {
			return width
		}
		return c
	}

	// settle cancels a pending wrap before a relative cursor move. A
	// width-column terminal holds the cursor on the last column while the wrap
	// is pending; a wider one has already moved it a column further, so step it
	// back to where the narrow terminal would have it.
	settle := func() {
		if x > width {
			out = append(out, "\x1b[D"...)
			x = width
		}
	}

	// printCell emits one printable cell, breaking the line first if the
	// previous character filled the last column.
	printCell := func(cell []byte) {
		if x > width {
			out = append(out, '\r', '\n')
			x = 1
		}
		out = append(out, cell...)
		if x < width || autoWrap {
			x++
			return
		}
		// Autowrap off: a width-column terminal keeps the cursor on the last
		// column, so later characters overwrite it. A wider terminal has moved
		// on a column; step it back.
		out = append(out, "\x1b[D"...)
	}

	for i := 0; i < len(data); {
		b := data[i]

		switch {
		case b == 0x1b && i+1 < len(data) && data[i+1] == '[':
			j := i + 2
			for j < len(data) && data[j] >= 0x20 && data[j] <= 0x3f {
				j++
			}
			if j >= len(data) {
				return append(out, data[i:]...) // truncated sequence
			}
			params := data[i+2 : j]
			seq := data[i : j+1]
			switch data[j] {
			case 'H', 'f': // CUP
				x = clampCol(param(params, 1, 1))
			case 'G', '`': // CHA
				x = clampCol(param(params, 0, 1))
			case 'E', 'F': // CNL / CPL
				x = 1
			case 'A', 'B': // CUU / CUD
				settle()
			case 'D': // CUB
				settle()
				x = clampCol(x - param(params, 0, 1))
			case 'C': // CUF — clamp at the right margin, as a width-column terminal would
				settle()
				from := x
				n := param(params, 0, 1)
				if from+n > width {
					n = width - from
				}
				x = from + n
				if n == 0 {
					seq = nil
				} else {
					seq = []byte("\x1b[" + strconv.Itoa(n) + "C")
				}
			case 's': // SCO save
				savedX, savedValid = x, true
			case 'u': // SCO restore; a no-op until a position has been saved
				if savedValid {
					x = savedX
				}
			case 'h', 'l': // mode set / reset — only DECAWM (?7) matters here
				if len(params) > 0 && params[0] == '?' {
					for _, f := range bytes.Split(params[1:], []byte{';'}) {
						if string(f) != "7" {
							continue
						}
						autoWrap = data[j] == 'h'
						if !autoWrap {
							settle() // a width-column terminal drops the pending wrap
						}
					}
				}
			}
			out = append(out, seq...)
			i = j + 1

		case b == 0x1b && i+1 < len(data):
			switch data[i+1] {
			case '7': // DECSC
				savedX, savedValid = x, true
			case '8': // DECRC
				if savedValid {
					x = savedX
				}
			}
			out = append(out, data[i:i+2]...)
			i += 2

		case b == '\r' || b == '\n': // LF is written as CRLF by the terminal layer
			x = 1
			out = append(out, b)
			i++

		case b == '\b':
			settle()
			x = clampCol(x - 1)
			out = append(out, b)
			i++

		case b == '\t':
			settle()
			if next := ((x-1)/8+1)*8 + 1; next <= width {
				x = next
				out = append(out, b)
			} else { // a wider terminal has tab stops past the margin
				x = width
				out = append(out, "\x1b["+strconv.Itoa(width)+"G"...)
			}
			i++

		case b < 0x20 || b == 0x7f: // other control characters do not move the cursor
			out = append(out, b)
			i++

		default:
			// Measure the text run up to the next escape or control character.
			end := i
			for end < len(data) && data[end] >= 0x20 && data[end] != 0x7f {
				end++
			}
			run := data[i:end]
			if utf8Spans && !isASCII(run) && utf8.Valid(run) {
				for k := 0; k < len(run); {
					_, size := utf8.DecodeRune(run[k:])
					printCell(run[k : k+size])
					k += size
				}
			} else {
				for k := range run {
					printCell(run[k : k+1])
				}
			}
			i = end
		}
	}

	return out
}

func isASCII(b []byte) bool {
	for _, c := range b {
		if c >= 0x80 {
			return false
		}
	}
	return true
}
