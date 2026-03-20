package configeditor

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
)

// updateV3NetAreaBrowser handles key events in the area browser.
func (m Model) updateV3NetAreaBrowser(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.areaBrowserLoading {
		if msg.Type == tea.KeyEscape {
			m.areaBrowserLoading = false
			m.mode = m.areaBrowserReturn
		}
		return m, nil
	}

	total := len(m.areaBrowserAreas)

	switch msg.Type {
	case tea.KeyUp:
		if m.areaBrowserCursor > 0 {
			m.areaBrowserCursor--
		}
		m.clampAreaBrowserScroll()

	case tea.KeyDown:
		if m.areaBrowserCursor < total-1 {
			m.areaBrowserCursor++
		}
		m.clampAreaBrowserScroll()

	case tea.KeyHome:
		m.areaBrowserCursor = 0
		m.clampAreaBrowserScroll()

	case tea.KeyEnd:
		if total > 0 {
			m.areaBrowserCursor = total - 1
		}
		m.clampAreaBrowserScroll()

	case tea.KeySpace:
		if total == 0 || m.areaBrowserCursor >= total {
			return m, nil
		}
		item := &m.areaBrowserAreas[m.areaBrowserCursor]
		if item.Subscribed {
			item.Subscribed = false
			item.Status = ""
			item.LocalBoard = ""
			m.message = fmt.Sprintf("Unsubscribed from %s", item.Tag)
			return m, nil
		}
		item.Subscribed = true
		item.LocalBoard = defaultLocalBoardName(m.areaBrowserNetwork, item.Name)

		ks, err := m.loadOrCreateIdentityKeystore()
		if err != nil {
			m.message = fmt.Sprintf("Keystore error: %v", err)
			item.Subscribed = false
			item.LocalBoard = ""
			return m, nil
		}

		var tags []string
		for _, a := range m.areaBrowserAreas {
			if a.Subscribed {
				tags = append(tags, a.Tag)
			}
		}

		bbsName := ""
		bbsHost := ""
		if m.configs != nil {
			bbsName = m.configs.Server.BoardName
			bbsHost = m.configs.Server.SSHHost
			if bbsHost == "" {
				bbsHost = m.configs.Server.TelnetHost
			}
		}

		m.message = "Subscribing..."
		return m, subscribeToAreas(
			m.areaBrowserHub, m.areaBrowserNetwork, tags,
			ks.NodeID(), ks.PubKeyBase64(), bbsName, bbsHost,
		)

	case tea.KeyEscape:
		return m.exitAreaBrowser()

	default:
		if msg.String() == "r" || msg.String() == "R" {
			if m.areaBrowserError != "" {
				m.areaBrowserLoading = true
				m.areaBrowserError = ""
				return m, fetchHubNAL(m.areaBrowserHub, m.areaBrowserNetwork)
			}
		}
	}
	return m, nil
}
