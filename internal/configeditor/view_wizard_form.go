package configeditor

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// viewWizardForm renders the wizard form with all fields visible.
func (m Model) viewWizardForm() string {
	if m.showSeedInterstitial {
		return m.viewSeedInterstitial()
	}

	var b strings.Builder

	row := 0

	// Global header
	b.WriteString(m.globalHeaderLine())
	b.WriteByte('\n')
	row++

	boxW := 70
	// Find max row in fields
	maxRow := 0
	for _, f := range m.wizardFields {
		if f.Row > maxRow {
			maxRow = f.Row
		}
	}
	visibleRows := maxRow
	// Fixed rows: globalheader(1) + box(border+title+empty+rows+empty+info+border = rows+6) + helptxt(1) + bgline(1) + helpbar(1)
	// Total fixed = rows + 10
	extraV := maxInt(0, m.height-visibleRows-10)
	topPad := extraV / 2
	bottomPad := extraV - topPad

	for i := 0; i < topPad; i++ {
		b.WriteString(m.backdrop.line(row))
		b.WriteByte('\n')
		row++
	}

	padL := maxInt(0, (m.width-boxW-2)/2)
	padR := maxInt(0, m.width-padL-boxW-2)

	// Top border
	b.WriteString(m.backdrop.segment(row, 0, padL) +
		editBorderStyle.Render("┌"+strings.Repeat("─", boxW)+"┐") +
		m.backdrop.segment(row, m.width-maxInt(0, padR), maxInt(0, padR)))
	b.WriteByte('\n')
	row++

	// Box title
	titleLine := editBorderStyle.Render("│") +
		menuHeaderStyle.Render(centerText(m.wizardTitle, boxW)) +
		editBorderStyle.Render("│")
	b.WriteString(m.backdrop.segment(row, 0, padL) + titleLine +
		m.backdrop.segment(row, m.width-maxInt(0, padR), maxInt(0, padR)))
	b.WriteByte('\n')
	row++

	// emptyLine renders a blank field-row line at the current row; it is used
	// at multiple, differently-numbered rows below, so it must be recomputed
	// each time rather than cached in a variable.
	emptyLine := func() string {
		return m.backdrop.segment(row, 0, padL) +
			editBorderStyle.Render("│") +
			fieldDisplayStyle.Render(strings.Repeat(" ", boxW)) +
			editBorderStyle.Render("│") +
			m.backdrop.segment(row, m.width-maxInt(0, padR), maxInt(0, padR))
	}

	// Empty line
	b.WriteString(emptyLine())
	b.WriteByte('\n')
	row++

	// Field rows
	for fr := 1; fr <= maxRow; fr++ {
		rowContent := m.renderWizardRow(fr, boxW)
		line := m.backdrop.segment(row, 0, padL) +
			editBorderStyle.Render("│") +
			rowContent +
			editBorderStyle.Render("│") +
			m.backdrop.segment(row, m.width-maxInt(0, padR), maxInt(0, padR))
		b.WriteString(line)
		b.WriteByte('\n')
		row++
	}

	// Empty line
	b.WriteString(emptyLine())
	b.WriteByte('\n')
	row++

	// Info line
	infoText := "S - Save  |  ESC - Cancel"
	infoLine := editBorderStyle.Render("│") +
		editInfoLabelStyle.Render(centerText(infoText, boxW)) +
		editBorderStyle.Render("│")
	b.WriteString(m.backdrop.segment(row, 0, padL) + infoLine +
		m.backdrop.segment(row, m.width-maxInt(0, padR), maxInt(0, padR)))
	b.WriteByte('\n')
	row++

	// Bottom border
	b.WriteString(m.backdrop.segment(row, 0, padL) +
		editBorderStyle.Render("└"+strings.Repeat("─", boxW)+"┘") +
		m.backdrop.segment(row, m.width-maxInt(0, padR), maxInt(0, padR)))
	b.WriteByte('\n')
	row++

	for i := 0; i < bottomPad; i++ {
		b.WriteString(m.backdrop.line(row))
		b.WriteByte('\n')
		row++
	}

	// Message or field help text
	b.WriteString(m.renderFieldHelpLine(m.wizardFields, padL, padR, boxW, row))
	b.WriteByte('\n')
	row++

	b.WriteString(m.backdrop.line(row))
	b.WriteByte('\n')
	row++

	helpBarStr := "Enter - Edit  |  S - Save  |  ESC - Back"
	helpText := centerText(helpBarStr, m.width)
	b.WriteString(helpBarStyle.Render(helpText))

	return b.String()
}

// renderWizardRow renders a single row of wizard form fields.
func (m Model) renderWizardRow(row, boxW int) string {
	var parts []string

	for i, f := range m.wizardFields {
		if f.Row != row {
			continue
		}
		parts = append(parts, m.renderWizardField(i, f))
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

// renderWizardField renders a single wizard field (label : value).
func (m Model) renderWizardField(fieldIdx int, f fieldDef) string {
	isActive := m.editField == fieldIdx

	labelText := padRight(f.Label, 16)
	label := labelText + " : "

	var value string
	if f.Get != nil {
		value = f.Get()
	}

	if isActive && m.mode == modeWizardField {
		return fieldLabelStyle.Render(label) + m.textInput.View()
	}

	// Mask password fields when not actively editing.
	displayVal := value
	if f.Masked {
		displayVal = maskValue(value)
	}
	displayValue := padRight(displayVal, f.Width)

	if isActive && m.mode == modeWizardForm {
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
