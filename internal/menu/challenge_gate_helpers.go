package menu

import (
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
)

// parseChallengeKey maps a config key string to the key code the challenge
// loop compares against. "ESC" (any case) or empty -> editor.KeyEsc; otherwise
// the first rune of the string.
func parseChallengeKey(s string) int {
	if s == "" || strings.EqualFold(s, "ESC") {
		return editor.KeyEsc
	}
	for _, r := range s { // first rune
		return int(r)
	}
	return editor.KeyEsc
}

// findCountdownField scans prompt bytes as a terminal would, tracking the
// visual cursor position, and returns the 1-based (row, col) and width of the
// first run of '#' characters. ANSI CSI (ESC[...) and SS3 (ESC O x) escape
// sequences are skipped without advancing the column, so color codes preceding
// the field do not offset it. \r resets the column, \n advances the row.
// found is false if there is no '#' run.
//
// Limitation: cursor-movement escape sequences before the field are treated as
// zero-width (like SGR); prompt art for the gate is expected to use plain text
// plus color codes, matching botgate's own assumptions.
func findCountdownField(prompt []byte) (row, col, width int, found bool) {
	row, col = 1, 1
	for i := 0; i < len(prompt); i++ {
		b := prompt[i]
		switch {
		case b == 0x1B: // ESC — skip an escape sequence
			i += escapeSeqLen(prompt[i:]) - 1 // -1: loop's i++ advances past the last byte
		case b == '\r':
			col = 1
		case b == '\n':
			row++
		case b == '#':
			w := 0
			for i+w < len(prompt) && prompt[i+w] == '#' {
				w++
			}
			return row, col, w, true
		default:
			col++
		}
	}
	return 0, 0, 0, false
}

// escapeSeqLen returns the byte length of the escape sequence at the start of b
// (which begins with ESC). Handles CSI (ESC [ ... final 0x40-0x7E), SS3
// (ESC O x), and a lone/unknown ESC (length 1).
func escapeSeqLen(b []byte) int {
	if len(b) < 2 {
		return 1
	}
	switch b[1] {
	case '[': // CSI: params/intermediates until a final byte 0x40-0x7E
		n := 2
		for n < len(b) {
			c := b[n]
			n++
			if c >= 0x40 && c <= 0x7E {
				break
			}
		}
		return n
	case 'O': // SS3: ESC O <one byte>
		if len(b) >= 3 {
			return 3
		}
		return 2
	default:
		return 1
	}
}
