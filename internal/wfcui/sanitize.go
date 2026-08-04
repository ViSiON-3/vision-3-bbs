package wfcui

// isTerminalControl reports whether r is a control character a terminal may
// act on: a C0 control, DEL, or a C1 control (U+0080–U+009F, which includes
// U+009B — the 8-bit form of CSI).
//
// CP437 text decodes its high bytes to printable codepoints (0x82 → é,
// 0x9B → ¢), so rejecting the C1 range never touches legitimate CP437 input.
func isTerminalControl(r rune) bool {
	return r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f)
}

// sanitizeTerminal strips terminal control characters from server-supplied
// text before it is rendered. Caller handles travel from the BBS into the
// sysop's terminal; without this, a hostile handle could smuggle escape
// sequences (title changes, screen clears, query responses) onto the admin's
// machine.
func sanitizeTerminal(s string) string {
	var b []rune
	for i, r := range s {
		if isTerminalControl(r) {
			if b == nil {
				b = []rune(s[:i])
			}
			continue
		}
		if b != nil {
			b = append(b, r)
		}
	}
	if b == nil {
		return s
	}
	return string(b)
}
