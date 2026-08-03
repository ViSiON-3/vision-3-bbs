package configeditor

import "fmt"

// nodesListVisible is the number of rows shown in the node management list.
const nodesListVisible = 10

// viewV3NetNodes renders the node management screen for a hosted network.
func (m Model) viewV3NetNodes() string {
	boxW := 70
	listVisible := nodesListVisible
	total := len(m.nodesList)

	lb := m.newListBox(boxW, listVisible+9)
	lb.topBorder()
	lb.title(fmt.Sprintf("Node Management — %s", m.nodesNetwork))

	if m.nodesLoading {
		return lb.statusScreen(menuItemStyle.Render(centerText("Fetching nodes...", boxW)),
			listVisible, 3, "ESC - Cancel")
	}
	if m.nodesError != "" && total == 0 {
		return lb.statusScreen(lb.errorRow(m.nodesError),
			listVisible, 3, "R - Retry  |  ESC - Back")
	}

	lb.colHeader(fmt.Sprintf("   %-16s %-20s %-8s %s", "Node ID", "BBS Name", "Status", "Joined"))
	lb.separator()
	lb.list(listVisible, m.nodesScroll, m.nodesCursor, total,
		func(i int) string {
			n := m.nodesList[i]
			joined := n.CreatedAt
			if len(joined) > 10 {
				joined = joined[:10] // date only
			}
			return fmt.Sprintf("   %-16s %-20s %-8s %s",
				n.NodeID, padRight(sanitizeRegistryField(n.BBSName), 20), padRight(n.Status, 8), joined)
		})
	lb.bottomBorder()
	lb.bgRows(lb.bottomPad)
	if m.nodesConfirmDelete {
		lb.messageRow("Remove this node registration? Y/N")
	} else {
		lb.messageRow(m.message)
	}
	lb.bgRows(1)
	return lb.finish("A - Approve  |  B - Ban/Unban  |  D - Delete  |  R - Refresh  |  ESC - Back")
}
