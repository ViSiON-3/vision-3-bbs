package configeditor

import (
	"fmt"
	"strings"
)

// viewV3NetAreaBrowser renders the area browser screen.
func (m Model) viewV3NetAreaBrowser() string {
	var b strings.Builder
	b.WriteString(m.globalHeaderLine())
	b.WriteByte('\n')

	bgLine := bgFillStyle.Render(strings.Repeat("░", m.width))
	boxW := 70
	listVisible := areaBrowserListVisible
	total := len(m.areaBrowserAreas)

	// Fixed rows: header(1) + border(1) + title(1) + colheader(1) + sep(1)
	//           + list(10) + border(1) + msg(1) + bgLine(1) + help(1)
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
	title := fmt.Sprintf("Area Browser — %s", m.areaBrowserNetwork)
	b.WriteString(border(menuBorderStyle.Render("│") +
		menuHeaderStyle.Render(centerText(title, boxW)) +
		menuBorderStyle.Render("│")))
	b.WriteByte('\n')

	// Handle special states.
	if m.areaBrowserLoading {
		b.WriteString(border(menuBorderStyle.Render("│") +
			menuItemStyle.Render(centerText("Fetching areas...", boxW)) +
			menuBorderStyle.Render("│")))
		b.WriteByte('\n')
		for i := 0; i < listVisible+1; i++ {
			b.WriteString(border(menuBorderStyle.Render("│") +
				menuItemStyle.Render(strings.Repeat(" ", boxW)) +
				menuBorderStyle.Render("│")))
			b.WriteByte('\n')
		}
		b.WriteString(border(menuBorderStyle.Render("└" + strings.Repeat("─", boxW) + "┘")))
		b.WriteByte('\n')
		for i := 0; i < bottomPad+1; i++ {
			b.WriteString(bgLine)
			b.WriteByte('\n')
		}
		b.WriteString(bgLine)
		b.WriteByte('\n')
		b.WriteString(bgLine)
		b.WriteByte('\n')
		b.WriteString(helpBarStyle.Render(centerText("ESC - Cancel", m.width)))
		return b.String()
	}

	if m.areaBrowserError != "" && total == 0 {
		errText := " " + m.areaBrowserError
		if len([]rune(errText)) > boxW {
			errText = string([]rune(errText)[:boxW-3]) + "..."
		}
		b.WriteString(border(menuBorderStyle.Render("│") +
			flashMessageStyle.Render(padRight(errText, boxW)) +
			menuBorderStyle.Render("│")))
		b.WriteByte('\n')
		for i := 0; i < listVisible+1; i++ {
			b.WriteString(border(menuBorderStyle.Render("│") +
				menuItemStyle.Render(strings.Repeat(" ", boxW)) +
				menuBorderStyle.Render("│")))
			b.WriteByte('\n')
		}
		b.WriteString(border(menuBorderStyle.Render("└" + strings.Repeat("─", boxW) + "┘")))
		b.WriteByte('\n')
		for i := 0; i < bottomPad+1; i++ {
			b.WriteString(bgLine)
			b.WriteByte('\n')
		}
		b.WriteString(bgLine)
		b.WriteByte('\n')
		b.WriteString(bgLine)
		b.WriteByte('\n')
		helpStr := "R - Retry  |  ESC - Back"
		b.WriteString(helpBarStyle.Render(centerText(helpStr, m.width)))
		return b.String()
	}

	// Column header.
	colHeader := fmt.Sprintf("   %-4s %-16s %-16s %-8s %s", " ", "Tag", "Name", "Status", "Local Board")
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
		visIdx := m.areaBrowserScroll + row
		var content string

		if visIdx >= 0 && visIdx < total {
			a := m.areaBrowserAreas[visIdx]
			check := "[ ]"
			if a.Subscribed {
				check = "[x]"
			}
			tag := padRight(a.Tag, 16)
			if len(tag) > 16 {
				tag = tag[:16]
			}
			name := padRight(a.Name, 16)
			if len(name) > 16 {
				name = name[:16]
			}
			status := padRight(a.Status, 8)
			localBoard := a.LocalBoard
			maxBoard := boxW - 4 - 16 - 16 - 8 - 5
			if len(localBoard) > maxBoard {
				localBoard = localBoard[:maxBoard]
			}
			content = fmt.Sprintf("   %s %-16s %-16s %-8s %s",
				check, tag, name, status, localBoard)
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
		if visIdx == m.areaBrowserCursor {
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

	// Help bar.
	helpStr := "Space - Subscribe/Unsubscribe  |  ESC - Done"
	b.WriteString(helpBarStyle.Render(centerText(helpStr, m.width)))

	return b.String()
}

