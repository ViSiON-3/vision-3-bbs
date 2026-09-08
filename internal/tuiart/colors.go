package tuiart

import "github.com/charmbracelet/lipgloss"

// DOS CGA/VGA color palette as explicit truecolor hex values.
//
// These are the canonical IBM VGA RGB values for the standard 16 DOS colors.
// We pin them to explicit hex (rather than ANSI palette indices 0-15) so the
// editors target the VGA palette instead of the host terminal's themed ANSI
// slots. Most Mac terminal themes map ANSI "blue" to a bright, low-contrast
// shade, which washed out the white/yellow-on-blue UI; the authentic VGA navy
// (#0000AA) restores the intended contrast. lipgloss renders these as
// truecolor where available and degrades to the fixed 256-color cube
// otherwise, which preserves the palette on modern terminals more reliably
// than themed 16-color ANSI slots.
var Palette = [16]string{
	"#000000", // 0:  Black
	"#0000AA", // 1:  Blue
	"#00AA00", // 2:  Green
	"#00AAAA", // 3:  Cyan
	"#AA0000", // 4:  Red
	"#AA00AA", // 5:  Magenta
	"#AA5500", // 6:  Brown
	"#AAAAAA", // 7:  Light Gray
	"#555555", // 8:  Dark Gray
	"#5555FF", // 9:  Light Blue
	"#55FF55", // 10: Light Green
	"#55FFFF", // 11: Light Cyan
	"#FF5555", // 12: Light Red
	"#FF55FF", // 13: Light Magenta
	"#FFFF55", // 14: Yellow
	"#FFFFFF", // 15: White
}

// Style creates a lipgloss style from a DOS TextAttr byte (bg*16 + fg).
func Style(attr byte) lipgloss.Style {
	fg := attr & 0x0F
	bg := (attr >> 4) & 0x07
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(Palette[fg])).
		Background(lipgloss.Color(Palette[bg]))
}

// Color creates a lipgloss style from separate DOS bg, fg values matching the
// Pascal Color(bg, fg) procedure.
func Color(bg, fg int) lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(Palette[fg&0x0F])).
		Background(lipgloss.Color(Palette[bg&0x07]))
}

// HeaderBarStyle is the persistent title bar drawn on the first row of every
// editor screen: white on dark gray.
var HeaderBarStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color(Palette[15])).
	Background(lipgloss.Color(Palette[8])).
	Bold(true)

// HelpBarStyle is the keyboard-shortcut bar on the last row of every editor
// screen: bold white on dark gray.
var HelpBarStyle = Color(0, 15).Bold(true).Background(lipgloss.Color(Palette[8]))

// FillStyle is the fallback background fill used when no backdrop art is
// available: Fill_Screen('░', 7, 1), gray on blue.
var FillStyle = Color(1, 7)
