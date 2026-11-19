package ansi

import (
	"regexp"
	"strconv"
)

var cprPattern = regexp.MustCompile(`\x1b\[(\d+);(\d+)R`)

// ParseCPR finds a cursor position report (ESC[row;colR) in data, ignoring
// any bytes around it. A row or column that is zero or does not fit an int
// makes the report invalid.
func ParseCPR(data []byte) (row, col int, ok bool) {
	m := cprPattern.FindSubmatch(data)
	if m == nil {
		return 0, 0, false
	}
	row, err1 := strconv.Atoi(string(m[1]))
	col, err2 := strconv.Atoi(string(m[2]))
	if err1 != nil || err2 != nil || row < 1 || col < 1 {
		return 0, 0, false
	}
	return row, col, true
}

// EncodingProbeRow is the row EncodingProbe draws on. It is not row 1, so a
// report from it cannot be mistaken for a key of the same shape: xterm sends
// Shift-F3 as CSI 1 ; 2 R.
const EncodingProbeRow = 2

// EncodingProbe saves the cursor, moves to column 1 of EncodingProbeRow,
// draws U+2302 (⌂) as its three UTF-8 bytes, and asks where the cursor
// ended up. A terminal that decodes UTF-8 moves one column; a single-byte
// terminal such as a CP437 client moves three. The glyph's East Asian width
// is Neutral, so no UTF-8 terminal draws it double-width, and every CP437
// font has it.
const EncodingProbe = "\x1b7\x1b[2;1H⌂\x1b[6n"

// EncodingProbeClear erases the row EncodingProbe drew on, with anything a
// terminal printed there for queries it did not understand, and restores
// the cursor.
const EncodingProbeClear = "\x1b[2;1H\x1b[K\x1b8"

// ProbeLineClear erases the current line, for a probe sent without
// EncodingProbe.
const ProbeLineClear = "\r\x1b[K"

// EncodingFromProbeColumn maps the column reported after EncodingProbe to an
// output mode: one column of movement (column 2) is UTF-8, two or three is
// CP437. OutputModeAuto means the answer settles nothing.
func EncodingFromProbeColumn(col int) OutputMode {
	switch col {
	case 2:
		return OutputModeUTF8
	case 3, 4:
		return OutputModeCP437
	}
	return OutputModeAuto
}
