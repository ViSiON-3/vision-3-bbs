package menueditor

import (
	"fmt"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/stringeditor"
	"github.com/ViSiON-3/vision-3-bbs/internal/tuiart"
)

// viewMenuEditScreen renders the per-menu field editor.
// Faithfully recreates MENUEDIT.PAS EditMenu / Edit_Menu.
func (m Model) viewMenuEditScreen() string {
	if m.menuEditIdx < 0 || m.menuEditIdx >= len(m.menus) {
		return m.viewMenuListScreen()
	}
	entry := m.menus[m.menuEditIdx]

	// Fixed rows: title(1) + box(border, box title, empty, fields, empty, info,
	// empty, border = len(fields)+7) + help(1). Derived from the live field
	// count: this screen used to hardcode a box height for seven fields, and
	// silently overflowed the terminal once the list grew to thirteen.
	boxRows := len(m.menuFields) + 7
	topPad, bottomPad := tuiart.Split(m.height, boxRows+2)

	sc := tuiart.NewScreen(m.width, m.backdrop)

	// === Row 1: Title bar ===
	sc.Line(titleBarStyle.Render(centerText("-- ViSiON/3 Menu Editor v1.0 --", m.width)))
	sc.BgRows(topPad)

	// Box dimensions matching Pascal GrowBOX(2,6,78,20)
	// width = 78-2-2 = 74 interior
	boxW := 74
	padL := max(0, (m.width-boxW-2)/2)
	padR := max(0, m.width-padL-boxW-2)

	// box writes one row of box content wrapped in the side borders.
	box := func(content string) {
		sc.Line(sc.Pad(padL, padR,
			editBorderStyle.Render("│")+content+editBorderStyle.Render("│")))
	}
	emptyRow := func() { box(fieldDisplayStyle.Render(strings.Repeat(" ", boxW))) }

	// === Top border ===
	// MENUEDIT.PAS: Color(15,8) → dark gray bg, white fg for box
	sc.Line(sc.Pad(padL, padR, editBorderStyle.Render("┌"+strings.Repeat("─", boxW)+"┐")))

	// === Title row inside box ===
	// MENUEDIT.PAS: Color(15,12) center_write 'Command Editing...'
	boxTitle := "Editing Menu: " + entry.Name + ".MNU"
	if entry.Data.Title != "" {
		boxTitle = entry.Data.Title + " (" + entry.Name + ".MNU)"
	}
	box(editTitleStyle.Render(centerText(boxTitle, boxW)))

	// === Empty separator ===
	emptyRow()

	// === Field rows ===
	for i, f := range m.menuFields {
		box(m.renderMenuField(i, f, &entry.Data, boxW))
	}

	// === Empty row above info ===
	emptyRow()

	// === Info row: current file + number (centered) ===
	box(editInfoLabelStyle.Render(
		centerText(fmt.Sprintf("Menu %d of %d", m.menuEditIdx+1, len(m.menus)), boxW)))

	// === Empty row below info ===
	emptyRow()

	// === Bottom border ===
	sc.Line(sc.Pad(padL, padR, editBorderStyle.Render("└"+strings.Repeat("─", boxW)+"┘")))

	sc.BgRows(bottomPad)

	// === Help bar ===
	sc.Last(helpBarStyle.Render(centerText(
		"PgUp/PgDn Prev/Next  F2 Delete  F5 Add New  F10 Edit Commands  ESC Back", m.width)))

	// Overlay dialogs
	result := sc.String()
	switch m.mode {
	case modeDeleteMenuConfirm:
		result = m.overlayConfirmDialog(result,
			"-- Delete Menu --",
			fmt.Sprintf("Delete %s.MNU and all commands? ", entry.Name))
	case modeAddMenu:
		result = m.overlayInputDialog(result,
			"-- Create New Menu --",
			"New menu filename (8 chars): ",
			m.textInput.View())
	}

	return result
}

// renderMenuField renders a single menu field row inside the edit box.
// Two spaces of left padding are applied to match the Pascal x=4 column offset.
func (m Model) renderMenuField(fieldIdx int, f fieldDef, d *MenuData, boxW int) string {
	const lpad = 2
	isActive := m.menuEditFld == fieldIdx

	label := f.Label + ": "
	labelLen := len(label)

	var value string
	if f.GetM != nil {
		value = f.GetM(d)
	}

	// Cap display width to available space inside the box, -1 for right margin
	maxW := boxW - lpad - labelLen - 1
	if maxW < 0 {
		maxW = 0
	}
	dispW := f.Width
	if dispW > maxW {
		dispW = maxW
	}

	rawW := lpad + labelLen + dispW
	leftPad := fieldDisplayStyle.Render(strings.Repeat(" ", lpad))

	// Actively editing this field
	if isActive && m.mode == modeMenuEditField {
		// Bound the widget's own output, not just the padding around it. It
		// appends a cursor cell after the text, so a value that fills the
		// field renders Width+1 cells; clamping only the padding leaves the
		// oversized view to overrun the box.
		view, inputW := tuiart.FitInput(m.textInput.View(), boxW-lpad-labelLen)
		fillW := max(0, boxW-lpad-labelLen-inputW)
		return leftPad + fieldLabelStyle.Render(label) + view +
			fieldDisplayStyle.Render(strings.Repeat(" ", fillW))
	}

	// Display value (truncated to dispW)
	displayValue := padRight(value, dispW)

	if isActive {
		// Highlighted field (ready to edit) — truncate to dispW to prevent overflow
		displayVal := value
		if len(displayVal) > dispW {
			displayVal = displayVal[:dispW]
		}
		fillStr := strings.Repeat(string(fieldFillChar), max(0, dispW-len(displayVal)))
		result := leftPad + fieldLabelStyle.Render(label) + fieldEditStyle.Render(displayVal+fillStr)
		result += fieldDisplayStyle.Render(strings.Repeat(" ", max(0, boxW-rawW)))
		return result
	}

	// Normal display — use pipe-code renderer if defined (e.g. Prompt Line fields)
	if f.Render != nil {
		rendered := f.Render(value, dispW)
		visLen := stringeditor.PlainTextLength(value)
		if visLen > dispW {
			visLen = dispW
		}
		fill := fieldDisplayStyle.Render(strings.Repeat(" ", max(0, dispW-visLen)))
		result := leftPad + fieldLabelStyle.Render(label) + rendered + fill
		result += fieldDisplayStyle.Render(strings.Repeat(" ", max(0, boxW-rawW)))
		return result
	}
	result := leftPad + fieldLabelStyle.Render(label) + fieldDisplayStyle.Render(displayValue)
	result += fieldDisplayStyle.Render(strings.Repeat(" ", max(0, boxW-rawW)))
	return result
}
