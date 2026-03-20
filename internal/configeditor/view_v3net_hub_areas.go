package configeditor

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// viewV3NetHubAreas renders the hub area management list.
func (m Model) viewV3NetHubAreas() string {
	var b strings.Builder
	b.WriteString(m.globalHeaderLine())
	b.WriteByte('\n')

	bgLine := bgFillStyle.Render(strings.Repeat("░", m.width))
	boxW := 70
	listVisible := 10
	indices := m.hubNetworkAreaIndices()
	total := len(indices)

	// Fixed rows: header(1) + border(1) + title(1) + colheader(1) + sep(1) + list + border(1) + msg(1) + bgLine(1) + help(1)
	fixedRows := listVisible + 9
	extraV := maxInt(0, m.height-fixedRows)
	topPad := extraV / 2
	bottomPad := extraV - topPad

	for i := 0; i < topPad; i++ {
		b.WriteString(bgLine)
		b.WriteByte('\n')
	}

	padL := maxInt(0, (m.width-boxW-2)/2)
	padR := maxInt(0, m.width-padL-boxW-2)

	border := func(s string) string {
		return bgFillStyle.Render(strings.Repeat("░", padL)) + s +
			bgFillStyle.Render(strings.Repeat("░", maxInt(0, padR)))
	}

	// Top border.
	b.WriteString(border(menuBorderStyle.Render("┌" + strings.Repeat("─", boxW) + "┐")))
	b.WriteByte('\n')

	// Title.
	title := fmt.Sprintf("Network Areas — %s", m.hubAreaNetwork)
	b.WriteString(border(menuBorderStyle.Render("│") +
		menuHeaderStyle.Render(centerText(title, boxW)) +
		menuBorderStyle.Render("│")))
	b.WriteByte('\n')

	// Column header.
	colHeader := fmt.Sprintf("   %-20s %-24s %s", "Tag", "Name", "Base Path")
	b.WriteString(border(menuBorderStyle.Render("│") +
		menuHeaderStyle.Render(padRight(colHeader, boxW)) +
		menuBorderStyle.Render("│")))
	b.WriteByte('\n')

	// Separator.
	b.WriteString(border(menuBorderStyle.Render("│") +
		separatorStyle.Render(strings.Repeat("─", boxW)) +
		menuBorderStyle.Render("│")))
	b.WriteByte('\n')

	// List rows.
	for row := 0; row < listVisible; row++ {
		visIdx := m.hubAreaScroll + row
		var content string

		if visIdx >= 0 && visIdx < total {
			areaIdx := indices[visIdx]
			a := m.configs.MsgAreas[areaIdx]
			tag := a.EchoTag
			if tag == "" {
				tag = a.Tag
			}
			content = fmt.Sprintf("   %-20s %-24s %s",
				padRight(tag, 20), padRight(a.Name, 24), a.BasePath)
		}

		if content == "" {
			content = strings.Repeat(" ", boxW)
		}
		if len(content) < boxW {
			content += strings.Repeat(" ", boxW-len(content))
		} else if len(content) > boxW {
			content = content[:boxW]
		}

		var styled string
		if visIdx == m.hubAreaCursor {
			styled = menuHighlightStyle.Render(content)
		} else {
			styled = menuItemStyle.Render(content)
		}

		b.WriteString(border(menuBorderStyle.Render("│") + styled + menuBorderStyle.Render("│")))
		b.WriteByte('\n')
	}

	// Bottom border.
	b.WriteString(border(menuBorderStyle.Render("└" + strings.Repeat("─", boxW) + "┘")))
	b.WriteByte('\n')

	for i := 0; i < bottomPad; i++ {
		b.WriteString(bgLine)
		b.WriteByte('\n')
	}

	// Help row (message or blank).
	if m.message != "" {
		msgLine := bgFillStyle.Render(strings.Repeat("░", padL)) +
			flashMessageStyle.Render(" "+padRight(m.message, boxW)) +
			bgFillStyle.Render(strings.Repeat("░", maxInt(0, padR+1)))
		b.WriteString(msgLine)
	} else {
		b.WriteString(bgLine)
	}
	b.WriteByte('\n')

	b.WriteString(bgLine)
	b.WriteByte('\n')

	helpStr := "I - Insert  |  E - Edit  |  D - Delete  |  S - Save  |  ESC/Q - Return"
	if total == 0 {
		helpStr = "I - Insert  |  S - Save  |  ESC/Q - Return"
	}
	b.WriteString(helpBarStyle.Render(centerText(helpStr, m.width)))

	return b.String()
}

// areaFormField describes one field in the insert/edit area form.
type areaFormField struct {
	label string
	value string
	step  int
}

// viewV3NetAreaForm renders a centered form box used by both insert and edit views.
func (m Model) viewV3NetAreaForm(title string, fields []areaFormField, activeStep int) string {
	var b strings.Builder
	b.WriteString(m.globalHeaderLine())
	b.WriteByte('\n')

	bgLine := bgFillStyle.Render(strings.Repeat("░", m.width))
	boxW := 60
	boxH := 9 // top + title + blank + fields(4) + blank + bottom

	extraV := maxInt(0, m.height-boxH-4)
	topPad := extraV / 2
	bottomPad := extraV - topPad

	for i := 0; i < topPad; i++ {
		b.WriteString(bgLine)
		b.WriteByte('\n')
	}

	padL := maxInt(0, (m.width-boxW-2)/2)
	padR := maxInt(0, m.width-padL-boxW-2)
	border := func(s string) string {
		return bgFillStyle.Render(strings.Repeat("░", padL)) + s +
			bgFillStyle.Render(strings.Repeat("░", maxInt(0, padR)))
	}
	row := func(content string) string {
		return border(editBorderStyle.Render("│") +
			fieldDisplayStyle.Width(boxW).Render(content) +
			editBorderStyle.Render("│"))
	}

	b.WriteString(border(editBorderStyle.Render("┌" + strings.Repeat("─", boxW) + "┐")))
	b.WriteByte('\n')
	b.WriteString(border(editBorderStyle.Render("│") +
		menuHeaderStyle.Render(centerText(title, boxW)) +
		editBorderStyle.Render("│")))
	b.WriteByte('\n')
	b.WriteString(row(strings.Repeat(" ", boxW)))
	b.WriteByte('\n')

	for _, f := range fields {
		labelStr := fieldLabelStyle.Render(padRight(f.label, 12) + " : ")
		var valueStr string
		if f.step == activeStep {
			valueStr = m.textInput.View()
		} else {
			valueStr = fieldDisplayStyle.Render(padRight(f.value, 30))
		}
		content := labelStr + valueStr
		padBefore := 2
		cw := lipgloss.Width(content)
		padAfter := maxInt(0, boxW-padBefore-cw)
		rowStr := fieldDisplayStyle.Render(strings.Repeat(" ", padBefore)) +
			content +
			fieldDisplayStyle.Render(strings.Repeat(" ", padAfter))
		b.WriteString(border(editBorderStyle.Render("│") + rowStr + editBorderStyle.Render("│")))
		b.WriteByte('\n')
	}

	b.WriteString(row(strings.Repeat(" ", boxW)))
	b.WriteByte('\n')
	b.WriteString(border(editBorderStyle.Render("└" + strings.Repeat("─", boxW) + "┘")))
	b.WriteByte('\n')

	for i := 0; i < bottomPad; i++ {
		b.WriteString(bgLine)
		b.WriteByte('\n')
	}

	// Help row (message or blank).
	if m.message != "" {
		msgLine := bgFillStyle.Render(strings.Repeat("░", padL)) +
			flashMessageStyle.Render(" "+padRight(m.message, boxW)) +
			bgFillStyle.Render(strings.Repeat("░", maxInt(0, padR+1)))
		b.WriteString(msgLine)
	} else {
		b.WriteString(bgLine)
	}
	b.WriteByte('\n')

	b.WriteString(bgLine)
	b.WriteByte('\n')

	helpStr := "Enter - Next Field / Confirm  |  ESC - Cancel"
	b.WriteString(helpBarStyle.Render(centerText(helpStr, m.width)))

	return b.String()
}

// viewV3NetAreaInsert renders the area insert form.
func (m Model) viewV3NetAreaInsert() string {
	return m.viewV3NetAreaForm("Insert Area", []areaFormField{
		{"Tag", m.hubAreaInsertTag, 0},
		{"Name", m.hubAreaInsertName, 1},
		{"Description", m.hubAreaInsertDesc, 2},
		{"Local Path", m.hubAreaInsertBase, 3},
	}, m.hubAreaInsertStep)
}

// viewV3NetAreaRename renders the area edit form.
func (m Model) viewV3NetAreaRename() string {
	return m.viewV3NetAreaForm("Edit Area", []areaFormField{
		{"Tag", m.hubAreaEditTag, 0},
		{"Name", m.hubAreaEditName, 1},
		{"Description", m.hubAreaEditDesc, 2},
		{"Local Path", m.hubAreaEditBase, 3},
	}, m.hubAreaEditStep)
}
