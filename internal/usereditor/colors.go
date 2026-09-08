package usereditor

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/ViSiON-3/vision-3-bbs/internal/tuiart"
)

// The DOS palette and its style constructors live in internal/tuiart so every
// editor renders in the same colors. These aliases keep the user editor's call
// sites unchanged; only the palette underneath changes, from ANSI 256 indices
// to the pinned VGA hex values.
var dosColors = tuiart.Palette

// dosStyle creates a lipgloss style from a DOS TextAttr byte (bg*16 + fg).
func dosStyle(attr byte) lipgloss.Style { return tuiart.Style(attr) }

// dosColor creates a lipgloss style from separate DOS bg, fg values
// matching the Pascal Color(bg, fg) procedure.
func dosColor(bg, fg int) lipgloss.Style { return tuiart.Color(bg, fg) }

// --- Title/Status bars ---
// UE.PAS: Color(8,15) for title and bottom bar
var titleBarStyle = tuiart.HeaderBarStyle

// --- Background fill ---
// UE.PAS: Fill_Screen('░',7,1) → gray on blue. Used only where the backdrop
// art is unavailable; tuiart.Backdrop renders this fill itself.
var bgFillStyle = tuiart.FillStyle

// --- List box border ---
// UE.PAS: Color(1,9) GrowBox
var listBorderStyle = dosColor(1, 9)

// --- List header text ---
// UE.PAS: Color(1,14) centered header inside box
var listHeaderStyle = dosColor(1, 14)

// --- Column title ---
// UE.PAS: Color(9,15) for column header row
var columnTitleStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color(dosColors[15])).
	Background(lipgloss.Color(dosColors[9]))

// --- Normal list item ---
// UE.PAS: Color(1,15)
var listItemStyle = dosColor(1, 15)

// --- Tagged marker ---
// UE.PAS: Color(1,14) for the √ char
var taggedStyle = dosColor(1, 14)

// --- Highlighted item ---
// UE.PAS: Color(0,9) for tag area, Color(0,14) for text
var highlightTagStyle = dosColor(0, 9)
var highlightTextStyle = dosColor(0, 14)

// --- Edit screen ---
// UE.PAS: PColor=31 → attr: bg=1(blue), fg=15(white)→ actually 31 = 1*16+15
// Wait - PColor=31 in DISPEDIT: normcolor=15, incolor=31, pcolor=9
// But Def_Colors sets PColor:=31, NormColor:=30, InColor:=14
// PColor=31: bg=1(blue), fg=15(white) → field labels
// NormColor=30: bg=1(blue), fg=14(yellow) → displayed values
// InColor=14: just fg=14(yellow), no bg → editing values

// Field label (prompt) color: PColor=31 → blue bg, white fg
var fieldLabelStyle = dosStyle(31)

// Field value (display mode): NormColor=30 → blue bg, yellow fg
var fieldDisplayStyle = dosStyle(30)

// Field value (edit mode): InColor=14 → yellow
var fieldEditStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color(dosColors[14]))

// Edit screen title bar — the same bar as the list screen, not a near-duplicate.
var editTitleStyle = tuiart.HeaderBarStyle

// Edit screen border: Color(1,9) same as list
var editBorderStyle = dosColor(1, 9)

// Edit screen user number text: Color(1,9) then Color(1,14)
var editInfoLabelStyle = dosColor(1, 9)
var editInfoValueStyle = dosColor(1, 14)

// --- Dialog styles ---
// Ask dialogs: PColor = 5*16+14 = 94, NormColor = 5*16+15 = 95
var dialogBorderStyle = dosStyle(95) // magenta bg, white fg
var dialogTitleStyle = lipgloss.NewStyle().
					Foreground(lipgloss.Color(dosColors[15])).
					Background(lipgloss.Color(dosColors[13])).
					Bold(true)
var dialogTextStyle = dosStyle(94) // magenta bg, yellow fg

// --- Help screen ---
// UE.PAS Help_Screen: Color(4,15) box, Color(4,14) title
var helpBoxStyle = dosColor(4, 15)
var helpTitleStyle = dosColor(4, 14)

// --- Bottom help bar ---
var helpBarStyle = tuiart.HelpBarStyle

// --- Flash message ---
var flashMessageStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color(dosColors[14]))

// --- Confirm dialog buttons ---
// Active: bright white on black (high contrast, clearly selected)
var buttonActiveStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color(dosColors[15])).
	Background(lipgloss.Color(dosColors[0])).
	Bold(true)

// Inactive: white on magenta (visible but not highlighted)
var buttonInactiveStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color(dosColors[15])).
	Background(lipgloss.Color(dosColors[5]))

// --- Separator style (dim label between editable and read-only sections) ---
var separatorStyle = dosColor(1, 9)

// --- Edit field fill character ---
const fieldFillChar = '░'
