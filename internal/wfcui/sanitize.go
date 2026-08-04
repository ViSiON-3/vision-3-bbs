package wfcui

// sanitizeTerminal strips terminal control bytes from server-supplied text
// before it is rendered. Caller handles travel from the BBS into the sysop's
// terminal; without this, a hostile handle could smuggle escape sequences
// (title changes, screen clears, query responses) onto the admin's machine.
func sanitizeTerminal(s string) string {
	var b []rune
	for i, r := range s {
		if (r < 0x20) || r == 0x7f {
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
