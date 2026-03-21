package configeditor

import (
	"fmt"
	"strings"
	"unicode"
)

// regBrowserListVisible is the number of visible rows in the registry list.
const regBrowserListVisible = 10

// sanitizeRegistryField strips control characters from untrusted registry
// data to prevent ANSI/OSC escape injection in the TUI.
func sanitizeRegistryField(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
}

// viewRegistryBrowser renders the registry browser screen.
func (m Model) viewRegistryBrowser() string {
	var b strings.Builder
	b.WriteString(m.globalHeaderLine())
	b.WriteByte('\n')

	bgLine := bgFillStyle.Render(strings.Repeat("░", m.width))
	boxW := 70
	listVisible := regBrowserListVisible
	total := len(m.regBrowserEntries)

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
	title := "Network Registry"
	b.WriteString(border(menuBorderStyle.Render("│") +
		menuHeaderStyle.Render(centerText(title, boxW)) +
		menuBorderStyle.Render("│")))
	b.WriteByte('\n')

	// Handle special states.
	if m.regBrowserLoading {
		b.WriteString(border(menuBorderStyle.Render("│") +
			menuItemStyle.Render(centerText("Fetching registry...", boxW)) +
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
		for i := 0; i < bottomPad; i++ {
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

	if m.regBrowserError != "" && total == 0 {
		b.WriteString(border(menuBorderStyle.Render("│") +
			flashMessageStyle.Render(padRight(" "+m.regBrowserError, boxW)) +
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
		for i := 0; i < bottomPad; i++ {
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
	colHeader := fmt.Sprintf("  %-14s %-28s %s", "Network", "Description", "Hub URL")
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
		visIdx := m.regBrowserScroll + row
		var content string

		if visIdx >= 0 && visIdx < total {
			e := m.regBrowserEntries[visIdx]
			subscribed := m.isLeafSubscribed(e.Name)
			tag := "  "
			if subscribed {
				tag = "* "
			}
			name := padRight(sanitizeRegistryField(e.Name), 14)
			desc := padRight(sanitizeRegistryField(e.Description), 28)
			hubURL := sanitizeRegistryField(e.HubURL)
			maxURL := boxW - 14 - 28 - 6
			if runeLen := len([]rune(hubURL)); runeLen > maxURL {
				hubURL = string([]rune(hubURL)[:maxURL])
			}
			content = fmt.Sprintf("%s%-14s %-28s %s", tag, name, desc, hubURL)
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
		if visIdx == m.regBrowserCursor {
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
	helpStr := "Enter - Select  |  ESC - Back  |  * = subscribed"
	b.WriteString(helpBarStyle.Render(centerText(helpStr, m.width)))

	return b.String()
}
