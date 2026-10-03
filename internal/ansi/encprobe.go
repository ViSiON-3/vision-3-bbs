package ansi

import (
	"regexp"
	"strconv"
)

var cprPattern = regexp.MustCompile(`\x1b\[(\d+);(\d+)R`)

// ParseCPR finds a cursor position report (ESC[row;colR) in data, ignoring
// any bytes around it.
func ParseCPR(data []byte) (row, col int, ok bool) {
	m := cprPattern.FindSubmatch(data)
	if m == nil {
		return 0, 0, false
	}
	row, _ = strconv.Atoi(string(m[1])) // regex guarantees digits
	col, _ = strconv.Atoi(string(m[2]))
	return row, col, true
}

// EncodingProbe returns the cursor to column 1, draws a box-drawing glyph as
// its three UTF-8 bytes, and asks where the cursor ended up. A terminal that
// decodes UTF-8 advances one cell (two if it draws the glyph double-width);
// a single-byte terminal such as a CP437 client advances three.
const EncodingProbe = "\r│\x1b[6n"

// EncodingProbeClear erases what EncodingProbe drew.
const EncodingProbeClear = "\r\x1b[K"

// EncodingFromProbeColumn maps the column reported after EncodingProbe to an
// output mode. OutputModeAuto means the answer settles nothing.
func EncodingFromProbeColumn(col int) OutputMode {
	switch col {
	case 2, 3:
		return OutputModeUTF8
	case 4:
		return OutputModeCP437
	}
	return OutputModeAuto
}
