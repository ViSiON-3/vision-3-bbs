package stringeditor

import (
	"fmt"
	"strings"
)

// The editor stores BBS strings verbatim, but a single-line text input cannot
// hold a carriage return or a tab. Values are therefore shown and typed in an
// escaped form: control characters appear as backslash sequences, and a literal
// backslash is doubled so ordinary text can never turn into a control sequence
// by accident.
//
// EscapeForEdit and UnescapeFromEdit are exact inverses: unescaping an escaped
// value always yields the original bytes, so opening an entry and accepting it
// unchanged is a no-op.

// EscapeForEdit converts a stored string into its editable representation.
func EscapeForEdit(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if esc, ok := escapeRune(r); ok {
			b.WriteString(esc)
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// escapeRune returns the backslash sequence for r, or ok=false if r should be
// written literally. Only backslash and control characters are escaped; every
// printable rune, including non-ASCII text, passes through untouched.
func escapeRune(r rune) (string, bool) {
	switch r {
	case '\\':
		return `\\`, true
	case '\r':
		return `\r`, true
	case '\n':
		return `\n`, true
	case '\t':
		return `\t`, true
	case 0x1b:
		return `\e`, true
	}
	if r < 0x20 || r == 0x7f {
		return fmt.Sprintf(`\x%02X`, r), true
	}
	return "", false
}

// UnescapeFromEdit converts an edited string back to its stored form. It
// rejects unknown or truncated escape sequences rather than guessing, so a
// typo is reported to the sysop instead of being silently written to disk.
func UnescapeFromEdit(s string) (string, error) {
	var b strings.Builder
	b.Grow(len(s))

	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		if runes[i] != '\\' {
			b.WriteRune(runes[i])
			continue
		}
		if i+1 >= len(runes) {
			return "", fmt.Errorf(`unfinished escape: a lone \ at the end (use \\ for a literal backslash)`)
		}
		i++
		switch runes[i] {
		case '\\':
			b.WriteByte('\\')
		case 'r':
			b.WriteByte('\r')
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case 'e':
			b.WriteByte(0x1b)
		case 'x':
			if i+2 >= len(runes) {
				return "", fmt.Errorf(`unfinished escape: \x needs two hex digits`)
			}
			hi, ok := hexDigit(runes[i+1])
			if !ok {
				return "", fmt.Errorf(`invalid escape: \x%c%c is not two hex digits`, runes[i+1], runes[i+2])
			}
			lo, ok := hexDigit(runes[i+2])
			if !ok {
				return "", fmt.Errorf(`invalid escape: \x%c%c is not two hex digits`, runes[i+1], runes[i+2])
			}
			b.WriteByte(byte(hi<<4 | lo))
			i += 2
		default:
			return "", fmt.Errorf(`unknown escape: \%c (use \\ for a literal backslash)`, runes[i])
		}
	}
	return b.String(), nil
}

// hexDigit returns the numeric value of a hexadecimal digit.
func hexDigit(r rune) (int, bool) {
	switch {
	case r >= '0' && r <= '9':
		return int(r - '0'), true
	case r >= 'a' && r <= 'f':
		return int(r-'a') + 10, true
	case r >= 'A' && r <= 'F':
		return int(r-'A') + 10, true
	}
	return 0, false
}
