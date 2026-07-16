package configeditor

import (
	"fmt"
	"strings"
)

// viewFTNAreaBrowser renders the FTN echo area browser screen.
func (m Model) viewFTNAreaBrowser() string {
	boxW := 70
	total := len(m.ftnAreaBrowserAreas)
	lb := m.newListBox(boxW, ftnAreaBrowserListVisible+9)

	netName := ""
	if m.ftnWizard != nil {
		netName = m.ftnWizard.networkName
	}

	lb.topBorder()
	lb.title(fmt.Sprintf("Echo Areas — %s", netName))

	if m.ftnAreaBrowserLoading {
		return lb.statusScreen(menuItemStyle.Render(centerText("Downloading echolist...", boxW)),
			ftnAreaBrowserListVisible, 2, "ESC - Cancel")
	}

	if m.ftnAreaBrowserError != "" && total == 0 {
		return lb.statusScreen(lb.errorRow(m.ftnAreaBrowserError),
			ftnAreaBrowserListVisible, 2, "R - Retry  |  ESC - Back")
	}

	lb.colHeader(fmt.Sprintf("   %-4s %-16s %s", " ", "Tag", "Description"))
	lb.separator()

	lb.list(ftnAreaBrowserListVisible, m.ftnAreaBrowserScroll, m.ftnAreaBrowserCursor, total,
		func(i int) string {
			a := m.ftnAreaBrowserAreas[i]
			check := "[ ]"
			if m.ftnAreaBrowserSelected[i] {
				check = "[x]"
			}
			tag := padRight(a.Tag, 16)
			descW := boxW - 4 - 16 - 2
			desc := a.Description
			if truncateToDisplayWidth(desc, descW) != desc {
				desc = truncateToDisplayWidth(desc, descW-3) + "..."
			}
			return fmt.Sprintf("   %s %-16s %s", check, tag, desc)
		})

	lb.bottomBorder()
	lb.bgRows(lb.bottomPad)

	// Selection count.
	selected := 0
	for _, sel := range m.ftnAreaBrowserSelected {
		if sel {
			selected++
		}
	}
	countMsg := fmt.Sprintf("%d of %d areas selected", selected, total)
	lb.line(bgFillStyle.Render(strings.Repeat("░", lb.padL)) +
		editInfoValueStyle.Render(centerText(countMsg, boxW+2)) +
		bgFillStyle.Render(strings.Repeat("░", maxInt(0, lb.padR))))
	lb.bgRows(1)

	return lb.finish("Space - Toggle  |  A - All  |  N - None  |  Enter - Confirm  |  ESC - Back")
}
