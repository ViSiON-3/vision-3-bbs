package menu

import "github.com/stlalpha/vision3/internal/ansi"

// unicodeASCIILookalikes maps Unicode characters that are visually identical (or nearly
// identical) to ASCII characters but have no CP437 representation. Common in FTN handles
// from systems like Synchronet where Cyrillic lookalikes appear in usernames.
var unicodeASCIILookalikes = map[rune]byte{
	'А': 'A', 'В': 'B', 'С': 'C', 'Е': 'E', 'К': 'K', 'М': 'M',
	'Н': 'H', 'О': 'O', 'Р': 'P', 'Т': 'T', 'Х': 'X',
	'а': 'a', 'е': 'e', 'о': 'o', 'р': 'p', 'с': 'c', 'х': 'x',
}

// toCP437Safe converts a UTF-8 string to a CP437-safe byte string for display on
// CP437 terminals. Each rune is mapped in priority order:
//  1. ASCII (< 0x80) — passed through as-is
//  2. CP437 equivalent — mapped via UnicodeToCP437
//  3. Visual ASCII lookalike — e.g. Cyrillic е → 'e'
//  4. '?' fallback
func toCP437Safe(s string) string {
	out := make([]byte, 0, len(s))
	for _, r := range s {
		if r < 0x80 {
			out = append(out, byte(r))
		} else if cp437Byte, ok := ansi.UnicodeToCP437[r]; ok {
			out = append(out, cp437Byte)
		} else if ascii, ok := unicodeASCIILookalikes[r]; ok {
			out = append(out, ascii)
		} else {
			out = append(out, '?')
		}
	}
	return string(out)
}
