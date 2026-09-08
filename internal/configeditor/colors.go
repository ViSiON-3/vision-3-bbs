package configeditor

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/ViSiON-3/vision-3-bbs/internal/tuiart"
)

// The DOS palette and its style constructors live in internal/tuiart so the
// string editor renders in the same colors. These aliases keep the config
// editor's call sites unchanged.
var dosColors = tuiart.Palette

// dosStyle creates a lipgloss style from a DOS TextAttr byte (bg*16 + fg).
func dosStyle(attr byte) lipgloss.Style { return tuiart.Style(attr) }

// dosColor creates a lipgloss style from separate DOS bg, fg values
// matching the Pascal Color(bg, fg) procedure.
func dosColor(bg, fg int) lipgloss.Style { return tuiart.Color(bg, fg) }

// --- Global header bar (white text on dark gray bg) ---
var globalHeaderBarStyle = tuiart.HeaderBarStyle

// --- Background fill ---
// Fill_Screen('░',7,1) → gray on blue
var bgFillStyle = tuiart.FillStyle

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
var helpBarStyle = tuiart.HelpBarStyle

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

// listEmptyHintStyle draws the "nothing here yet, press I" line an empty
// record list shows in place of its first row. Yellow, not the list's own
// white, so it reads as guidance rather than as a record.
var listEmptyHintStyle = dosColor(1, 14)

// --- Edit field fill character ---
const fieldFillChar = '░'
