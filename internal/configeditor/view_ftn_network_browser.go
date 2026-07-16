package configeditor

import (
	"fmt"
	"strings"
)

// viewFTNNetworkBrowser renders the FTN network browser screen.
func (m Model) viewFTNNetworkBrowser() string {
	boxW := 70
	total := len(m.ftnNetBrowserEntries)

	// Fixed rows: header(1) + border(1) + title(1) + colheader(1) + sep(1)
	//           + list(12) + sep(1) + info(4) + border(1) + bgLine(1) + help(1)
	lb := m.newListBox(boxW, ftnNetBrowserListVisible+13)

	lb.topBorder()
	lb.title("Known FTN Networks")
	lb.colHeader(fmt.Sprintf("  %5s  %-12s  %s", "Zone", "Network", "Description"))
	lb.separator()

	lb.list(ftnNetBrowserListVisible, m.ftnNetBrowserScroll, m.ftnNetBrowserCursor, total,
		func(i int) string {
			net := m.ftnNetBrowserEntries[i]
			zone := fmt.Sprintf("%5d", net.Zone)
			name := padRight(net.Name, 12)
			descW := boxW - 5 - 2 - 12 - 2 - 2
			desc := net.Description
			if truncateToDisplayWidth(desc, descW) != desc {
				desc = truncateToDisplayWidth(desc, descW-3) + "..."
			}

			// Mark already-configured networks.
			marker := " "
			if m.ftnNetworkExists(net.Name) {
				marker = "*"
			}
			return fmt.Sprintf("%s %5s  %-12s  %s", marker, zone, name, desc)
		})

	// Separator before info panel.
	lb.separator()

	// Info panel for highlighted network.
	if total > 0 && m.ftnNetBrowserCursor < total {
		net := m.ftnNetBrowserEntries[m.ftnNetBrowserCursor]

		info1 := fmt.Sprintf("  %s — %s", net.Name, net.Description)
		lb.row(editInfoValueStyle.Render(padRight(info1, boxW)))

		coordStr := "  Coordinator: " + net.Coordinator
		if net.CoordinatorEmail != "" {
			coordStr += " <" + net.CoordinatorEmail + ">"
		}
		lb.row(editInfoValueStyle.Render(padRight(coordStr, boxW)))

		hubStr := fmt.Sprintf("  Hub: %s at %s", net.HubAddress, net.HubHostname)
		if net.HubPort > 0 {
			hubStr += fmt.Sprintf(":%d", net.HubPort)
		}
		lb.row(editInfoValueStyle.Render(padRight(hubStr, boxW)))

		infoStr := "  Info: " + net.InfoURL
		lb.row(editInfoValueStyle.Render(padRight(infoStr, boxW)))
	} else {
		lb.emptyRows(4)
	}

	lb.bottomBorder()
	lb.bgRows(lb.bottomPad + 1)

	return lb.finish("Enter - Select  |  C - Custom Network  |  ESC - Back")
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
