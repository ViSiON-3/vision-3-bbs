package ansi

import (
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"
)

// DecodeBody returns a stored message body, or a header field stored the same
// way, as UTF-8. charset is the message's CHRS kludge (DisplayMessage.Charset),
// or "" when it has none.
//
// A body that is valid UTF-8 is taken as UTF-8 whatever CHRS says: labels
// naming a single-byte set on what is really UTF-8 are common (CP850 and
// LATIN-1 on Russian and English text), while a single-byte body that is
// valid UTF-8 throughout is not seen in practice. Otherwise CHRS picks the
// decoding. A UTF-8 label means the body is UTF-8 with stray bytes in it,
// usually a quote of a CP437 message, so only those bytes are read as CP437.
// The Cyrillic sets are decoded by name; their box drawing sits where CP437's
// does, so art drawn in them survives. Every other label is read as CP437:
// art is often labelled with whatever its editor defaulted to, and the other
// sets put letters where CP437 draws blocks and lines.
//
// Escape sequences and control characters are ASCII and decode as themselves;
// DecodeBody filters nothing.
func DecodeBody(body, charset string) string {
	if utf8.ValidString(body) {
		return body
	}
	switch charset {
	case "UTF-8", "UTF8":
		return mixedUTF8(body)
	case "CP866", "+7_FIDO", "+7":
		return decodeCharmap(body, charmap.CodePage866)
	case "CP1251":
		return decodeCharmap(body, charmap.Windows1251)
	case "KOI8-R":
		return decodeCharmap(body, charmap.KOI8R)
	case "KOI8-U":
		return decodeCharmap(body, charmap.KOI8U)
	}
	return string(CP437BytesToUTF8([]byte(body)))
}

// mixedUTF8 keeps every valid UTF-8 sequence in s and reads each byte that
// is not part of one as CP437.
func mixedUTF8(s string) string {
	var b strings.Builder
	b.Grow(len(s) + len(s)/4)
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			r = Cp437ToUnicode[s[i]]
		}
		b.WriteRune(r)
		i += size
	}
	return b.String()
}

// decodeCharmap decodes s from a single-byte character set. Every byte has a
// character in the sets DecodeBody passes, and ASCII, escape sequences
// included, decodes as itself.
func decodeCharmap(s string, cm *charmap.Charmap) string {
	out, err := cm.NewDecoder().String(s)
	if err != nil {
		return string(CP437BytesToUTF8([]byte(s)))
	}
	return out
}
