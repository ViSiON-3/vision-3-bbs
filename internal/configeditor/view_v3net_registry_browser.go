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
	boxW := 70
	listVisible := regBrowserListVisible
	total := len(m.regBrowserEntries)

	// Fixed rows: header(1) + border(1) + title(1) + colheader(1) + sep(1)
	//           + list(10) + border(1) + msg(1) + bgLine(1) + help(1)
	lb := m.newListBox(boxW, listVisible+9)

	lb.topBorder()
	lb.title("Network Registry")

	if m.regBrowserLoading {
		return lb.statusScreen(menuItemStyle.Render(centerText("Fetching registry...", boxW)),
			listVisible, 2, "ESC - Cancel")
	}

	if m.regBrowserError != "" && total == 0 {
		return lb.statusScreen(lb.errorRow(m.regBrowserError),
			listVisible, 2, "R - Retry  |  ESC - Back")
	}

	lb.colHeader(fmt.Sprintf("  %-14s %-28s %s", "Network", "Description", "Hub URL"))
	lb.separator()

	lb.list(listVisible, m.regBrowserScroll, m.regBrowserCursor, total,
		func(i int) string {
			e := m.regBrowserEntries[i]
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
			return fmt.Sprintf("%s%-14s %-28s %s", tag, name, desc, hubURL)
		})

	lb.bottomBorder()
	lb.bgRows(lb.bottomPad)
	lb.messageRow(m.message)
	lb.bgRows(1)

	return lb.finish("Enter - Select  |  ESC - Back  |  * = subscribed")
}
