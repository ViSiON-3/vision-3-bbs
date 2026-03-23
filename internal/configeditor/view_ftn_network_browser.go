package configeditor

import (
	"fmt"
	"strings"
)

// viewFTNNetworkBrowser renders the FTN network browser screen.
func (m Model) viewFTNNetworkBrowser() string {
	var b strings.Builder
	b.WriteString(m.globalHeaderLine())
	b.WriteByte('\n')

	bgLine := bgFillStyle.Render(strings.Repeat("░", m.width))
	boxW := 70
	total := len(m.ftnNetBrowserEntries)

	// Fixed rows: header(1) + border(1) + title(1) + colheader(1) + sep(1)
	//           + list(12) + sep(1) + info(4) + border(1) + bgLine(1) + help(1)
	fixedRows := ftnNetBrowserListVisible + 13
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

	emptyRow := row(menuItemStyle.Render(strings.Repeat(" ", boxW)))

	// Top border.
	b.WriteString(border(menuBorderStyle.Render("┌" + strings.Repeat("─", boxW) + "┐")))
	b.WriteByte('\n')

	// Title.
	b.WriteString(row(menuHeaderStyle.Render(centerText("Known FTN Networks", boxW))))
	b.WriteByte('\n')

	// Column header.
	colHeader := fmt.Sprintf("  %5s  %-12s  %s", "Zone", "Network", "Description")
	b.WriteString(row(menuHeaderStyle.Render(padRight(colHeader, boxW))))
	b.WriteByte('\n')

	// Separator.
	b.WriteString(row(separatorStyle.Render(strings.Repeat("─", boxW))))
	b.WriteByte('\n')

	// List rows.
	for i := 0; i < ftnNetBrowserListVisible; i++ {
		visIdx := m.ftnNetBrowserScroll + i
		var content string

		if visIdx >= 0 && visIdx < total {
			net := m.ftnNetBrowserEntries[visIdx]
			zone := fmt.Sprintf("%5d", net.Zone)
			name := padRight(net.Name, 12)
			if len(name) > 12 {
				name = name[:12]
			}
			descW := boxW - 5 - 2 - 12 - 2 - 2
			desc := net.Description
			if len(desc) > descW {
				desc = desc[:descW-3] + "..."
			}

			// Mark already-configured networks.
			marker := " "
			if m.ftnNetworkExists(net.Name) {
				marker = "*"
			}

			content = fmt.Sprintf("%s %5s  %-12s  %s", marker, zone, name, desc)
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
		if visIdx == m.ftnNetBrowserCursor {
			styled = menuHighlightStyle.Render(content)
		} else {
			styled = menuItemStyle.Render(content)
		}

		b.WriteString(border(menuBorderStyle.Render("│") + styled + menuBorderStyle.Render("│")))
		b.WriteByte('\n')
	}

	// Separator before info panel.
	b.WriteString(row(separatorStyle.Render(strings.Repeat("─", boxW))))
	b.WriteByte('\n')

	// Info panel for highlighted network.
	if total > 0 && m.ftnNetBrowserCursor < total {
		net := m.ftnNetBrowserEntries[m.ftnNetBrowserCursor]

		info1 := fmt.Sprintf("  %s — %s", net.Name, net.Description)
		b.WriteString(row(editInfoValueStyle.Render(padRight(info1, boxW))))
		b.WriteByte('\n')

		coordStr := "  Coordinator: " + net.Coordinator
		if net.CoordinatorEmail != "" {
			coordStr += " <" + net.CoordinatorEmail + ">"
		}
		b.WriteString(row(editInfoValueStyle.Render(padRight(coordStr, boxW))))
		b.WriteByte('\n')

		hubStr := fmt.Sprintf("  Hub: %s at %s", net.HubAddress, net.HubHostname)
		if net.HubPort > 0 {
			hubStr += fmt.Sprintf(":%d", net.HubPort)
		}
		b.WriteString(row(editInfoValueStyle.Render(padRight(hubStr, boxW))))
		b.WriteByte('\n')

		infoStr := "  Info: " + net.InfoURL
		b.WriteString(row(editInfoValueStyle.Render(padRight(infoStr, boxW))))
		b.WriteByte('\n')
	} else {
		for i := 0; i < 4; i++ {
			b.WriteString(emptyRow)
			b.WriteByte('\n')
		}
	}

	// Bottom border.
	b.WriteString(border(menuBorderStyle.Render("└" + strings.Repeat("─", boxW) + "┘")))
	b.WriteByte('\n')

	for i := 0; i < bottomPad; i++ {
		b.WriteString(bgLine)
		b.WriteByte('\n')
	}

	b.WriteString(bgLine)
	b.WriteByte('\n')

	helpStr := "Enter - Select  |  C - Custom Network  |  ESC - Back"
	b.WriteString(helpBarStyle.Render(centerText(helpStr, m.width)))

	return b.String()
}

// ftnNetworkExists checks if a network with the given name already exists in ftn.json.
func (m Model) ftnNetworkExists(name string) bool {
	if m.configs == nil || m.configs.FTN.Networks == nil {
		return false
	}
	lower := strings.ToLower(name)
	for k := range m.configs.FTN.Networks {
		if strings.ToLower(k) == lower {
			return true
		}
	}
	return false
}
