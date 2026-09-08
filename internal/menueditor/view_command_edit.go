package menueditor

import (
	"fmt"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/tuiart"
	"github.com/ViSiON-3/vision-3-bbs/internal/uitext"
)

// viewCommandEditScreen renders the per-command field editor.
// Faithfully recreates MENUEDIT.PAS Edit_Command.
func (m Model) viewCommandEditScreen() string {
	if m.cmdEditIdx < 0 || m.cmdEditIdx >= len(m.cmds) {
		return m.viewCommandListScreen()
	}

	menuName := ""  // filename stem, used for file references
	menuTitle := "" // friendly title for display headings
	if m.cmdsMenuIdx >= 0 && m.cmdsMenuIdx < len(m.menus) {
		e := m.menus[m.cmdsMenuIdx]
		menuName = e.Name
		menuTitle = e.Data.Title
		if menuTitle == "" {
			menuTitle = e.Name
		}
	}

	cmd := m.cmds[m.cmdEditIdx]

	// Fixed rows: title(1) + box(border, box title, empty, fields, info, empty,
	// border = len(fields)+6) + help(1). Derived from the live field count for
	// the same reason as the menu edit screen.
	boxRows := len(m.cmdFields) + 6
	topPad, bottomPad := tuiart.Split(m.height, boxRows+2)

	sc := tuiart.NewScreen(m.width, m.backdrop)

	// === Row 1: Title bar ===
	sc.Line(titleBarStyle.Render(centerText("-- ViSiON/3 Menu Editor v1.0 --", m.width)))
	sc.BgRows(topPad)

	// Box dimensions: MENUEDIT.PAS GrowBox(2,9,78,16) → 74 cols wide
	boxW := 74
	padL := max(0, (m.width-boxW-2)/2)
	padR := max(0, m.width-padL-boxW-2)

	box := func(content string) {
		sc.Line(sc.Pad(padL, padR,
			editBorderStyle.Render("│")+content+editBorderStyle.Render("│")))
	}
	emptyRow := func() { box(fieldDisplayStyle.Render(strings.Repeat(" ", boxW))) }

	// === Top border ===
	sc.Line(sc.Pad(padL, padR, editBorderStyle.Render("┌"+strings.Repeat("─", boxW)+"┐")))

	// === Title row inside box ===
	// MENUEDIT.PAS: Color(15,12) Center_Write('Command Editing (MenuTitle)')
	box(editTitleStyle.Render(centerText(fmt.Sprintf("Command Editing (%s)", menuTitle), boxW)))

	// === Empty separator ===
	emptyRow()

	// === Field rows ===
	for i, f := range m.cmdFields {
		box(m.renderCmdField(i, f, &cmd, boxW))
	}

	// === Info row: file + command number ===
	// MENUEDIT.PAS shows file at col 50 row 11 and number at col 50 row 15;
	// consolidated here to avoid per-field right-column overflow.
	infoFile := fmt.Sprintf("  File: %s.CFG", menuName)
	infoNum := fmt.Sprintf("Cmd: %d of %d", m.cmdEditIdx+1, len(m.cmds))
	box(editInfoLabelStyle.Render(padRight(padRight(infoFile, 40)+infoNum, boxW)))

	// === Empty bottom row ===
	emptyRow()

	// === Bottom border ===
	sc.Line(sc.Pad(padL, padR, editBorderStyle.Render("└"+strings.Repeat("─", boxW)+"┘")))

	sc.BgRows(bottomPad)

	// === Help bar ===
	sc.Last(helpBarStyle.Render(centerText(
		"PgUp/PgDn Prev/Next  F2 Delete  F5 Add New  F8 Abort  ESC Save+Back", m.width)))

	// Overlay dialogs
	result := sc.String()
	if m.mode == modeDeleteCmdConfirm {
		desc := cmd.NodeActivity
		if desc == "" {
			desc = cmd.Keys
		}
		result = m.overlayConfirmDialog(result,
			"-- Delete Command --",
			fmt.Sprintf("Delete command '%s'? ", desc))
	}

	return result
}

// renderCmdField renders a single command field row inside the edit box.
// Uses the full box width — max value width = boxW - lpad(2) - labelLen(17).
func (m Model) renderCmdField(fieldIdx int, f fieldDef, d *CmdData, boxW int) string {
	const lpad = 2
	isActive := m.cmdEditFld == fieldIdx

	label := f.Label + ": "
	labelLen := len(label)

	var value string
	if f.GetC != nil {
		value = f.GetC(d)
	}

	// Cap display/input width to available space inside the box, -1 for right margin
	maxW := boxW - lpad - labelLen - 1
	if maxW < 0 {
		maxW = 0
	}
	dispW := f.Width
	if dispW > maxW {
		dispW = maxW
	}

	rawW := lpad + labelLen + dispW
	leftPadStr := fieldDisplayStyle.Render(strings.Repeat(" ", lpad))

	// Actively editing this field
	if isActive && m.mode == modeCommandEditField {
		// Measure what the widget actually renders rather than assuming it
		// occupies textInput.Width cells: it appends a cursor cell after the
		// text, so assuming Width overflowed the box by exactly one column.
		inputW := uitext.ApproximateVisibleLen(m.textInput.View())
		if inputW > maxW {
			inputW = maxW
		}
		fillW := max(0, boxW-lpad-labelLen-inputW)
		return leftPadStr + fieldLabelStyle.Render(label) + m.textInput.View() +
			fieldDisplayStyle.Render(strings.Repeat(" ", fillW))
	}

	// Display value
	displayValue := padRight(value, dispW)

	if isActive {
		// Highlighted (ready to edit) — truncate to dispW to prevent overflow
		displayVal := value
		if len(displayVal) > dispW {
			displayVal = displayVal[:dispW]
		}
		fillStr := strings.Repeat(string(fieldFillChar), max(0, dispW-len(displayVal)))
		result := leftPadStr + fieldLabelStyle.Render(label) + fieldEditStyle.Render(displayVal+fillStr)
		result += fieldDisplayStyle.Render(strings.Repeat(" ", max(0, boxW-rawW)))
		return result
	}

	result := leftPadStr + fieldLabelStyle.Render(label) + fieldDisplayStyle.Render(displayValue)
	result += fieldDisplayStyle.Render(strings.Repeat(" ", max(0, boxW-rawW)))
	return result
}
