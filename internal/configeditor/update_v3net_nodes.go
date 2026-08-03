package configeditor

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// enterNodeManagement opens the node screen for a hosted network.
func (m Model) enterNodeManagement(network string) (tea.Model, tea.Cmd) {
	m.nodesNetwork = network
	m.nodesList = nil
	m.nodesCursor = 0
	m.nodesScroll = 0
	m.nodesLoading = true
	m.nodesError = ""
	m.nodesConfirmDelete = false
	m.message = ""
	m.mode = modeV3NetNodes
	return m, fetchHubNodes(m.configs.V3Net.Hub.Port, network, m.configs.V3Net.KeystorePath)
}

// handleFetchNodesMsg processes the node list fetch result.
func (m Model) handleFetchNodesMsg(msg fetchNodesMsg) (tea.Model, tea.Cmd) {
	m.nodesLoading = false
	if msg.err != nil {
		m.nodesError = hubErrorText("Could not fetch nodes", msg.err)
		return m, nil
	}
	m.nodesList = msg.nodes
	m.nodesCursor = 0
	m.nodesScroll = 0
	m.nodesError = ""
	return m, nil
}

// handleNodeActionMsg processes an approve/ban/unban/remove result.
func (m Model) handleNodeActionMsg(msg nodeActionMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.message = hubErrorText("Action failed", msg.err)
		return m, nil
	}
	if msg.action == "remove" {
		kept := m.nodesList[:0:0]
		for _, n := range m.nodesList {
			if n.NodeID != msg.nodeID {
				kept = append(kept, n)
			}
		}
		m.nodesList = kept
		if m.nodesCursor >= len(m.nodesList) && m.nodesCursor > 0 {
			m.nodesCursor--
		}
		m.message = "Node removed"
		return m, nil
	}
	for i := range m.nodesList {
		if m.nodesList[i].NodeID == msg.nodeID {
			m.nodesList[i].Status = msg.status
		}
	}
	m.message = "Node " + msg.action + "d" // approved / banned / unbanned reads fine
	return m, nil
}

// updateV3NetNodes handles key events on the node management screen.
func (m Model) updateV3NetNodes(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	total := len(m.nodesList)

	if m.nodesConfirmDelete {
		switch msg.String() {
		case "y", "Y":
			m.nodesConfirmDelete = false
			if n := m.currentNode(); n != nil {
				return m, nodeAction(m.configs.V3Net.Hub.Port, m.nodesNetwork,
					m.configs.V3Net.KeystorePath, n.NodeID, "remove")
			}
			return m, nil
		default:
			m.nodesConfirmDelete = false
			return m, nil
		}
	}

	if cursor, ok := listNavKey(msg, m.nodesCursor, total); ok {
		m.nodesCursor = cursor
		m.nodesScroll = clampListScroll(cursor, m.nodesScroll, nodesListVisible)
		return m, nil
	}

	if msg.Type == tea.KeyEscape {
		m.mode = modeRecordList
		return m, nil
	}

	switch msg.String() {
	case "a", "A":
		if n := m.currentNode(); n != nil && n.Status == "pending" {
			return m, nodeAction(m.configs.V3Net.Hub.Port, m.nodesNetwork,
				m.configs.V3Net.KeystorePath, n.NodeID, "approve")
		}
	case "b", "B":
		if n := m.currentNode(); n != nil {
			action := "ban"
			if n.Status == "banned" {
				action = "unban"
			}
			return m, nodeAction(m.configs.V3Net.Hub.Port, m.nodesNetwork,
				m.configs.V3Net.KeystorePath, n.NodeID, action)
		}
	case "d", "D":
		if total > 0 {
			m.nodesConfirmDelete = true
		}
	case "r", "R":
		m.nodesLoading = true
		return m, fetchHubNodes(m.configs.V3Net.Hub.Port, m.nodesNetwork, m.configs.V3Net.KeystorePath)
	}
	return m, nil
}

// currentNode returns the node under the cursor, or nil.
func (m *Model) currentNode() *protocol.NodeInfo {
	if m.nodesCursor < 0 || m.nodesCursor >= len(m.nodesList) {
		return nil
	}
	return &m.nodesList[m.nodesCursor]
}
