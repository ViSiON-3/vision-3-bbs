package configeditor

import (
	"fmt"
	"strings"

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

	if m.areaBrowserEditing {
		return m.updateAreaBrowserEdit(msg)
	}

	if m.areaBrowserManual {
		return m.updateAreaBrowserManual(msg)
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
		key := strings.ToUpper(msg.String())
		switch key {
		case "E":
			if total == 0 || m.areaBrowserCursor >= total {
				return m, nil
			}
			item := &m.areaBrowserAreas[m.areaBrowserCursor]
			if !item.Subscribed {
				m.message = "Subscribe first to set a local board name"
				return m, nil
			}
			m.areaBrowserEditing = true
			m.textInput.SetValue(item.LocalBoard)
			m.textInput.CharLimit = 40
			m.textInput.Width = 40
			m.textInput.CursorEnd()
			m.textInput.Focus()
			return m, nil

		case "M":
			m.areaBrowserManual = true
			var tags []string
			for _, a := range m.areaBrowserAreas {
				if a.Subscribed {
					tags = append(tags, a.Tag)
				}
			}
			m.textInput.SetValue(strings.Join(tags, ","))
			m.textInput.CharLimit = 200
			m.textInput.Width = 60
			m.textInput.CursorEnd()
			m.textInput.Focus()
			m.message = ""
			return m, nil

		case "R":
			if m.areaBrowserError != "" {
				m.areaBrowserLoading = true
				m.areaBrowserError = ""
				return m, fetchHubNAL(m.areaBrowserHub, m.areaBrowserNetwork)
			}
		}
	}
	return m, nil
}

// updateAreaBrowserEdit handles key events while editing a local board name.
func (m Model) updateAreaBrowserEdit(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEnter:
		val := m.textInput.Value()
		if val == "" {
			m.message = "Board name cannot be empty"
			return m, nil
		}
		m.areaBrowserAreas[m.areaBrowserCursor].LocalBoard = val
		m.textInput.Blur()
		m.areaBrowserEditing = false
		m.message = ""
		return m, nil
	case tea.KeyEscape:
		m.textInput.Blur()
		m.areaBrowserEditing = false
		m.message = ""
		return m, nil
	default:
		var cmd tea.Cmd
		m.textInput, cmd = m.textInput.Update(msg)
		return m, cmd
	}
}

// updateAreaBrowserManual handles key events in manual text entry mode.
func (m Model) updateAreaBrowserManual(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEnter:
		val := m.textInput.Value()
		var tags []string
		for _, t := range strings.Split(val, ",") {
			if tag := strings.TrimSpace(t); tag != "" {
				tags = append(tags, tag)
			}
		}
		tagSet := make(map[string]bool)
		for _, t := range tags {
			tagSet[t] = true
		}
		for i := range m.areaBrowserAreas {
			m.areaBrowserAreas[i].Subscribed = tagSet[m.areaBrowserAreas[i].Tag]
			if m.areaBrowserAreas[i].Subscribed && m.areaBrowserAreas[i].LocalBoard == "" {
				m.areaBrowserAreas[i].LocalBoard = defaultLocalBoardName(
					m.areaBrowserNetwork, m.areaBrowserAreas[i].Name)
			}
		}
		existing := make(map[string]bool)
		for _, a := range m.areaBrowserAreas {
			existing[a.Tag] = true
		}
		for _, t := range tags {
			if !existing[t] {
				m.areaBrowserAreas = append(m.areaBrowserAreas, areaBrowserItem{
					Tag:        t,
					Name:       t,
					Subscribed: true,
					LocalBoard: defaultLocalBoardName(m.areaBrowserNetwork, t),
				})
			}
		}
		m.textInput.Blur()
		m.areaBrowserManual = false
		m.message = fmt.Sprintf("%d area(s) set", len(tags))
		return m, nil
	case tea.KeyEscape:
		m.textInput.Blur()
		m.areaBrowserManual = false
		m.message = ""
		return m, nil
	default:
		var cmd tea.Cmd
		m.textInput, cmd = m.textInput.Update(msg)
		return m, cmd
	}
}
