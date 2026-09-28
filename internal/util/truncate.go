package util

import "unicode/utf8"

// TruncateBytes clamps s to at most maxBytes BYTES, cutting on a UTF-8 rune
// boundary so a multi-byte character is dropped whole rather than split.
//
// Use it for fixed-width wire and file fields whose size is specified in
// bytes (FTN packet headers, QWK message headers and BBS IDs): the result
// never exceeds the field, and it never ends in a partial UTF-8 sequence that
// would read back as invalid text. Bytes that are not valid UTF-8 (raw CP437,
// say) are each treated as a one-byte character and kept as they are, so a
// CP437 value is cut exactly at maxBytes.
//
// For limits counted in characters or screen columns, use ansi.TruncateRunes
// or ansi.TruncateVisible instead.
func TruncateBytes(s string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(s) <= maxBytes {
		return s
	}
	end := 0
	for end < len(s) {
		// DecodeRuneInString reports size 1 for a stray invalid byte, so the
		// cut never skips past one. utf8.RuneLen would report 3 for those,
		// since range surfaces them as RuneError, and land inside the next
		// character.
		_, size := utf8.DecodeRuneInString(s[end:])
		if end+size > maxBytes {
			break
		}
		end += size
	}
	return s[:end]
}
