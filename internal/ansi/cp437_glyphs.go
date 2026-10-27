package ansi

import "unicode/utf8"

// cp437ControlGlyphs are the glyphs the IBM PC draws for the control bytes,
// which art uses as characters: ☺ ♥ ► ▲ ▼ and the rest. Zero means the byte
// is acted on rather than drawn, as by ANSI.SYS: NUL, BEL, BS, TAB, LF, CR,
// the EOF marker and ESC.
var cp437ControlGlyphs = [32]rune{
	0, '☺', '☻', '♥', '♦', '♣', '♠', 0,
	0, 0, 0, '♂', '♀', 0, '♫', '☼',
	'►', '◄', '↕', '‼', '¶', '§', '▬', '↨',
	'↑', '↓', 0, 0, '∟', '↔', '▲', '▼',
}

// CP437ArtToUTF8 converts CP437 art for a UTF-8 terminal. High bytes become
// their Unicode characters, as the UTF-8 output writer would make them, and
// the control bytes art draws as glyphs become those glyphs, which the writer
// leaves as control bytes a terminal draws nothing for. Escape sequences pass
// through unchanged. Art that is already valid UTF-8 is returned as it is.
func CP437ArtToUTF8(data []byte) []byte {
	if utf8.Valid(data) && !hasControlGlyph(data) {
		return data
	}
	if utf8.Valid(data) {
		// UTF-8 art: only the control glyphs need drawing.
		return mapControlGlyphs(data, false)
	}
	return mapControlGlyphs(data, true)
}

// DrawCP437ControlGlyphs returns UTF-8 text with the control bytes a CP437
// terminal draws as glyphs replaced by those glyphs, as for CP437ArtToUTF8;
// everything else, escape sequences included, is unchanged. It suits output
// already converted from CP437 a chunk at a time, as the WFC mirrors a CP437
// caller.
func DrawCP437ControlGlyphs(data []byte) []byte {
	if !hasControlGlyph(data) {
		return data
	}
	return mapControlGlyphs(data, false)
}

func hasControlGlyph(data []byte) bool {
	for i := 0; i < len(data); i++ {
		if data[i] == 0x1B {
			i = skipEscape(data, i)
			continue
		}
		if data[i] < 0x20 && cp437ControlGlyphs[data[i]] != 0 {
			return true
		}
	}
	return false
}

// mapControlGlyphs writes data with its control glyphs drawn, and with its
// high bytes taken as CP437 when cp437 is set.
func mapControlGlyphs(data []byte, cp437 bool) []byte {
	out := make([]byte, 0, len(data)+len(data)/2)
	for i := 0; i < len(data); i++ {
		b := data[i]
		switch {
		case b == 0x1B:
			end := skipEscape(data, i)
			out = append(out, data[i:end+1]...)
			i = end
		case b < 0x20 && cp437ControlGlyphs[b] != 0:
			out = utf8.AppendRune(out, cp437ControlGlyphs[b])
		case b >= 0x80 && cp437:
			out = utf8.AppendRune(out, Cp437ToUnicode[b])
		default:
			out = append(out, b)
		}
	}
	return out
}

// skipEscape returns the index of the last byte of the escape sequence
// starting at data[i]: a CSI sequence through its final byte, or ESC and
// the one byte after it.
func skipEscape(data []byte, i int) int {
	if i+1 >= len(data) {
		return i
	}
	if data[i+1] != '[' {
		return i + 1
	}
	j := i + 2
	for j < len(data) && (data[j] < 0x40 || data[j] > 0x7E) {
		j++
	}
	if j >= len(data) {
		return len(data) - 1
	}
	return j
}
