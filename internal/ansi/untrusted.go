package ansi

import "strings"

// OneLine returns text from another system flattened to one line for display:
// each control character becomes a space, so it cannot start an escape sequence,
// move the cursor or scroll the screen. The C1 range is included because U+009B
// is the 8-bit form of CSI.
//
// Pipe codes are left alone. Text going to a screen that expands them also needs
// EscapePipes or EscapeScreenPipeCodes; a screen that does not expand them, such
// as the config editor, needs only this.
func OneLine(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || (r >= 0x7F && r <= 0x9F) {
			return ' '
		}
		return r
	}, s)
}

// EscapePipes doubles every pipe, so ReplacePipeCodes prints s as written. Use
// it for text that was never meant to carry colour. The doubled pipes draw as one
// column each, so fit s to its column first.
func EscapePipes(s string) string {
	return strings.ReplaceAll(s, "|", "||")
}

// PlainName returns a name from another system ready to keep as a local name
// that screens will later expand: one line, with its pipes turned into slashes.
// Doubling them is not an option here, since not every screen that shows a stored
// name expands it, and those would show "||".
func PlainName(s string) string {
	return strings.ReplaceAll(OneLine(s), "|", "/")
}

// DefangScreenCodes returns a stored name made safe to expand when it may have
// come from another system: one line, with the pipe of each screen code (|CL,
// |CR, |DE, |P, |PP) turned into a slash. Colour codes, "||" and other pipes stay,
// because a sysop may have written them and every screen reads them the same way.
func DefangScreenCodes(s string) string {
	s = OneLine(s)
	data := []byte(s)
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(data); {
		if data[i] != '|' {
			b.WriteByte(data[i])
			i++
			continue
		}
		if i+1 < len(data) && data[i+1] == '|' {
			b.WriteString("||")
			i += 2
			continue
		}
		if _, n := colorPipeCodeAt(data, i); n > 0 {
			b.Write(data[i : i+n])
			i += n
			continue
		}
		if screenPipeCodeAt(data, i) {
			b.WriteByte('/')
		} else {
			b.WriteByte('|')
		}
		i++
	}
	return b.String()
}

// screenPipeCodeAt reports whether ReplacePipeCodes would expand a code at
// data[i]. The caller has already ruled out colour codes.
func screenPipeCodeAt(data []byte, i int) bool {
	for n := 4; n >= 2; n-- {
		if i+n <= len(data) {
			if _, ok := pipeCodeReplacements[string(data[i:i+n])]; ok {
				return true
			}
		}
	}
	return false
}
