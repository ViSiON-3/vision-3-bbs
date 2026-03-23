package configeditor

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// viewFTNWizardForm renders the FTN setup wizard form.
func (m Model) viewFTNWizardForm() string {
	var b strings.Builder

	b.WriteString(m.globalHeaderLine())
	b.WriteByte('\n')

	bgLine := bgFillStyle.Render(strings.Repeat("░", m.width))
	boxW := 70

	// Find max row in fields.
	maxRow := 0
	for _, f := range m.ftnWizardFields {
		if f.Row > maxRow {
			maxRow = f.Row
		}
	}
	visibleRows := maxRow
	if visibleRows > maxFieldRows {
		visibleRows = maxFieldRows
	}
	extraV := maxInt(0, m.height-visibleRows-10)
	topPad := extraV / 2
	bottomPad := extraV - topPad

	for i := 0; i < topPad; i++ {
		b.WriteString(bgLine)
		b.WriteByte('\n')
	}

	padL := maxInt(0, (m.width-boxW-2)/2)
	padR := maxInt(0, m.width-padL-boxW-2)

	// Top border.
	b.WriteString(bgFillStyle.Render(strings.Repeat("░", padL)) +
		editBorderStyle.Render("┌"+strings.Repeat("─", boxW)+"┐") +
		bgFillStyle.Render(strings.Repeat("░", maxInt(0, padR))))
	b.WriteByte('\n')

	// Title.
	titleLine := editBorderStyle.Render("│") +
		menuHeaderStyle.Render(centerText("FTN Setup Wizard", boxW)) +
		editBorderStyle.Render("│")
	b.WriteString(bgFillStyle.Render(strings.Repeat("░", padL)) + titleLine +
		bgFillStyle.Render(strings.Repeat("░", maxInt(0, padR))))
	b.WriteByte('\n')

	// Empty line.
	emptyLine := bgFillStyle.Render(strings.Repeat("░", padL)) +
		editBorderStyle.Render("│") +
		fieldDisplayStyle.Render(strings.Repeat(" ", boxW)) +
		editBorderStyle.Render("│") +
		bgFillStyle.Render(strings.Repeat("░", maxInt(0, padR)))
	b.WriteString(emptyLine)
	b.WriteByte('\n')

	// Field rows (windowed by fieldScroll).
	firstRow := m.fieldScroll + 1
	lastRow := m.fieldScroll + visibleRows
	if lastRow > maxRow {
		lastRow = maxRow
	}
	for row := firstRow; row <= lastRow; row++ {
		rowContent := m.renderFTNWizardRow(row, boxW)
		line := bgFillStyle.Render(strings.Repeat("░", padL)) +
			editBorderStyle.Render("│") +
			rowContent +
			editBorderStyle.Render("│") +
			bgFillStyle.Render(strings.Repeat("░", maxInt(0, padR)))
		b.WriteString(line)
		b.WriteByte('\n')
	}
	// Pad remaining rows if fewer fields than visibleRows.
	for row := lastRow + 1; row <= m.fieldScroll+visibleRows; row++ {
		b.WriteString(emptyLine)
		b.WriteByte('\n')
	}

	// Empty line.
	b.WriteString(emptyLine)
	b.WriteByte('\n')

	// Info line.
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
	infoText := "S - Save & Apply  |  ESC - Cancel" + scrollHint
	infoLine := editBorderStyle.Render("│") +
		editInfoLabelStyle.Render(centerText(infoText, boxW)) +
		editBorderStyle.Render("│")
	b.WriteString(bgFillStyle.Render(strings.Repeat("░", padL)) + infoLine +
		bgFillStyle.Render(strings.Repeat("░", maxInt(0, padR))))
	b.WriteByte('\n')

	// Bottom border.
	b.WriteString(bgFillStyle.Render(strings.Repeat("░", padL)) +
		editBorderStyle.Render("└"+strings.Repeat("─", boxW)+"┘") +
		bgFillStyle.Render(strings.Repeat("░", maxInt(0, padR))))
	b.WriteByte('\n')

	for i := 0; i < bottomPad; i++ {
		b.WriteString(bgLine)
		b.WriteByte('\n')
	}

	// Message or field help text.
	b.WriteString(m.renderFieldHelpLine(m.ftnWizardFields, padL, padR, boxW))
	b.WriteByte('\n')

	b.WriteString(bgLine)
	b.WriteByte('\n')

	helpBarStr := "Enter - Edit  |  S - Save  |  ESC - Back"
	helpText := centerText(helpBarStr, m.width)
	b.WriteString(helpBarStyle.Render(helpText))

	return b.String()
}

// renderFTNWizardRow renders a single row of FTN wizard fields.
func (m Model) renderFTNWizardRow(row, boxW int) string {
	var parts []string

	for i, f := range m.ftnWizardFields {
		if f.Row != row {
			continue
		}
		parts = append(parts, m.renderFTNWizardField(i, f))
	}

	fieldStr := strings.Join(parts, "  ")

	if fieldStr == "" {
		return fieldDisplayStyle.Render(strings.Repeat(" ", boxW))
	}

	padBefore := 2
	maxFieldW := boxW - padBefore
	if lipgloss.Width(fieldStr) > maxFieldW {
		fieldStr = truncateToDisplayWidth(fieldStr, maxFieldW)
	}
	padAfter := boxW - padBefore - lipgloss.Width(fieldStr)
	if padAfter < 0 {
		padAfter = 0
	}

	return fieldDisplayStyle.Render(strings.Repeat(" ", padBefore)) +
		fieldStr +
		fieldDisplayStyle.Render(strings.Repeat(" ", padAfter))
}

// renderFTNWizardField renders a single FTN wizard field (label : value).
func (m Model) renderFTNWizardField(fieldIdx int, f fieldDef) string {
	isActive := m.editField == fieldIdx

	labelText := padRight(f.Label, 16)
	label := labelText + " : "

	var value string
	if f.Get != nil {
		value = f.Get()
	}

	if isActive && m.mode == modeFTNWizardField {
		return fieldLabelStyle.Render(label) + m.textInput.View()
	}

	// Mask password fields when not actively editing.
	displayVal := value
	if f.Masked {
		displayVal = maskValue(value)
	}
	displayValue := padRight(displayVal, f.Width)

	if isActive && m.mode == modeFTNWizardForm {
		effectiveWidth := f.Width
		if f.Type == ftYesNo || f.Type == ftInteger {
			effectiveWidth = f.Width + 2
		}

		v := truncateToDisplayWidth(displayVal, effectiveWidth)
		vWidth := lipgloss.Width(v)
		fillStr := strings.Repeat(string(fieldFillChar), maxInt(0, effectiveWidth-vWidth))

		return fieldLabelStyle.Render(label) + fieldEditStyle.Render(v+fillStr)
	}

	if f.Type == ftDisplay {
		return fieldLabelStyle.Render(label) + editInfoValueStyle.Render(displayValue)
	}

	return fieldLabelStyle.Render(label) + fieldDisplayStyle.Render(displayValue)
}
