package configeditor

import (
	"fmt"
	"strings"
)

// viewFTNAreaBrowser renders the FTN echo area browser screen.
func (m Model) viewFTNAreaBrowser() string {
	var b strings.Builder
	b.WriteString(m.globalHeaderLine())
	b.WriteByte('\n')

	bgLine := bgFillStyle.Render(strings.Repeat("░", m.width))
	boxW := 70
	total := len(m.ftnAreaBrowserAreas)

	fixedRows := ftnAreaBrowserListVisible + 9
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

	row := func(content string) string {
		return border(menuBorderStyle.Render("│") + content + menuBorderStyle.Render("│"))
	}

	netName := ""
	if m.ftnWizard != nil {
		netName = m.ftnWizard.networkName
	}

	// Top border.
	b.WriteString(border(menuBorderStyle.Render("┌" + strings.Repeat("─", boxW) + "┐")))
	b.WriteByte('\n')

	// Title.
	title := fmt.Sprintf("Echo Areas — %s", netName)
	b.WriteString(row(menuHeaderStyle.Render(centerText(title, boxW))))
	b.WriteByte('\n')

	// Handle loading state.
	if m.ftnAreaBrowserLoading {
		b.WriteString(row(menuItemStyle.Render(centerText("Downloading echolist...", boxW))))
		b.WriteByte('\n')
		for i := 0; i < ftnAreaBrowserListVisible+1; i++ {
			b.WriteString(row(menuItemStyle.Render(strings.Repeat(" ", boxW))))
			b.WriteByte('\n')
		}
		b.WriteString(border(menuBorderStyle.Render("└" + strings.Repeat("─", boxW) + "┘")))
		b.WriteByte('\n')
		for i := 0; i < bottomPad+2; i++ {
			b.WriteString(bgLine)
			b.WriteByte('\n')
		}
		b.WriteString(helpBarStyle.Render(centerText("ESC - Cancel", m.width)))
		return b.String()
	}

	// Handle error state.
	if m.ftnAreaBrowserError != "" && total == 0 {
		errText := " " + m.ftnAreaBrowserError
		if len([]rune(errText)) > boxW {
			errText = string([]rune(errText)[:boxW-3]) + "..."
		}
		b.WriteString(row(flashMessageStyle.Render(padRight(errText, boxW))))
		b.WriteByte('\n')
		for i := 0; i < ftnAreaBrowserListVisible+1; i++ {
			b.WriteString(row(menuItemStyle.Render(strings.Repeat(" ", boxW))))
			b.WriteByte('\n')
		}
		b.WriteString(border(menuBorderStyle.Render("└" + strings.Repeat("─", boxW) + "┘")))
		b.WriteByte('\n')
		for i := 0; i < bottomPad+2; i++ {
			b.WriteString(bgLine)
			b.WriteByte('\n')
		}
		b.WriteString(helpBarStyle.Render(centerText("R - Retry  |  ESC - Back", m.width)))
		return b.String()
	}

	// Column header.
	colHeader := fmt.Sprintf("   %-4s %-16s %s", " ", "Tag", "Description")
	b.WriteString(row(menuHeaderStyle.Render(padRight(colHeader, boxW))))
	b.WriteByte('\n')

	// Separator.
	b.WriteString(row(separatorStyle.Render(strings.Repeat("─", boxW))))
	b.WriteByte('\n')

	// List rows.
	for i := 0; i < ftnAreaBrowserListVisible; i++ {
		visIdx := m.ftnAreaBrowserScroll + i
		var content string

		if visIdx >= 0 && visIdx < total {
			a := m.ftnAreaBrowserAreas[visIdx]
			check := "[ ]"
			if m.ftnAreaBrowserSelected[visIdx] {
				check = "[x]"
			}
			tag := padRight(a.Tag, 16)
			if len(tag) > 16 {
				tag = tag[:16]
			}
			descW := boxW - 4 - 16 - 2
			desc := a.Description
			if len(desc) > descW {
				desc = desc[:descW-3] + "..."
			}
			content = fmt.Sprintf("   %s %-16s %s", check, tag, desc)
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
		if visIdx == m.ftnAreaBrowserCursor {
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

	// Selection count.
	selected := 0
	for _, sel := range m.ftnAreaBrowserSelected {
		if sel {
			selected++
		}
	}
	countMsg := fmt.Sprintf("%d of %d areas selected", selected, total)
	countLine := bgFillStyle.Render(strings.Repeat("░", padL)) +
		editInfoValueStyle.Render(centerText(countMsg, boxW+2)) +
		bgFillStyle.Render(strings.Repeat("░", maxInt(0, padR)))
	b.WriteString(countLine)
	b.WriteByte('\n')

	b.WriteString(bgLine)
	b.WriteByte('\n')

	helpStr := "Space - Toggle  |  A - All  |  N - None  |  Enter - Confirm  |  ESC - Back"
	b.WriteString(helpBarStyle.Render(centerText(helpStr, m.width)))

	return b.String()
}

// viewFTNAreaDownloading renders the downloading progress screen.
func (m Model) viewFTNAreaDownloading() string {
	// Delegate to the area browser view which handles the loading state.
	return m.viewFTNAreaBrowser()
}
