package configeditor

import (
	"fmt"
	"strconv"
	"strings"
)

// A field holding bytes a sysop cannot type. The value on disk holds the
// real characters, so a hand-written doors.json needs no convention beyond
// JSON's own; only the editor shows and reads them as escapes.

// showControlEscapes renders a string for editing, with every control
// character written as an escape: \r, \n, \t, \xHH for the rest, and \\ for a
// backslash so the result reads back exactly.
func showControlEscapes(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '\\':
			b.WriteString(`\\`)
		case c == '\r':
			b.WriteString(`\r`)
		case c == '\n':
			b.WriteString(`\n`)
		case c == '\t':
			b.WriteString(`\t`)
		case c < ' ' || c == 0x7F:
			fmt.Fprintf(&b, `\x%02X`, c)
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// parseControlEscapes reads a value as showControlEscapes writes it. A
// backslash before anything else is an error rather than a literal, since
// the sysop most likely mistyped an escape and would not want the backslash
// sent to a door server.
func parseControlEscapes(s string) (string, error) {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '\\' {
			b.WriteByte(c)
			continue
		}
		if i+1 >= len(s) {
			return "", fmt.Errorf(`a backslash needs a character after it (\\ for a backslash)`)
		}
		i++
		switch s[i] {
		case '\\':
			b.WriteByte('\\')
		case 'r':
			b.WriteByte('\r')
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case 'x':
			if i+2 >= len(s) {
				return "", fmt.Errorf(`\x needs two hex digits, as in \x1B`)
			}
			// ParseUint rather than Sscanf, which would stop at a non-hex
			// second byte without complaint and read "1G" as 0x01.
			v, err := strconv.ParseUint(s[i+1:i+3], 16, 8)
			if err != nil {
				return "", fmt.Errorf(`\x needs two hex digits, as in \x1B`)
			}
			b.WriteByte(byte(v))
			i += 2
		default:
			return "", fmt.Errorf(`unknown escape \%c: use \r, \n, \t, \xHH or \\`, s[i])
		}
	}
	return b.String(), nil
}
