package wfcui

import (
	tea "github.com/charmbracelet/bubbletea"
)

// handleKey dispatches keyboard input to the appropriate action.
// The Model is received and returned by value (Bubble Tea convention).
func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	if m.mode == modeConfirmKick {
		return m.handleKeyConfirmKick(msg)
	}
	switch msg.Type {
	case tea.KeyTab:
		return m.nextTab(1), nil
	case tea.KeyShiftTab:
		return m.nextTab(-1), nil
	case tea.KeyPgUp:
		return m.page(-1), nil
	case tea.KeyPgDown:
		return m.page(1), nil
	case tea.KeyRunes:
		switch string(msg.Runes) {
		case "q", "Q":
			return m, tea.Quit
		case "r", "R":
			return m.refreshOrRetry()
		case "k", "K":
			return m.beginKick()
		}
	}
	switch m.mode {
	case modeDetails:
		return m.handleKeyDetails(msg)
	default:
		return m.handleKeyList(msg)
	}
}

// nextTab cycles the lower box through Callers, Bots and Events.
func (m Model) nextTab(step int) Model {
	m.tab = group((int(m.tab) + step + int(groupCount)) % int(groupCount))
	m.selected = 0
	m.scrollBack = 0
	if m.mode == modeDetails {
		m.mode = modeList
	}
	return m
}

// page is PgUp (dir -1) / PgDn (dir +1): it scrolls the log in the lower
// box a screenful at a time, or on the Events tab moves the cursor a page.
func (m Model) page(dir int) Model {
	rows := m.lowerRows()
	if m.tab == groupEvents {
		return m.moveSelection(dir * rows)
	}
	step := rows - 1
	if step < 1 {
		step = 1
	}
	m.scrollBack -= dir * step
	m.clampScroll()
	return m
}

// refreshOrRetry is R: a manual refresh while connected, an immediate
// reconnect attempt while offline, nothing while a dial is already running.
func (m Model) refreshOrRetry() (tea.Model, tea.Cmd) {
	switch m.conn {
	case connConnected:
		return m, m.refreshNow()
	case connLost:
		if m.opts.Dial == nil {
			return m, nil
		}
		return m, m.startDial()
	}
	return m, nil
}

// beginKick is K: open the confirm prompt for the selected connection.
func (m Model) beginKick() (tea.Model, tea.Cmd) {
	if m.opts.ReadOnly {
		m.setStatus("Read-only console: kick is disabled", true)
		return m, nil
	}
	if m.conn != connConnected {
		m.setStatus("Not connected", true)
		return m, nil
	}
	if _, ok := m.selectedNode(); !ok {
		m.setStatus("No caller selected", true)
		return m, nil
	}
	m.prevMode = m.mode
	m.mode = modeConfirmKick
	return m, nil
}

// handleKeyConfirmKick: Y sends the kick, anything else cancels.
func (m Model) handleKeyConfirmKick(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.mode = m.prevMode
	if msg.Type == tea.KeyRunes {
		switch string(msg.Runes) {
		case "y", "Y":
			n, ok := m.selectedNode()
			if !ok {
				return m, nil
			}
			return m, m.kick(n)
		}
	}
	return m, nil
}

// moveSelection moves the cursor by delta within the current tab.
func (m Model) moveSelection(delta int) Model {
	n := m.tabCount()
	m.selected += delta
	if m.selected >= n {
		m.selected = n - 1
	}
	if m.selected < 0 {
		m.selected = 0
	}
	return m
}

// handleKeyList handles keys in the table.
func (m Model) handleKeyList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyDown:
		m = m.moveSelection(1)
	case tea.KeyUp:
		m = m.moveSelection(-1)
	case tea.KeyHome:
		m.selected = 0
	case tea.KeyEnd:
		m = m.moveSelection(m.tabCount())
	case tea.KeyEnter:
		if m.tabCount() > 0 {
			m.mode = modeDetails
		}
	}
	return m, nil
}

// handleKeyDetails handles keys in the details overlay.
func (m Model) handleKeyDetails(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc, tea.KeyEnter, tea.KeyBackspace:
		m.mode = modeList
	case tea.KeyDown:
		m = m.moveSelection(1)
	case tea.KeyUp:
		m = m.moveSelection(-1)
	}
	return m, nil
}
