package configeditor

import (
	"github.com/charmbracelet/lipgloss"
)

// DOS CGA/VGA color palette as explicit truecolor hex values.
//
// These are the canonical IBM VGA RGB values for the standard 16 DOS colors.
// We pin them to explicit hex (rather than ANSI palette indices 0-15) so the
// editor renders the same regardless of the host terminal's color theme. Most
// Mac terminal themes map ANSI "blue" to a bright, low-contrast shade, which
// washed out the white/yellow-on-blue UI; the authentic VGA navy (#0000AA)
// restores the intended contrast. lipgloss renders these as truecolor where
// available and degrades to the fixed 256-color cube otherwise — in both cases
// independent of the user's 16-color theme.
var dosColors = [16]string{
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

// DOS CGA/VGA background colors (the low 8 colors usable as a background).
var dosBgColors = [8]string{
	"#000000", // 0: Black BG
	"#0000AA", // 1: Blue BG
	"#00AA00", // 2: Green BG
	"#00AAAA", // 3: Cyan BG
	"#AA0000", // 4: Red BG
	"#AA00AA", // 5: Magenta BG
	"#AA5500", // 6: Brown BG
	"#AAAAAA", // 7: Light Gray BG
}

// dosStyle creates a lipgloss style from a DOS TextAttr byte (bg*16 + fg).
func dosStyle(attr byte) lipgloss.Style {
	fg := attr & 0x0F
	bg := (attr >> 4) & 0x07
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(dosColors[fg])).
		Background(lipgloss.Color(dosBgColors[bg]))
}

// dosColor creates a lipgloss style from separate DOS bg, fg values
// matching the Pascal Color(bg, fg) procedure.
func dosColor(bg, fg int) lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(dosColors[fg&0x0F])).
		Background(lipgloss.Color(dosBgColors[bg&0x07]))
}

// --- Global header bar (white text on dark gray bg) ---
var globalHeaderBarStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color(dosColors[15])).
	Background(lipgloss.Color(dosColors[8])).
	Bold(true)

// --- Title/Status bars ---
var titleBarStyle = dosColor(0, 15).Bold(true).Background(lipgloss.Color(dosColors[8]))

// --- Background fill ---
// Fill_Screen('░',7,1) → gray on blue
var bgFillStyle = dosColor(1, 7)

// --- Menu box border ---
var menuBorderStyle = dosColor(1, 9)

// --- Menu header text ---
var menuHeaderStyle = dosColor(1, 14)

// --- Normal menu item ---
var menuItemStyle = dosColor(1, 15)

// --- Highlighted menu item ---
var menuHighlightStyle = dosColor(0, 14)

// --- Field label (prompt) color: blue bg, white fg ---
var fieldLabelStyle = dosStyle(31)

// --- Field value (display mode): blue bg, yellow fg ---
var fieldDisplayStyle = dosStyle(30)

// --- Field value (edit mode): blue bg, yellow fg ---
var fieldEditStyle = dosColor(1, 14)

// --- Edit screen border ---
var editBorderStyle = dosColor(1, 9)

// --- Edit info label/value ---
var editInfoLabelStyle = dosColor(1, 9)
var editInfoValueStyle = dosColor(1, 14)

// --- Dialog styles ---
var dialogBorderStyle = dosStyle(95) // magenta bg, white fg
var dialogTitleStyle = lipgloss.NewStyle().
					Foreground(lipgloss.Color(dosColors[15])).
					Background(lipgloss.Color(dosColors[13])).
					Bold(true)
var dialogTextStyle = dosStyle(94) // magenta bg, yellow fg

// --- Help screen ---
var helpBoxStyle = dosColor(4, 15)
var helpTitleStyle = dosColor(4, 14)

// --- Bottom help bar ---
var helpBarStyle = dosColor(0, 15).Bold(true).Background(lipgloss.Color(dosColors[8]))

// --- Flash message ---
var flashMessageStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color(dosColors[14]))

// --- Confirm dialog buttons ---
var buttonActiveStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color(dosColors[15])).
	Background(lipgloss.Color(dosColors[0])).
	Bold(true)

var buttonInactiveStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color(dosColors[15])).
	Background(lipgloss.Color(dosColors[5]))

// --- Reorder source row (green bg, white fg) ---
var reorderSourceStyle = dosColor(2, 15)

// --- Separator style ---
var separatorStyle = dosColor(1, 9)

// --- Edit field fill character ---
const fieldFillChar = '░'
