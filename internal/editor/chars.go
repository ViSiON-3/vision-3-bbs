package editor

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// A line's columns are characters (#1051): a valid UTF-8 character, or a
// single byte that isn't one, such as raw CP437 a quote carries
// (quote_cp437_test.go). Converting a line to []rune would turn those bytes
// into U+FFFD, so the editor never does: it finds character boundaries with
// utf8.DecodeRuneInString and slices the string, keeping every byte.

// splitChars is s as its characters, each a substring of s, so joining them
// gives back s byte for byte.
func splitChars(s string) []string {
	out := make([]string, 0, len(s))
	for i := 0; i < len(s); {
		_, size := utf8.DecodeRuneInString(s[i:])
		out = append(out, s[i:i+size])
		i += size
	}
	return out
}

// isSpaceChar reports whether c, one character from splitChars, is a space.
// A byte that isn't valid UTF-8 never is.
func isSpaceChar(c string) bool {
	r, size := utf8.DecodeRuneInString(c)
	return size > 0 && r != utf8.RuneError && unicode.IsSpace(r)
}

// joinChars is strings.Join(cs, "").
func joinChars(cs []string) string { return strings.Join(cs, "") }
