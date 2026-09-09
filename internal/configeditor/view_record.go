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
	b.WriteString(m.renderFieldHelpLine(m.recordFields, padL, padR, boxW, row, helpRegionRows))
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
func (m Model) renderFieldHelpLine(fields []fieldDef, padL, padR, boxW, row, region int) string {
	// Renders the help area: the active field's help (one line, or two when it
	// wraps — #274), or a flash message, padded with blank backdrop rows to the
	// fixed region height so the trailing blank(s) always sit above the footer.
	// The caller passes region (from fieldHelpRegionRows, used for its layout
	// math), which is sized to the tallest help in this field set — not the
	// active field — so moving between fields never shifts the box or footer.
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
	}
	// Pad to the fixed region height. The trailing blank(s) are the gap above
	// the footer; there is always at least one because region > max content.
	for len(rows) < region {
		rows = append(rows, m.backdrop.Line(row+len(rows)))
	}
	return strings.Join(rows, "\n")
}

// activeFieldHelp returns the help text for the field being edited, with its
// interaction hint appended, or "" when there is no active help.
func (m Model) activeFieldHelp(fields []fieldDef) string {
	if m.editField < 0 || m.editField >= len(fields) {
		return ""
	}
	return fieldHelpText(fields[m.editField])
}

// fieldHelpText is a field's help with its interaction hint appended.
func fieldHelpText(f fieldDef) string {
	if f.Help == "" {
		return ""
	}
	switch f.Type {
	case ftYesNo:
		return f.Help + " (Space toggles)"
	case ftLookup:
		return f.Help + " (Enter to select)"
	}
	return f.Help
}

// fieldHelpRegionRows is the fixed number of screen rows the help area occupies
// for this field set: the tallest field help (one line, or two when it wraps)
// plus one blank separator row. It depends only on the fields and box width —
// not on which field is active — so the footer and box stay put as the user
// moves between fields, and the blank gap above the footer is always present.
func (m Model) fieldHelpRegionRows(fields []fieldDef, boxW int) int {
	maxContent := 1
	for i := range fields {
		help := fieldHelpText(fields[i])
		if help == "" {
			continue
		}
		if _, line2 := wrapHelpTwoLines(help, boxW+1); line2 != "" {
			maxContent = 2
			break
		}
	}
	return maxContent + 1 // + separator
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
