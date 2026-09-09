package configeditor

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// truncateToDisplayWidth truncates a string to fit within maxWidth display cells,
// safely handling multi-byte UTF-8 characters by iterating runes.
func truncateToDisplayWidth(s string, maxWidth int) string {
	if lipgloss.Width(s) <= maxWidth {
		return s
	}

	var result strings.Builder
	currentWidth := 0

	for _, r := range s {
		runeStr := string(r)
		runeWidth := lipgloss.Width(runeStr)

		if currentWidth+runeWidth > maxWidth {
			break
		}

		result.WriteRune(r)
		currentWidth += runeWidth
	}

	return result.String()
}

// viewRecordEdit renders the single-record field editor popup.
func (m Model) viewRecordEdit() string {
	var b strings.Builder

	row := 0

	// Global header
	b.WriteString(m.globalHeaderLine())
	b.WriteByte('\n')
	row++

	boxW := 70
	// Find max row in fields
	maxRow := 0
	for _, f := range m.recordFields {
		if f.Row > maxRow {
			maxRow = f.Row
		}
	}
	visibleRows := maxRow
	if visibleRows > maxFieldRows {
		visibleRows = maxFieldRows
	}
	// Fixed rows: header(1) + box(visibleRows+7) + helpbar(1) = visibleRows+9,
	// plus the help region (help content + a blank separator), which is 2 rows
	// unless the active field's help wraps to a second line.
	helpRegionRows := m.fieldHelpRegionRows(m.recordFields, boxW)
	extraV := maxInt(0, m.height-visibleRows-9-helpRegionRows)
	topPad := extraV / 2
	bottomPad := extraV - topPad

	for i := 0; i < topPad; i++ {
		b.WriteString(m.backdrop.Line(row))
		b.WriteByte('\n')
		row++
	}

	padL := maxInt(0, (m.width-boxW-2)/2)
	padR := maxInt(0, m.width-padL-boxW-2)

	// Top border
	b.WriteString(m.backdrop.Segment(row, 0, padL) +
		editBorderStyle.Render("┌"+strings.Repeat("─", boxW)+"┐") +
		m.backdrop.Segment(row, m.width-maxInt(0, padR), maxInt(0, padR)))
	b.WriteByte('\n')
	row++

	// Box title
	boxTitleText := fmt.Sprintf("Edit %s", m.recordTypeTitle())
	if m.recordType == "ftn" && m.recordEditIdx < 0 {
		boxTitleText = "FTN Global Settings"
	}
	boxTitleLine := editBorderStyle.Render("│") +
		menuHeaderStyle.Render(centerText(boxTitleText, boxW)) +
		editBorderStyle.Render("│")
	b.WriteString(m.backdrop.Segment(row, 0, padL) + boxTitleLine +
		m.backdrop.Segment(row, m.width-maxInt(0, padR), maxInt(0, padR)))
	b.WriteByte('\n')
	row++

	// Record name header
	headerText := m.recordEditHeader()
	headerLine := editBorderStyle.Render("│") +
		menuHeaderStyle.Render(centerText(headerText, boxW)) +
		editBorderStyle.Render("│")
	b.WriteString(m.backdrop.Segment(row, 0, padL) + headerLine +
		m.backdrop.Segment(row, m.width-maxInt(0, padR), maxInt(0, padR)))
	b.WriteByte('\n')
	row++

	// emptyFieldLine renders a blank field-row line at the current row; it is
	// used at multiple, differently-numbered rows below, so it must be
	// recomputed each time rather than cached in a variable.
	emptyFieldLine := func() string {
		return m.backdrop.Segment(row, 0, padL) +
			editBorderStyle.Render("│") +
			fieldDisplayStyle.Render(strings.Repeat(" ", boxW)) +
			editBorderStyle.Render("│") +
			m.backdrop.Segment(row, m.width-maxInt(0, padR), maxInt(0, padR))
	}

	// Empty line
	b.WriteString(emptyFieldLine())
	b.WriteByte('\n')
	row++

	// Field rows (windowed by fieldScroll)
	firstRow := m.fieldScroll + 1
	lastRow := m.fieldScroll + visibleRows
	if lastRow > maxRow {
		lastRow = maxRow
	}
	for fr := firstRow; fr <= lastRow; fr++ {
		rowContent := m.renderRecordEditRow(fr, boxW)
		line := m.backdrop.Segment(row, 0, padL) +
			editBorderStyle.Render("│") +
			rowContent +
			editBorderStyle.Render("│") +
			m.backdrop.Segment(row, m.width-maxInt(0, padR), maxInt(0, padR))
		b.WriteString(line)
		b.WriteByte('\n')
		row++
	}
	// Pad remaining rows if fewer fields than visibleRows
	for fr := lastRow + 1; fr <= m.fieldScroll+visibleRows; fr++ {
		b.WriteString(emptyFieldLine())
		b.WriteByte('\n')
		row++
	}

	// Empty line
	b.WriteString(emptyFieldLine())
	b.WriteByte('\n')
	row++

	// Record info
	total := m.recordCount()
	scrollHint := ""
	if maxRow > maxFieldRows {
		if m.fieldScroll > 0 && lastRow < maxRow {
			scrollHint = " [▲▼ more]"
		} else if m.fieldScroll > 0 {
			scrollHint = " [▲ more]"
		} else if lastRow < maxRow {
			scrollHint = " [▼ more]"
		}
	}
	infoText := fmt.Sprintf("Record %d of %d%s", m.recordEditIdx+1, total, scrollHint)
	if m.recordEditIdx < 0 {
		infoText = "Global Settings" + scrollHint
	}
	infoLine := editBorderStyle.Render("│") +
		editInfoLabelStyle.Render(centerText(infoText, boxW)) +
		editBorderStyle.Render("│")
	b.WriteString(m.backdrop.Segment(row, 0, padL) + infoLine +
		m.backdrop.Segment(row, m.width-maxInt(0, padR), maxInt(0, padR)))
	b.WriteByte('\n')
	row++

	// Bottom border
	b.WriteString(m.backdrop.Segment(row, 0, padL) +
		editBorderStyle.Render("└"+strings.Repeat("─", boxW)+"┘") +
		m.backdrop.Segment(row, m.width-maxInt(0, padR), maxInt(0, padR)))
	b.WriteByte('\n')
	row++

	for i := 0; i < bottomPad; i++ {
		b.WriteString(m.backdrop.Line(row))
		b.WriteByte('\n')
		row++
	}

	// Help area: active field help (wraps to a second line when long) plus a
	// blank separator; fieldHelpRegionRows above budgeted its height.
	b.WriteString(m.renderFieldHelpLine(m.recordFields, padL, padR, boxW, row))
	b.WriteByte('\n')
	row += helpRegionRows

	helpBarStr := "Enter - Edit  |  PgUp/PgDn - Records  |  ESC - Return"
	if m.recordEditIdx < 0 {
		helpBarStr = "Enter - Edit  |  ESC - Return"
	}
	helpText := centerText(helpBarStr, m.width)
	b.WriteString(helpBarStyle.Render(helpText))

	return b.String()
}

// recordEditHeader returns a header string for the record edit screen.
func (m Model) recordEditHeader() string {
	switch m.recordType {
	case "msgarea":
		if m.recordEditIdx < len(m.configs.MsgAreas) {
			a := m.configs.MsgAreas[m.recordEditIdx]
			return fmt.Sprintf("%s  (ID: %d)", a.Name, a.ID)
		}
	case "filearea":
		if m.recordEditIdx < len(m.configs.FileAreas) {
			a := m.configs.FileAreas[m.recordEditIdx]
			return fmt.Sprintf("%s  (ID: %d)", a.Name, a.ID)
		}
	case "conference":
		if m.recordEditIdx < len(m.configs.Conferences) {
			c := m.configs.Conferences[m.recordEditIdx]
			return fmt.Sprintf("%s  (ID: %d)", c.Name, c.ID)
		}
	case "door":
		keys := m.doorKeys()
		if m.recordEditIdx < len(keys) {
			return m.configs.Doors[keys[m.recordEditIdx]].Name
		}
	case "event":
		if m.recordEditIdx < len(m.configs.Events.Events) {
			return m.configs.Events.Events[m.recordEditIdx].Name
		}
	case "protocol":
		if m.recordEditIdx < len(m.configs.Protocols) {
			return m.configs.Protocols[m.recordEditIdx].Name
		}
	case "archiver":
		if m.recordEditIdx < len(m.configs.Archivers.Archivers) {
			return m.configs.Archivers.Archivers[m.recordEditIdx].Name
		}
	case "ftn":
		if m.recordEditIdx < 0 {
			return "Paths & Storage"
		}
		keys := m.ftnNetworkKeys()
		if m.recordEditIdx < len(keys) {
			return keys[m.recordEditIdx]
		}
	case "ftnlink":
		refs := m.ftnAllLinkRefs()
		if m.recordEditIdx >= 0 && m.recordEditIdx < len(refs) {
			ref := refs[m.recordEditIdx]
			nc := m.configs.FTN.Networks[ref.networkKey]
			if ref.linkIdx < len(nc.Links) {
				lnk := nc.Links[ref.linkIdx]
				if lnk.Name != "" {
					return fmt.Sprintf("%s  (%s)", lnk.Name, ref.networkKey)
				}
				return fmt.Sprintf("%s  (%s)", lnk.Address, ref.networkKey)
			}
		}
	case "login":
		if m.recordEditIdx < len(m.configs.LoginSeq) {
			return fmt.Sprintf("Step %d", m.recordEditIdx+1)
		}
	}
	return "Edit Record"
}

// renderRecordEditRow renders a single row of record edit fields.
func (m Model) renderRecordEditRow(row, boxW int) string {
	var fieldStr string

	for i, f := range m.recordFields {
		if f.Row != row {
			continue
		}
		fieldStr, _ = m.renderRecordField(i, f)
	}

	if fieldStr == "" {
		return fieldDisplayStyle.Render(strings.Repeat(" ", boxW))
	}

	padBefore := 2
	maxFieldW := boxW - padBefore
	// Truncate field content to fit within the box
	if lipgloss.Width(fieldStr) > maxFieldW {
		fieldStr = truncateToDisplayWidth(fieldStr, maxFieldW)
	}
	// Use actual visual width to avoid blow-out from multi-byte characters
	padAfter := boxW - padBefore - lipgloss.Width(fieldStr)
	if padAfter < 0 {
		padAfter = 0
	}

	return fieldDisplayStyle.Render(strings.Repeat(" ", padBefore)) +
		fieldStr +
		fieldDisplayStyle.Render(strings.Repeat(" ", padAfter))
}

// renderRecordField renders a single record field.
func (m Model) renderRecordField(fieldIdx int, f fieldDef) (string, int) {
	isActive := m.editField == fieldIdx

	labelText := padRight(f.Label, 16)
	label := labelText + " : "
	labelLen := len(label)

	var value string
	if f.Get != nil {
		value = f.Get()
	}

	rawW := labelLen + f.Width

	if isActive && m.mode == modeRecordField {
		return fieldLabelStyle.Render(label) + m.textInput.View(), rawW
	}

	// Mask password fields when not actively editing.
	displayVal := value
	if f.Masked {
		displayVal = maskValue(value)
	}
	displayValue := padRight(displayVal, f.Width)

	if isActive && m.mode == modeRecordEdit {
		// Ensure at least 1 fill character is always visible so the user
		// can see the field is highlighted/selected.
		effectiveWidth := f.Width
		if f.Type == ftYesNo || f.Type == ftInteger {
			effectiveWidth = f.Width + 2 // Add space for visual padding
		}

		// Truncate using display-width-aware method to handle multi-byte UTF-8 safely
		v := truncateToDisplayWidth(displayVal, effectiveWidth)

		// Calculate fill based on display width — guarantee at least 1 fill char
		vWidth := lipgloss.Width(v)
		fillCount := maxInt(1, effectiveWidth-vWidth)
		fillStr := strings.Repeat(string(fieldFillChar), fillCount)

		return fieldLabelStyle.Render(label) + fieldEditStyle.Render(v+fillStr), rawW
	}

	if f.Type == ftDisplay {
		return fieldLabelStyle.Render(label) + editInfoValueStyle.Render(displayValue), rawW
	}

	return fieldLabelStyle.Render(label) + fieldDisplayStyle.Render(displayValue), rawW
}

// renderFieldHelpLine returns the message/help line below the bottom border,
// sourcing its background fill from m.backdrop. Priority: flash message >
// active field help text > blank fill. row is the absolute screen row this
// line occupies.
func (m Model) renderFieldHelpLine(fields []fieldDef, padL, padR, boxW, row int) string {
	// Renders the help area: the active field's help (one line, or two when it
	// is long enough to wrap — #274), or a flash message, followed by one blank
	// separator row before the footer. The separator is always present, so the
	// gap below the help is preserved whether or not the help wrapped. Callers
	// budget the row count with fieldHelpRegionRows and advance row by it.
	helpLine := func(text string, r int) string {
		return m.backdrop.Segment(r, 0, padL) +
			editInfoLabelStyle.Render(centerText(text, boxW+1)) +
			m.backdrop.Segment(r, m.width-(padR+1), padR+1)
	}

	var rows []string
	switch {
	case m.message != "":
		rows = append(rows, m.backdrop.Segment(row, 0, padL)+
			flashMessageStyle.Render(" "+padRight(m.message, boxW))+
			m.backdrop.Segment(row, m.width-(padR+1), padR+1))
	case m.activeFieldHelp(fields) != "":
		line1, line2 := wrapHelpTwoLines(m.activeFieldHelp(fields), boxW+1)
		rows = append(rows, helpLine(line1, row))
		if line2 != "" {
			rows = append(rows, helpLine(line2, row+len(rows)))
		}
	default:
		rows = append(rows, m.backdrop.Line(row))
	}
	// Trailing blank separator between the help and the footer.
	rows = append(rows, m.backdrop.Line(row+len(rows)))
	return strings.Join(rows, "\n")
}

// activeFieldHelp returns the help text for the field being edited, with its
// interaction hint appended, or "" when there is no active help. Shared by the
// renderer and the row-count budget so the two agree on when help wraps.
func (m Model) activeFieldHelp(fields []fieldDef) string {
	if m.editField < 0 || m.editField >= len(fields) || fields[m.editField].Help == "" {
		return ""
	}
	help := fields[m.editField].Help
	switch fields[m.editField].Type {
	case ftYesNo:
		help += " (Space toggles)"
	case ftLookup:
		help += " (Enter to select)"
	}
	return help
}

// fieldHelpRegionRows is how many screen rows renderFieldHelpLine occupies for
// the given state: the help/message content (one row, or two when the active
// help wraps) plus one blank separator row. Views use it to keep the footer
// position and the vertical padding math exact.
func (m Model) fieldHelpRegionRows(fields []fieldDef, boxW int) int {
	content := 1
	if m.message == "" {
		if help := m.activeFieldHelp(fields); help != "" {
			if _, line2 := wrapHelpTwoLines(help, boxW+1); line2 != "" {
				content = 2
			}
		}
	}
	return content + 1 // + separator
}

// wrapHelpTwoLines splits help text to fit a field-help area that is at most
// two lines of the given display width. It breaks on spaces where it can; the
// second line is truncated with an ellipsis only if the text is too long even
// for two lines. Returns the whole text as line1 (line2 empty) when it fits.
// All measurement and truncation is by display width, so wide (e.g. CJK)
// glyphs cannot push a line past the box edge.
func wrapHelpTwoLines(s string, width int) (string, string) {
	if lipgloss.Width(s) <= width {
		return s, ""
	}
	words := strings.Fields(s)
	var line1 string
	i := 0
	for ; i < len(words); i++ {
		cand := words[i]
		if line1 != "" {
			cand = line1 + " " + words[i]
		}
		if lipgloss.Width(cand) > width {
			break
		}
		line1 = cand
	}
	if line1 == "" {
		// A single word wider than the line — hard-cut it by display width.
		return truncateWithEllipsis(s, width), ""
	}
	line2 := strings.Join(words[i:], " ")
	if lipgloss.Width(line2) > width {
		line2 = truncateWithEllipsis(line2, width)
	}
	return line1, line2
}

// truncateWithEllipsis cuts s to at most width display cells, reserving room
// for a one-cell ellipsis. Rune-based truncation would overshoot width when s
// contains wide glyphs, so measure in display cells throughout.
func truncateWithEllipsis(s string, width int) string {
	if lipgloss.Width(s) <= width {
		return s
	}
	if width <= 1 {
		return truncateToDisplayWidth(s, width)
	}
	return truncateToDisplayWidth(s, width-1) + "…"
}
