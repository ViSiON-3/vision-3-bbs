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

	// Global header
	b.WriteString(m.globalHeaderLine())
	b.WriteByte('\n')

	bgLine := bgFillStyle.Render(strings.Repeat("░", m.width))

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
		b.WriteString(bgLine)
		b.WriteByte('\n')
	}

	padL := maxInt(0, (m.width-boxW-2)/2)
	padR := maxInt(0, m.width-padL-boxW-2)

	// Top border
	b.WriteString(bgFillStyle.Render(strings.Repeat("░", padL)) +
		editBorderStyle.Render("┌"+strings.Repeat("─", boxW)+"┐") +
		bgFillStyle.Render(strings.Repeat("░", maxInt(0, padR))))
	b.WriteByte('\n')

	// Box title
	titleLine := editBorderStyle.Render("│") +
		menuHeaderStyle.Render(centerText(m.wizardTitle, boxW)) +
		editBorderStyle.Render("│")
	b.WriteString(bgFillStyle.Render(strings.Repeat("░", padL)) + titleLine +
		bgFillStyle.Render(strings.Repeat("░", maxInt(0, padR))))
	b.WriteByte('\n')

	// Empty line
	emptyLine := bgFillStyle.Render(strings.Repeat("░", padL)) +
		editBorderStyle.Render("│") +
		fieldDisplayStyle.Render(strings.Repeat(" ", boxW)) +
		editBorderStyle.Render("│") +
		bgFillStyle.Render(strings.Repeat("░", maxInt(0, padR)))
	b.WriteString(emptyLine)
	b.WriteByte('\n')

	// Field rows
	for row := 1; row <= maxRow; row++ {
		rowContent := m.renderWizardRow(row, boxW)
		line := bgFillStyle.Render(strings.Repeat("░", padL)) +
			editBorderStyle.Render("│") +
			rowContent +
			editBorderStyle.Render("│") +
			bgFillStyle.Render(strings.Repeat("░", maxInt(0, padR)))
		b.WriteString(line)
		b.WriteByte('\n')
	}

	// Empty line
	b.WriteString(emptyLine)
	b.WriteByte('\n')

	// Info line
	infoText := "S - Save  |  ESC - Cancel"
	infoLine := editBorderStyle.Render("│") +
		editInfoLabelStyle.Render(centerText(infoText, boxW)) +
		editBorderStyle.Render("│")
	b.WriteString(bgFillStyle.Render(strings.Repeat("░", padL)) + infoLine +
		bgFillStyle.Render(strings.Repeat("░", maxInt(0, padR))))
	b.WriteByte('\n')

	// Bottom border
	b.WriteString(bgFillStyle.Render(strings.Repeat("░", padL)) +
		editBorderStyle.Render("└"+strings.Repeat("─", boxW)+"┘") +
		bgFillStyle.Render(strings.Repeat("░", maxInt(0, padR))))
	b.WriteByte('\n')

	for i := 0; i < bottomPad; i++ {
		b.WriteString(bgLine)
		b.WriteByte('\n')
	}

	// Message or field help text
	b.WriteString(m.renderFieldHelpLine(m.wizardFields, padL, padR, boxW))
	b.WriteByte('\n')

	b.WriteString(bgLine)
	b.WriteByte('\n')

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

	displayValue := padRight(value, f.Width)

	if isActive && m.mode == modeWizardForm {
		effectiveWidth := f.Width
		if f.Type == ftYesNo || f.Type == ftInteger {
			effectiveWidth = f.Width + 2
		}

		v := truncateToDisplayWidth(value, effectiveWidth)
		vWidth := lipgloss.Width(v)
		fillStr := strings.Repeat(string(fieldFillChar), maxInt(0, effectiveWidth-vWidth))

		return fieldLabelStyle.Render(label) + fieldEditStyle.Render(v+fillStr)
	}

	if f.Type == ftDisplay {
		return fieldLabelStyle.Render(label) + editInfoValueStyle.Render(displayValue)
	}

	return fieldLabelStyle.Render(label) + fieldDisplayStyle.Render(displayValue)
}
