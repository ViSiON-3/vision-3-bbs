package ansi

import (
	"fmt"
	"strings"
)

// VGAPalette is the 16-colour palette of the IBM VGA text mode, which BBS art
// is drawn for and retro terminals (SyncTERM, NetRunner) reproduce exactly.
var VGAPalette = [16][3]uint8{
	{0x00, 0x00, 0x00}, {0xAA, 0x00, 0x00}, {0x00, 0xAA, 0x00}, {0xAA, 0x55, 0x00},
	{0x00, 0x00, 0xAA}, {0xAA, 0x00, 0xAA}, {0x00, 0xAA, 0xAA}, {0xAA, 0xAA, 0xAA},
	{0x55, 0x55, 0x55}, {0xFF, 0x55, 0x55}, {0x55, 0xFF, 0x55}, {0xFF, 0xFF, 0x55},
	{0x55, 0x55, 0xFF}, {0xFF, 0x55, 0xFF}, {0x55, 0xFF, 0xFF}, {0xFF, 0xFF, 0xFF},
}

// SetVGAPalette returns xterm OSC sequences that load VGAPalette into the
// terminal's 16 ANSI colours (OSC 4) and make its default foreground and
// background VGA light grey on black (OSC 10/11).
//
// Modern terminals theme those colours: palette black is often a dark grey,
// and the default background is a separate colour again. Art that mixes
// explicit black (ESC[40m) with reset cells (ESC[0m), as nearly all BBS art
// does, then shows grey patches. Redefining the palette fixes every cell at
// once — including areas erased by ED/EL, which take the default background
// no matter what SGR the BBS sends. Terminals without OSC support ignore it.
func SetVGAPalette() string {
	var b strings.Builder
	for i, c := range VGAPalette {
		fmt.Fprintf(&b, "\x1b]4;%d;rgb:%02x/%02x/%02x\a", i, c[0], c[1], c[2])
	}
	fg, bg := VGAPalette[7], VGAPalette[0]
	fmt.Fprintf(&b, "\x1b]10;rgb:%02x/%02x/%02x\a", fg[0], fg[1], fg[2])
	fmt.Fprintf(&b, "\x1b]11;rgb:%02x/%02x/%02x\a", bg[0], bg[1], bg[2])
	return b.String()
}

// ResetPalette returns OSC sequences that restore the terminal's own palette
// and default colours, undoing SetVGAPalette (OSC 104, 110, 111).
func ResetPalette() string {
	return "\x1b]104\a\x1b]110\a\x1b]111\a"
}
