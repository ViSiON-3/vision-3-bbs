package configeditor

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

const (
	leafStepHubURL       = 0
	leafStepNetwork      = 1
	leafStepBoard        = 2
	leafStepPollInterval = 3
	leafStepOrigin       = 4
)

func (m Model) updateLeafWizardStep(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// ESC anywhere in the leaf wizard returns to fork.
	if msg.Type == tea.KeyEscape {
		m.mode = modeV3NetSetupFork
		return m, nil
	}

	switch m.wizard.step {
	case leafStepHubURL:
		return m.updateLeafStepHubURL(msg)
	case leafStepNetwork:
		return m.updateLeafStepNetwork(msg)
	case leafStepBoard:
		return m.updateLeafStepBoard(msg)
	case leafStepPollInterval:
		return m.updateLeafStepPollInterval(msg)
	case leafStepOrigin:
		return m.updateLeafStepOrigin(msg)
	}
	return m, nil
}

func (m Model) updateLeafStepHubURL(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyEnter {
		// Validate m.wizard.hubURL (kept in sync during typing).
		val := strings.TrimSpace(m.wizard.hubURL)
		if val == "" || (!strings.HasPrefix(val, "http://") && !strings.HasPrefix(val, "https://")) {
			m.message = "Hub URL must start with http:// or https://"
			return m, nil
		}
		m.wizard.hubURL = val
		m.wizard.step = leafStepNetwork
		m.wizard.fetchError = ""
		m.textInput.Reset()
		// Auto-fetch network list.
		return m, fetchHubNetworks(val)
	}
	m = m.updateWizardTextInput(msg)
	m.wizard.hubURL = m.textInput.Value()
	return m, nil
}

func (m Model) updateLeafStepNetwork(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyEnter {
		val := strings.TrimSpace(m.wizard.networkName)
		if val == "" {
			m.message = "Network name cannot be empty"
			return m, nil
		}
		m.wizard.networkName = val
		m.wizard.step = leafStepBoard
		m.textInput.Reset()
		return m, nil
	}
	m = m.updateWizardTextInput(msg)
	m.wizard.networkName = m.textInput.Value()
	return m, nil
}

func (m Model) updateLeafStepBoard(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyEnter {
		val := strings.TrimSpace(m.wizard.boardTag)
		if val == "" {
			m.message = "Board tag cannot be empty"
			return m, nil
		}
		m.wizard.boardTag = val
		m.wizard.step = leafStepPollInterval
		m.textInput.SetValue(m.wizard.pollInterval)
		m.textInput.Focus()
		return m, nil
	}
	m = m.updateWizardTextInput(msg)
	m.wizard.boardTag = m.textInput.Value()
	return m, nil
}

func (m Model) updateLeafStepPollInterval(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyEnter {
		val := strings.TrimSpace(m.wizard.pollInterval)
		d, err := time.ParseDuration(val)
		if err != nil || d <= 0 {
			m.message = "Poll interval must be a valid duration (e.g. 5m, 30s)"
			return m, nil
		}
		m.wizard.pollInterval = val
		m.wizard.step = leafStepOrigin
		m.textInput.SetValue(m.wizard.origin)
		m.textInput.Focus()
		return m, nil
	}
	m = m.updateWizardTextInput(msg)
	m.wizard.pollInterval = m.textInput.Value()
	return m, nil
}

func (m Model) updateLeafStepOrigin(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyEnter {
		m.wizard.origin = strings.TrimSpace(m.wizard.origin)
		return m.confirmLeafWizard()
	}
	m = m.updateWizardTextInput(msg)
	m.wizard.origin = m.textInput.Value()
	return m, nil
}

func (m Model) confirmLeafWizard() (Model, tea.Cmd) {
	// Duplicate check.
	for _, l := range m.configs.V3Net.Leaves {
		if l.HubURL == m.wizard.hubURL && l.Network == m.wizard.networkName {
			m.message = "Already subscribed to this network on this hub"
			m.mode = modeV3NetWizardStep
			return m, nil
		}
	}

	leaf := config.V3NetLeafConfig{
		HubURL:       m.wizard.hubURL,
		Network:      m.wizard.networkName,
		Board:        m.wizard.boardTag,
		PollInterval: m.wizard.pollInterval,
		Origin:       m.wizard.origin,
	}
	m.configs.V3Net.Leaves = append(m.configs.V3Net.Leaves, leaf)
	m.configs.V3Net.Enabled = true
	m.dirty = true
	m.saveAll()
	m.message = "Saved — restart the BBS to activate."
	m.mode = modeTopMenu
	return m, nil
}
