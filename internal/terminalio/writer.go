package terminalio

import (
	"io"
	"unicode/utf8"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
)

func writeUTF8Mode(writer io.Writer, data []byte) error {
	out := make([]byte, 0, len(data))
	i := 0

	for i < len(data) {
		b := data[i]

		if b == 0x1B {
			end := findAnsiEnd(data, i)
			out = append(out, data[i:end]...)
			i = end
			continue
		}

		// Process a text span up to the next ANSI escape.
		spanStart := i
		for i < len(data) && data[i] != 0x1B {
			i++
		}
		span := data[spanStart:i]

		// Decide the span's encoding as a whole rather than rune by rune.
		//
		// Deciding per rune looks like it handles mixed content, but the two
		// encodings are not separable that way: plenty of adjacent CP437 pairs
		// form a structurally valid UTF-8 sequence, and the decoder then
		// swallows both bytes and emits one unrelated character. CP437 line
		// art is full of such pairs — ▄│ (DC B3) decodes as U+0733, a Syriac
		// combining mark — so exactly the content most likely to be CP437 was
		// the content most likely to be misread (#280).
		//
		// A span that is valid UTF-8 throughout is taken as UTF-8; anything
		// else is taken as CP437 and mapped byte for byte. Where a span is
		// valid under both this still guesses, and only the message's own CHRS
		// kludge can settle it — but it no longer mangles art that is
		// unambiguously CP437.
		if utf8.Valid(span) {
			out = append(out, span...)
			continue
		}
		for _, sb := range span {
			if sb < 0x80 {
				out = append(out, sb)
				continue
			}
			mapped := ansi.Cp437ToUnicode[sb]
			if mapped == 0 {
				out = append(out, '?')
			} else {
				out = append(out, []byte(string(mapped))...)
			}
		}
	}

	_, err := writer.Write(out)
	return err
}

// Helper to find the end of an ANSI sequence
// Returns the index *after* the terminating character
func findAnsiEnd(data []byte, start int) int {
	// Assumes data[start] == 0x1B
	if start+1 >= len(data) {
		return start + 1 // Incomplete sequence
	}

	seqEnd := start + 1
	switch data[seqEnd] {
	case '[': // CSI
		seqEnd++
		for seqEnd < len(data) {
			c := data[seqEnd]
			if c >= '@' && c <= '~' { // Check for standard CSI terminators
				seqEnd++ // Include the terminator
				return seqEnd
			}
			seqEnd++
			if seqEnd-start > 32 {
				return seqEnd
			} // Safety break
		}
		return seqEnd // Terminator not found
	case '(', ')': // Character set designation
		if start+2 < len(data) {
			return start + 3 // ESC ( B, ESC ) 0 etc.
		}
		return start + 2 // Incomplete
	default:
		// Other simple ESC sequences (like ESC M - Reverse Index)
		return start + 2 // Assume 2 bytes total: ESC + char
	}
}

// WriteProcessedBytes writes bytes while honoring the configured output mode.
// CP437 mode converts UTF-8 runes to CP437 where possible and passes raw
// single bytes through unchanged. UTF-8 mode converts raw CP437 high bytes
// to UTF-8 when they are not valid UTF-8 sequences.
func WriteProcessedBytes(writer io.Writer, rawBytes []byte, mode ansi.OutputMode) error {
	switch mode {
	case ansi.OutputModeCP437:
		return WriteStringCP437(writer, rawBytes, mode)
	case ansi.OutputModeUTF8:
		return writeUTF8Mode(writer, rawBytes)
	default:
		_, err := writer.Write(rawBytes)
		return err
	}
}

// WriteStringCP437 writes a UTF-8 string (e.g., from strings.json) to a
// CP437 terminal, converting multi-byte UTF-8 runes to their CP437 byte
// equivalents. ANSI escape sequences are passed through untouched.
// In non-CP437 modes, bytes are written as-is (UTF-8 passthrough).
func WriteStringCP437(writer io.Writer, data []byte, mode ansi.OutputMode) error {
	if mode != ansi.OutputModeCP437 {
		_, err := writer.Write(data)
		return err
	}

	// Convert UTF-8 runes to CP437 bytes only for spans that are entirely valid UTF-8,
	// preserving ANSI escapes and passing raw CP437 bytes through unchanged.
	out := make([]byte, 0, len(data))
	i := 0
	for i < len(data) {
		b := data[i]

		// Pass ANSI escape sequences through untouched
		if b == 0x1B {
			end := findAnsiEnd(data, i)
			out = append(out, data[i:end]...)
			i = end
			continue
		}

		// Process a text span up to next ANSI escape.
		spanStart := i
		for i < len(data) && data[i] != 0x1B {
			i++
		}
		span := data[spanStart:i]

		// Decide the span's encoding as a whole, as the comment above always
		// claimed and the rune-by-rune loop here did not. The same CP437 pairs
		// that form valid UTF-8 sequences were decoded as one rune and then
		// converted back — and since nothing like U+0733 exists in CP437, the
		// pair came out as a single '?'. That is the mojibake #280 reported,
		// on a terminal that would have rendered the original bytes correctly
		// had they simply been left alone.
		if !utf8.Valid(span) {
			// Already CP437: hand it to the terminal untouched.
			out = append(out, span...)
			continue
		}
		for _, r := range string(span) {
			if r < 0x80 {
				out = append(out, byte(r))
				continue
			}
			if cp437Byte, ok := ansi.UnicodeToCP437[r]; ok {
				out = append(out, cp437Byte)
			} else {
				out = append(out, '?')
			}
		}
	}

	_, err := writer.Write(out)
	return err
}
