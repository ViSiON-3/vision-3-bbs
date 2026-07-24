package configeditor

import (
	tea "github.com/charmbracelet/bubbletea"
)

// updateFTNNodelistLookup handles keys while the nodelist download runs.
func (m Model) updateFTNNodelistLookup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyEscape {
		m.ftnWizard.lookupLoading = false
		m.mode = modeFTNWizardForm
	}
	return m, nil
}

// handleFTNNodelistMsg processes the nodelist download result.
func (m Model) handleFTNNodelistMsg(msg ftnNodelistMsg) (tea.Model, tea.Cmd) {
	// The user pressed ESC and left the download mode: drop the late result.
	if m.mode != modeFTNNodelistLookup {
		return m, nil
	}
	m.ftnWizard.lookupLoading = false
	m.mode = modeFTNWizardForm
	if msg.err != nil {
		m.ftnWizard.lookupErr = msg.err.Error()
		m.message = "Nodelist download failed: " + msg.err.Error()
		return m, nil
	}
	m.ftnWizard.nodelist = msg.nodelist
	m.ftnWizard.lookupErr = ""
	m.applyFTNNodeLookup()
	return m, nil
}

// applyFTNNodeLookup runs the lookup against the cached nodelist and
// autofills hub fields. Implemented with the wizard row in the next commit.
func (m *Model) applyFTNNodeLookup() {}
