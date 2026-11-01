package ansi

import (
	"strings"
	"unicode/utf8"
)

// maxBodyStringLen bounds how far a dropped terminal string (OSC, DCS, APC, PM,
// SOS) may reach looking for its terminator. Past it, or with no terminator at
// all, only the rest of the line goes.
const maxBodyStringLen = 4096

// BodyText returns a message body with everything taken out that a terminal
// would do something with other than draw. It is an allowlist. What stays:
//
//   - SGR (colour and attributes): ESC[...m
//   - cursor movement and position: ESC[ with final A B C D E F G H d f
//   - erase: ESC[J and ESC[K, except ESC[3J, which wipes the caller's scrollback
//   - save and restore: ESC[s, ESC[u, ESC 7, ESC 8
//   - tab, line feed, carriage return
//   - the other C0 bytes that have a CP437 glyph and that terminals ignore
//
// Everything else goes whole: device queries, whose answers land in the caller's
// input, modes, resets, charset changes, C1 in its UTF-8 form, and the strings
// OSC, DCS, APC, PM and SOS with their payloads. ENQ, BEL, BS, VT, FF, SO, SI,
// CAN, SUB and DEL go too.
//
// A string ends at ST, at BEL for OSC, at CAN or SUB, or just before any other
// escape. One with no such end within maxBodyStringLen ends with its line, so an
// unterminated string costs one line and not the rest of the message.
//
// s should be UTF-8 already, the way the reader settles a body's charset. A byte
// that is not valid UTF-8 is kept as it is, because it is a CP437 character:
// 0x9B is a cent sign, and only the encoded U+009B is CSI.
func BodyText(s string) string {
	return filterBody(s, false, false)
}

// StoredBodyText is BodyText for a body as it is stored, before its charset is
// settled, for output that must keep the stored bytes, such as an offline mail
// packet. A body that is valid UTF-8 gets BodyText. Any other body is single-byte
// text, CP437 or another set, where every byte is one character: bytes 0x80 to
// 0xFF are all kept, so a pair such as C2 9B (┬¢ in CP437) is not taken for the
// UTF-8 form of CSI, and only the 7-bit escapes and controls are filtered.
func StoredBodyText(s string) string {
	return filterBody(s, false, !utf8.ValidString(s))
}

// ArtBodyText is BodyText for a body going to the ANSI art renderer rather than
// the terminal. It also keeps auto-wrap on and off (ESC[?7h, ESC[?7l), which the
// renderer acts on and art relies on, and which never reach the caller from
// there. Nothing but the renderer may be given its result.
func ArtBodyText(s string) string {
	return filterBody(s, true, false)
}

// keptC0 reports whether a C0 byte may stay in a body. Terminals ignore these or
// draw them as CP437 glyphs, so art may use them.
func keptC0(c rune) bool {
	switch c {
	case 0x05, 0x07, 0x08, 0x0B, 0x0C, 0x0E, 0x0F, 0x18, 0x1A, 0x1B:
		return false
	}
	return true
}

// filterBody does the work for BodyText, ArtBodyText and StoredBodyText. With
// singleByte, s is read a byte at a time and every byte from 0x80 up is a
// character to keep, never part of a UTF-8 sequence or a C1 control.
func filterBody(s string, art, singleByte bool) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if singleByte {
			r, size = rune(s[i]), 1
			if r >= utf8.RuneSelf {
				r = utf8.RuneError // kept by the default case
			}
		}
		switch {
		case r == 0x1B:
			i = filterEscape(&b, s, i, art)
		case r == 0x9B:
			i, _ = csiEnd(s, i+size)
		case r == 0x90 || r == 0x98 || r == 0x9E || r == 0x9F:
			i = bodyStringEnd(s, i+size, false)
		case r == 0x9D:
			i = bodyStringEnd(s, i+size, true)
		case r == 0x7F || (r >= 0x80 && r <= 0x9F), r < 0x20 && !keptC0(r):
			i += size
		default:
			b.WriteString(s[i : i+size])
			i += size
		}
	}
	return b.String()
}

// filterEscape handles the ESC at s[i], writing it to b if it is allowed, and
// returns where to go on from.
func filterEscape(b *strings.Builder, s string, i int, art bool) int {
	j := i + 1
	if j >= len(s) {
		return j
	}
	switch c := s[j]; {
	case c == '[':
		end, ok := csiEnd(s, j+1)
		if ok {
			writeAllowedCSI(b, s[i:end], art)
		}
		return end
	case c == ']':
		return bodyStringEnd(s, j+1, true)
	case c == 'P' || c == 'X' || c == '^' || c == '_':
		return bodyStringEnd(s, j+1, false)
	case c == '7' || c == '8':
		b.WriteString(s[i : j+1])
		return j + 1
	case c >= 0x20 && c <= 0x2F: // intermediates then a final, such as ESC ( 0
		j++
		for j < len(s) && s[j] >= 0x20 && s[j] <= 0x2F {
			j++
		}
		if j < len(s) && s[j] >= 0x30 && s[j] <= 0x7E {
			j++
		}
		return j
	case c >= 0x30 && c <= 0x7E: // two-byte forms, such as ESC c
		return j + 1
	}
	return j // a bare ESC: what follows is processed as itself
}

// csiEnd returns where a CSI whose parameters start at s[i] ends, and whether it
// was complete. An incomplete one ends before the byte that stopped it, which is
// still processed as itself: a line break left by a message cut mid-sequence.
func csiEnd(s string, i int) (int, bool) {
	for i < len(s) && s[i] >= 0x20 && s[i] <= 0x3F {
		i++
	}
	if i < len(s) && s[i] >= 0x40 && s[i] <= 0x7E {
		return i + 1, true
	}
	return i, false
}

// writeAllowedCSI writes a complete CSI sequence to b if it is on the allowlist.
func writeAllowedCSI(b *strings.Builder, seq string, art bool) {
	params, final := seq[2:len(seq)-1], seq[len(seq)-1]
	if art && (final == 'h' || final == 'l') && strings.HasPrefix(params, "?") {
		// One sequence may set several modes, and only auto-wrap is wanted.
		for _, p := range strings.Split(params[1:], ";") {
			if p == "7" {
				b.WriteString("\x1b[?7")
				b.WriteByte(final)
				return
			}
		}
		return
	}
	for i := 0; i < len(params); i++ {
		c := params[i]
		if (c < '0' || c > '9') && c != ';' && (c != ':' || final != 'm') {
			return // a private marker or an intermediate selects another function
		}
	}
	switch final {
	case 'm', 'A', 'B', 'C', 'D', 'E', 'F', 'G', 'H', 'd', 'f':
	case 'J', 'K':
		if params != "" && params != "0" && params != "1" && params != "2" {
			return
		}
	case 's', 'u':
		if params != "" {
			return
		}
	default:
		return
	}
	b.WriteString(seq)
}

// bodyStringEnd returns where a terminal string whose payload starts at s[i]
// ends. The bounds are on BodyText.
func bodyStringEnd(s string, i int, osc bool) int {
	limit := min(len(s), i+maxBodyStringLen)
	for k := i; k < limit; {
		r, size := utf8.DecodeRuneInString(s[k:])
		switch {
		case r == 0x9C, r == 0x18, r == 0x1A, r == 0x07 && osc:
			return k + size
		case r == 0x1B:
			if k+1 < len(s) && s[k+1] == '\\' {
				return k + 2
			}
			return k
		}
		k += size
	}
	if n := strings.IndexAny(s[i:limit], "\r\n"); n >= 0 {
		return i + n
	}
	// Don't stop inside a character. Raw bytes may have no start to find, and going
	// back past i would read the introducer again, forever.
	for end := limit; end > i && end < len(s); end-- {
		if utf8.RuneStart(s[end]) {
			return end
		}
	}
	return limit
}

// PlainText returns a message body as plain text, for a consumer that prints
// it with no idea what it holds, such as a script. Everything BodyText takes out
// goes, and so do the sequences it keeps: colour, cursor movement, erasing and
// cursor save and restore. Line breaks (CR LF, a lone CR or LF) all become LF.
// Tab stays; every other control character goes. Pipe codes are text and stay.
//
// s should be UTF-8, as DecodeBody returns it. A byte that is not valid UTF-8 is
// kept as it is, as in BodyText.
func PlainText(s string) string {
	s = BodyText(s)
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == 0x1B:
			// BodyText leaves only complete CSI sequences and ESC 7 / ESC 8.
			if i+1 < len(s) && s[i+1] == '[' {
				i, _ = csiEnd(s, i+2)
			} else {
				i += 2
			}
		case r == '\r':
			b.WriteByte('\n')
			i += size
			if i < len(s) && s[i] == '\n' {
				i++
			}
		case r == '\n' || r == '\t':
			b.WriteRune(r)
			i += size
		case r < 0x20 || r == 0x7F || (r >= 0x80 && r <= 0x9F):
			i += size
		default:
			b.WriteString(s[i : i+size])
			i += size
		}
	}
	return b.String()
}
