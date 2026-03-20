package configeditor

import (
	"fmt"
	"strings"
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

	// Fixed rows: header(1) + border(1) + title(1) + colheader(1) + sep(1) + list + border(1) + msg(1) + help(1)
	fixedRows := listVisible + 8
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

	// Message line.
	if m.message != "" {
		b.WriteString(border(flashMessageStyle.Render(" " + padRight(m.message, boxW))))
	} else {
		b.WriteString(bgLine)
	}
	b.WriteByte('\n')

	for i := 0; i < bottomPad; i++ {
		b.WriteString(bgLine)
		b.WriteByte('\n')
	}

	helpStr := "D - Delete  |  R - Rename  |  S - Save  |  ESC/Q - Return"
	if total == 0 {
		helpStr = "(no areas)  |  S - Save  |  ESC/Q - Return"
	}
	b.WriteString(helpBarStyle.Render(centerText(helpStr, m.width)))

	return b.String()
}

// viewV3NetAreaRename renders the area rename form.
func (m Model) viewV3NetAreaRename() string {
	var b strings.Builder
	b.WriteString(m.globalHeaderLine())
	b.WriteByte('\n')

	bgLine := bgFillStyle.Render(strings.Repeat("░", m.width))
	boxW := 60
	boxH := 7 // top + title + blank + name + basepath + blank + bottom

	extraV := maxInt(0, m.height-boxH-3)
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
		menuHeaderStyle.Render(centerText("Rename Area", boxW)) +
		editBorderStyle.Render("│")))
	b.WriteByte('\n')
	b.WriteString(row(strings.Repeat(" ", boxW)))
	b.WriteByte('\n')

	fields := []struct {
		label string
		value string
		step  int
	}{
		{"Name", m.hubAreaNewName, 0},
		{"Base Path", m.hubAreaNewBase, 1},
	}

	for _, f := range fields {
		labelStr := fieldLabelStyle.Render(padRight(f.label, 12) + " : ")
		var valueStr string
		if f.step == m.hubAreaRenameStep {
			valueStr = m.textInput.View()
		} else {
			valueStr = fieldDisplayStyle.Render(padRight(f.value, 40))
		}
		content := "  " + labelStr + valueStr
		// Pad to box width.
		vis := approximateVisibleLen(content)
		if vis < boxW {
			content += strings.Repeat(" ", boxW-vis)
		}
		b.WriteString(border(editBorderStyle.Render("│") + content + editBorderStyle.Render("│")))
		b.WriteByte('\n')
	}

	b.WriteString(row(strings.Repeat(" ", boxW)))
	b.WriteByte('\n')
	b.WriteString(border(editBorderStyle.Render("└" + strings.Repeat("─", boxW) + "┘")))
	b.WriteByte('\n')

	// Message line.
	if m.message != "" {
		b.WriteString(border(flashMessageStyle.Render(" " + padRight(m.message, boxW))))
	} else {
		b.WriteString(bgLine)
	}
	b.WriteByte('\n')

	for i := 0; i < bottomPad; i++ {
		b.WriteString(bgLine)
		b.WriteByte('\n')
	}

	helpStr := "Enter - Next Field / Confirm  |  ESC - Cancel"
	b.WriteString(helpBarStyle.Render(centerText(helpStr, m.width)))

	return b.String()
}
