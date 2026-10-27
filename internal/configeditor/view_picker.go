package configeditor

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// viewLookupPicker renders the lookup picker popup over the current edit screen.
func (m Model) viewLookupPicker() string {
	// Render the underlying edit screen as background
	var background string
	if m.pickerReturnMode == modeRecordEdit {
		background = m.viewRecordEdit()
	} else {
		background = m.viewSysConfigEdit()
	}

	lines := strings.Split(background, "\n")

	// Determine picker title from current field label
	pickerTitle := "Select Item"
	var fields []fieldDef
	if m.pickerReturnMode == modeRecordEdit {
		fields = m.recordFields
	} else {
		fields = m.sysFields
	}
	if m.editField < len(fields) {
		pickerTitle = fmt.Sprintf("Select %s", fields[m.editField].Label)
	}

	total := len(m.pickerItems)
	visible := pickerVisibleRows
	if visible > total {
		visible = total
	}

	// The box is 50 columns, wider when an entry needs it (the ► prefix and
	// a trailing space included), up to what the terminal leaves beside the
	// border; padRight cuts anything longer.
	boxW := 50
	for _, item := range m.pickerItems {
		boxW = max(boxW, utf8.RuneCountInString(item.Display)+4)
	}
	boxW = min(boxW, max(50, m.width-4))
	// box: top border + title + separator + items + empty + bottom border
	boxH := visible + 5

	startRow := (m.height - boxH) / 2
	startCol := (m.width - boxW - 2) / 2
	if startRow < 0 {
		startRow = 0
	}
	if startCol < 0 {
		startCol = 0
	}

	side := menuBorderStyle.Render("│")

	var dialogLines []string

	// Top border
	dialogLines = append(dialogLines,
		menuBorderStyle.Render("┌"+strings.Repeat("─", boxW)+"┐"))

	// Title
	dialogLines = append(dialogLines,
		side+menuHeaderStyle.Render(centerText(pickerTitle, boxW))+side)

	// Separator
	dialogLines = append(dialogLines,
		menuBorderStyle.Render("│"+strings.Repeat("─", boxW)+"│"))

	// Items
	for i := 0; i < visible; i++ {
		idx := m.pickerScroll + i
		if idx >= total {
			// Empty row
			dialogLines = append(dialogLines,
				side+menuItemStyle.Render(strings.Repeat(" ", boxW))+side)
			continue
		}
		item := m.pickerItems[idx]

		prefix := "   "
		if idx == m.pickerCursor {
			prefix = " ► "
		}
		content := prefix + item.Display
		content = padRight(content, boxW)

		var styled string
		if idx == m.pickerCursor {
			styled = menuHighlightStyle.Render(content)
		} else {
			styled = menuItemStyle.Render(content)
		}
		dialogLines = append(dialogLines, side+styled+side)
	}

	// Empty line
	dialogLines = append(dialogLines,
		side+menuItemStyle.Render(strings.Repeat(" ", boxW))+side)

	// Bottom border
	dialogLines = append(dialogLines,
		menuBorderStyle.Render("└"+strings.Repeat("─", boxW)+"┘"))

	// Help line (not inside box, rendered below)
	helpLine := menuItemStyle.Render(
		centerText("Enter - Select  |  ESC - Cancel", boxW+2))
	dialogLines = append(dialogLines, helpLine)

	// Overlay on background
	endCol := startCol + boxW + 2
	for i, dl := range dialogLines {
		row := startRow + i
		if row >= 0 && row < len(lines) {
			left := padToCol(lines[row], startCol)
			right := skipToCol(lines[row], endCol)
			lines[row] = left + dl + right
		}
	}

	return strings.Join(lines, "\n")
}
