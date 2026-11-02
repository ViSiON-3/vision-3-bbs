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
	if IsUTF8Charset(charset) {
		return mixedUTF8(body)
	}
	if cm := NamedCharmap(charset); cm != nil {
		return decodeCharmap(body, cm)
	}
	return string(CP437BytesToUTF8([]byte(body)))
}

// CharsetName returns the character set a CHRS kludge value names, upper
// case: "cp866 2" gives "CP866", and "" gives "".
func CharsetName(chrs string) string {
	f := strings.Fields(chrs)
	if len(f) == 0 {
		return ""
	}
	return strings.ToUpper(f[0])
}

// IsUTF8Charset reports whether charset, a name as CharsetName returns it,
// is UTF-8.
func IsUTF8Charset(charset string) bool {
	return charset == "UTF-8" || charset == "UTF8"
}

// NamedCharmap returns the single-byte set charset names, for the sets
// decoded by name: the Cyrillic CP866 (also labelled +7_FIDO and +7),
// CP1251, KOI8-R and KOI8-U. It returns nil for every other name, which the
// callers read as CP437 (or as UTF-8, see IsUTF8Charset). charset is a name as
// CharsetName returns it. Message bodies and FTN header fields share this
// list, so a message's header and body agree on what its bytes say.
func NamedCharmap(charset string) *charmap.Charmap {
	switch charset {
	case "CP866", "+7_FIDO", "+7":
		return charmap.CodePage866
	case "CP1251":
		return charmap.Windows1251
	case "KOI8-R":
		return charmap.KOI8R
	case "KOI8-U":
		return charmap.KOI8U
	}
	return nil
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
