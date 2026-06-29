package wfcui

import (
	tea "github.com/charmbracelet/bubbletea"
)

// handleKey dispatches keyboard input to the appropriate action.
// The Model is received and returned by value (Bubble Tea convention).
func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Global keys — active in any mode.
	switch msg.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit
	}
	if msg.Type == tea.KeyRunes {
		switch string(msg.Runes) {
		case "q", "Q":
			return m, tea.Quit
		}
	}

	// Mode-specific keys.
	switch m.mode {
	case modeDetails:
		return m.handleKeyDetails(msg)
	case modeDisconnected:
		return m.handleKeyDisconnected(msg)
	default:
		// modeList and modeTooSmall share list-mode navigation.
		return m.handleKeyList(msg)
	}
}

// handleKeyList handles keys when in list (or too-small) mode.
func (m Model) handleKeyList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyDown:
		if m.snapshot != nil && m.selected < len(m.snapshot.Nodes)-1 {
			m.selected++
		}
	case tea.KeyUp:
		if m.selected > 0 {
			m.selected--
		}
	case tea.KeyEnter:
		if m.mode == modeList {
			m.mode = modeDetails
		}
	case tea.KeyRunes:
		switch string(msg.Runes) {
		case "r", "R":
			if m.client != nil {
				return m, m.fetchSnapshot()
			}
		case "l", "L":
			m.showLogs = !m.showLogs
		case "?":
			// Help: no-op for now; hint is in the cmd bar.
		}
	}
	return m, nil
}

// handleKeyDetails handles keys when viewing node details.
func (m Model) handleKeyDetails(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.mode = modeList
	case tea.KeyRunes:
		switch string(msg.Runes) {
		case "l", "L":
			m.showLogs = !m.showLogs
		}
	}
	return m, nil
}

// handleKeyDisconnected handles keys when in disconnected mode.
func (m Model) handleKeyDisconnected(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyRunes {
		switch string(msg.Runes) {
		case "r", "R":
			if m.client != nil {
				return m, m.fetchSnapshot()
			}
			// No client — stay disconnected; nothing to do.
		}
	}
	return m, nil
}
