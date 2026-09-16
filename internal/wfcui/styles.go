package wfcui

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/ViSiON-3/vision-3-bbs/internal/tuiart"
)

// DOS palette indices (CGA order, matching tuiart.Palette). Foregrounds may
// use 0–15; backgrounds 0–7.
const (
	cBlack        uint8 = 0
	cBlue         uint8 = 1
	cGreen        uint8 = 2
	cCyan         uint8 = 3
	cRed          uint8 = 4
	cMagenta      uint8 = 5
	cBrown        uint8 = 6
	cLightGray    uint8 = 7
	cDarkGray     uint8 = 8
	cLightBlue    uint8 = 9
	cLightGreen   uint8 = 10
	cLightCyan    uint8 = 11
	cLightRed     uint8 = 12
	cLightMagenta uint8 = 13
	cYellow       uint8 = 14
	cWhite        uint8 = 15
)

// Glyphs used by the layout. ASCII mode substitutes each via asciiGlyph.
const (
	gTL, gTR, gBL, gBR = '┌', '┐', '└', '┘'
	gH, gV             = '─', '│'
	gLower, gUpper     = '▄', '▀'
	gFull              = '█'
	gUp, gDown         = '↑', '↓'
)

// asciiGlyph maps a box/block glyph to its 7-bit stand-in.
func asciiGlyph(r rune) rune {
	switch r {
	case gTL, gTR, gBL, gBR:
		return '+'
	case gH:
		return '-'
	case gV:
		return '|'
	case gLower, gUpper:
		return ' '
	case gFull:
		return '#'
	case gUp:
		return '^'
	case gDown:
		return 'v'
	}
	if r > 0x7e {
		return '?'
	}
	return r
}

// styler turns (fg, bg) cell attributes into lipgloss styles, honouring the
// NoColor and ASCII options. Styles are cached: a frame has a few dozen
// distinct runs but only a handful of distinct attribute pairs.
type styler struct {
	noColor bool
	ascii   bool
	cache   map[uint16]lipgloss.Style
}

func newStyler(opts Options) *styler {
	return &styler{noColor: opts.NoColor, ascii: opts.ASCII, cache: make(map[uint16]lipgloss.Style, 32)}
}

// style returns the lipgloss style for a foreground/background pair. The
// colors are pinned to the VGA palette (see tuiart.Palette) rather than the
// terminal's themed ANSI slots so blue stays navy and yellow stays yellow.
func (s *styler) style(fg, bg uint8) lipgloss.Style {
	key := uint16(bg)<<8 | uint16(fg)
	if st, ok := s.cache[key]; ok {
		return st
	}
	st := tuiart.Color(int(bg&7), int(fg&15))
	s.cache[key] = st
	return st
}

// glyph applies the ASCII substitution when enabled.
func (s *styler) glyph(r rune) rune {
	if s.ascii {
		return asciiGlyph(r)
	}
	return r
}
