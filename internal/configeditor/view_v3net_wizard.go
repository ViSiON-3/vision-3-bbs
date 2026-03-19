package configeditor

import (
	"fmt"
	"strings"
)

// viewV3NetWizard renders the hub areas sub-form (the only remaining
// modeV3NetWizardStep usage).
func (m Model) viewV3NetWizard() string {
	return m.viewHubAreasStep()
}

func (m Model) viewHubAreasStep() string {
	var b strings.Builder
	b.WriteString(m.globalHeaderLine())
	b.WriteByte('\n')

	bgLine := bgFillStyle.Render(strings.Repeat("░", m.width))
	boxW := 60

	// Content rows inside the box (between title and bottom border).
	var contentRows int
	if m.wizard.areaAdding {
		contentRows = 2 // prompt + input
	} else if len(m.wizard.areas) == 0 {
		contentRows = 1 // "(no areas yet …)"
	} else {
		contentRows = len(m.wizard.areas)
	}
	// Box: top border(1) + title(1) + blank(1) + content + blank(1) + bottom border(1)
	boxH := contentRows + 5
	// -4: header(1) + help line(1) + bgLine spacer(1) + help bar(1)
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
		menuHeaderStyle.Render(centerText("Initial Message Areas", boxW)) +
		editBorderStyle.Render("│")))
	b.WriteByte('\n')
	b.WriteString(border(editBorderStyle.Render("│") +
		editInfoLabelStyle.Render(strings.Repeat(" ", boxW)) +
		editBorderStyle.Render("│")))
	b.WriteByte('\n')

	if m.wizard.areaAdding {
		if m.wizard.areaEditTag == "" {
			b.WriteString(row("  Tag (e.g. net.general):"))
		} else {
			b.WriteString(row(fmt.Sprintf("  Tag: %s  Name:", m.wizard.areaEditTag)))
		}
		b.WriteByte('\n')
		b.WriteString(border(editBorderStyle.Render("│") +
			fieldDisplayStyle.Width(boxW).Render("  > "+m.textInput.View()) +
			editBorderStyle.Render("│")))
		b.WriteByte('\n')
	} else {
		if len(m.wizard.areas) == 0 {
			b.WriteString(row("  (no areas yet — press A to add)"))
			b.WriteByte('\n')
		}
		for i, a := range m.wizard.areas {
			cursor := "  "
			if i == m.wizard.areaCursor {
				cursor = "> "
			}
			b.WriteString(row(fmt.Sprintf("  %s%-20s %s", cursor, a.Tag, a.Name)))
			b.WriteByte('\n')
		}
	}

	b.WriteString(border(editBorderStyle.Render("│") +
		editInfoLabelStyle.Render(strings.Repeat(" ", boxW)) +
		editBorderStyle.Render("│")))
	b.WriteByte('\n')
	b.WriteString(border(editBorderStyle.Render("└" + strings.Repeat("─", boxW) + "┘")))
	b.WriteByte('\n')

	for i := 0; i < bottomPad; i++ {
		b.WriteString(bgLine)
		b.WriteByte('\n')
	}

	helpText := "A Add area  |  D Delete  |  Enter Done  |  ESC Back"
	if m.wizard.areaAdding {
		helpText = "Enter Confirm  |  ESC Cancel"
	}
	if m.message != "" {
		b.WriteString(bgFillStyle.Render(strings.Repeat("░", padL)) +
			flashMessageStyle.Render(" "+padRight(m.message, boxW)) +
			bgFillStyle.Render(strings.Repeat("░", maxInt(0, padR+1))))
	} else {
		b.WriteString(bgFillStyle.Render(strings.Repeat("░", padL)) +
			editInfoLabelStyle.Render(centerText(helpText, boxW+1)) +
			bgFillStyle.Render(strings.Repeat("░", maxInt(0, padR+1))))
	}
	b.WriteByte('\n')
	b.WriteString(bgLine)
	b.WriteByte('\n')
	b.WriteString(helpBarStyle.Render(centerText("A Add  D Delete  Enter Done  ESC Back", m.width)))
	return b.String()
}
