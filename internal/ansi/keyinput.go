package ansi

import "unicode/utf8"

// DecodeExtendedKey processes one keystroke byte b (128-255) according to
// mode, returning the line with the decoded character appended (if any), the
// raw bytes to echo back to the terminal (if any), and the updated
// utf8Pending accumulator for the next call.
//
// CP437 mode is a single-byte table lookup: b maps through Cp437ToUnicode to
// exactly one rune, which is appended to line as UTF-8 (so callers only ever
// store valid UTF-8) while the RAW byte b is echoed unchanged -- a CP437
// terminal draws directly from the byte value, so echoing anything else would
// not round-trip. A byte with no mapping (Cp437ToUnicode[b] == 0) is dropped:
// it is not stored, matching how line readers already drop any other
// keystroke they won't accept, and it avoids ever writing invalid/unintended
// data into stored records.
//
// UTF-8 mode receives one byte of a multi-byte sequence per call (that is how
// bytes >= 128 arrive from a key reader on a real connection), so b is
// accumulated into utf8Pending until utf8.FullRune reports a complete sequence
// (or 4 bytes -- utf8.UTFMax, the longest possible UTF-8 encoding -- have
// accumulated without one, which guards against a malformed sequence that
// would otherwise never complete and silently swallow all subsequent input).
// Once complete, the sequence is appended to line and echoed verbatim.
//
// A malformed sequence is not stored or echoed, and the decoder resynchronises
// by discarding one byte at a time rather than the whole accumulator: a stray
// lead byte is often immediately followed by a real character's lead byte, and
// dropping the buffer wholesale would swallow that character too.
//
// Callers should reset utf8Pending to nil whenever a non-extended keystroke
// (ASCII, control code, backspace) arrives, so an abandoned partial sequence
// cannot absorb the bytes of the next character.
func DecodeExtendedKey(line []byte, mode OutputMode, b byte, utf8Pending []byte) (newLine []byte, echo []byte, pending []byte) {
	if mode == OutputModeUTF8 {
		utf8Pending = append(utf8Pending, b)
		for len(utf8Pending) > 0 {
			if !utf8.FullRune(utf8Pending) && len(utf8Pending) < utf8.UTFMax {
				return line, nil, utf8Pending
			}
			r, size := utf8.DecodeRune(utf8Pending)
			if r == utf8.RuneError && size <= 1 {
				// Malformed: drop one byte and retry, so a stray lead byte does
				// not take the following character down with it. Discarding the
				// whole buffer would swallow a valid lead byte sitting behind it.
				utf8Pending = utf8Pending[1:]
				continue
			}
			seq := append([]byte(nil), utf8Pending[:size]...)
			return append(line, seq...), seq, nil
		}
		return line, nil, nil
	}
	r := Cp437ToUnicode[b]
	if r == 0 {
		return line, nil, utf8Pending
	}
	var buf [utf8.UTFMax]byte
	n := utf8.EncodeRune(buf[:], r)
	return append(line, buf[:n]...), []byte{b}, utf8Pending
}

// BackspaceRune removes the last RUNE (not byte) from line. line is expected
// to hold valid UTF-8 (ASCII, or an extended character decoded via
// DecodeExtendedKey), so a byte-based backspace would cut a multi-byte
// character in half. The caller still echoes "\b \b" exactly once, matching
// the single terminal column every stored character occupies.
func BackspaceRune(line []byte) []byte {
	if len(line) == 0 {
		return line
	}
	_, size := utf8.DecodeLastRune(line)
	return line[:len(line)-size]
}
