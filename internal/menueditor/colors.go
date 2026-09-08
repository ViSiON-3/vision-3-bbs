package menueditor

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/ViSiON-3/vision-3-bbs/internal/tuiart"
)

// The DOS palette and its style constructors live in internal/tuiart so every
// editor renders in the same colors. These aliases keep the menu editor's call
// sites unchanged; only the palette underneath changes, from ANSI 256 indices
// to the pinned VGA hex values.
var dosColors = tuiart.Palette

// dosStyle creates a lipgloss style from a DOS TextAttr byte (bg<<4 | fg).
func dosStyle(attr byte) lipgloss.Style { return tuiart.Style(attr) }

// dosColor creates a lipgloss style from separate DOS bg, fg values
// matching the Pascal Color(bg, fg) procedure.
func dosColor(bg, fg int) lipgloss.Style { return tuiart.Color(bg, fg) }

// --- Title/Status bars ---
// MENUEDIT.PAS: Color(8,15) → dark gray bg, white fg
var titleBarStyle = tuiart.HeaderBarStyle
var helpBarStyle = tuiart.HelpBarStyle

// --- Background fill ---
// MENUEDIT.PAS: Fill_Screen('░',7,1) → light gray fg, blue bg. Used only when
// the backdrop art is unavailable; tuiart.Backdrop renders this fill itself.
var bgFillStyle = tuiart.FillStyle

// --- Menu list box ---
// MENUEDIT.PAS Open_Screen: Color(3,11) GrowBox → cyan bg, light cyan fg
var listBorderStyle = dosColor(3, 11)

// MENUEDIT.PAS: Color(11,3) column header → light cyan bg, cyan fg
var listHeaderStyle = dosColor(3, 11)
var listColTitleStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color(dosColors[3])).
	Background(lipgloss.Color(dosColors[11]))

// MENUEDIT.PAS: Color(3,15) → cyan bg, white fg for normal items
var listItemStyle = dosColor(3, 15)

// MENUEDIT.PAS: Color(1,15) → blue bg, white fg for highlighted item
var listHighlightStyle = dosColor(1, 15)

// --- Edit box (menu/command edit screens) ---
// MENUEDIT.PAS Edit_Menu: Color(8,15) box border → dark gray bg, white fg (actually uses Color(15,8) for White_Colors)
// White_Colors: PColor=$F1(bg=7/white, fg=1/blue), NormColor=$F0(bg=7/white, fg=0/black)
var editBorderStyle = dosColor(0, 15).Background(lipgloss.Color(dosColors[8]))

// Field label: PColor=$F1 → light gray bg, blue fg
var fieldLabelStyle = dosStyle(0xF1)

// Field value (inactive): NormColor=$F0 → light gray bg, black fg
var fieldDisplayStyle = dosStyle(0xF0)

// Field value (active/highlighted): InColor=$0E → black bg, yellow fg
var fieldEditStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color(dosColors[14]))

// Field fill character for active (not-yet-editing) fields
const fieldFillChar = '░'

// Edit box title / section header: Color(15,12) → white bg, light red fg
var editTitleStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color(dosColors[12])).
	Background(lipgloss.Color(dosColors[15]))

// Info label/value inside edit screens: Color(15,8) and Color(15,4)
var editInfoLabelStyle = dosStyle(0xF0)

// --- Confirm dialog ---
// MENUEDIT.PAS Ask_Colors: PColor=94($5E), NormColor=95($5F) → magenta bg
var dialogBorderStyle = dosStyle(0x5F) // magenta bg, white fg
var dialogTitleStyle = lipgloss.NewStyle().
					Foreground(lipgloss.Color(dosColors[15])).
					Background(lipgloss.Color(dosColors[13])).
					Bold(true)
var dialogTextStyle = dosStyle(0x5E) // magenta bg, yellow fg

// --- Input dialog (Add Menu, etc.) ---
var inputDialogBorderStyle = dosStyle(0x5F)
var inputDialogTextStyle = dosStyle(0x5E)

// --- Help screen ---
var helpBoxStyle = dosColor(4, 15)
var helpTitleStyle = dosColor(4, 14)

// --- Confirm buttons ---
var buttonActiveStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color(dosColors[15])).
	Background(lipgloss.Color(dosColors[0])).
	Bold(true)
var buttonInactiveStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color(dosColors[15])).
	Background(lipgloss.Color(dosColors[5]))

// --- Flash message ---
var flashMessageStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color(dosColors[14]))
