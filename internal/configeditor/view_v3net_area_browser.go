package configeditor

import "fmt"

// viewV3NetAreaBrowser renders the area browser screen.
func (m Model) viewV3NetAreaBrowser() string {
	boxW := 70
	listVisible := areaBrowserListVisible
	total := len(m.areaBrowserAreas)

	// Fixed rows: header(1) + border(1) + title(1) + colheader(1) + sep(1)
	//           + list(10) + border(1) + msg(1) + bgLine(1) + help(1)
	lb := m.newListBox(boxW, listVisible+9)

	lb.topBorder()
	lb.title(fmt.Sprintf("Area Browser — %s", m.areaBrowserNetwork))

	if m.areaBrowserLoading {
		return lb.statusScreen(menuItemStyle.Render(centerText("Fetching areas...", boxW)),
			listVisible, 3, "ESC - Cancel")
	}

	if m.areaBrowserError != "" && total == 0 {
		return lb.statusScreen(lb.errorRow(m.areaBrowserError),
			listVisible, 3, "R - Retry  |  ESC - Back")
	}

	lb.colHeader(fmt.Sprintf("   %-4s %-16s %-16s %-8s %s", " ", "Tag", "Name", "Status", "Local Board"))
	lb.separator()

	lb.list(listVisible, m.areaBrowserScroll, m.areaBrowserCursor, total,
		func(i int) string {
			a := m.areaBrowserAreas[i]
			check := "[ ]"
			if a.Subscribed {
				check = "[x]"
			}
			tag := padRight(a.Tag, 16)
			if len(tag) > 16 {
				tag = tag[:16]
			}
			// padRight is rune-aware and both pads and truncates to width.
			name := padRight(a.Name, 16)
			status := padRight(a.Status, 8)
			maxBoard := boxW - 4 - 16 - 16 - 8 - 5
			localBoard := truncateToDisplayWidth(a.LocalBoard, maxBoard)
			return fmt.Sprintf("   %s %-16s %-16s %-8s %s",
				check, tag, name, status, localBoard)
		})

	lb.bottomBorder()
	lb.bgRows(lb.bottomPad)
	lb.messageRow(m.message)
	lb.bgRows(1)

	return lb.finish("Space - Subscribe/Unsubscribe  |  ESC - Done")
}
