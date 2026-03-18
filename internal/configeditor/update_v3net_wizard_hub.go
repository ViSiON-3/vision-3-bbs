package configeditor

import (
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

const (
	hubStepNetwork     = 0
	hubStepPort        = 1
	hubStepAutoApprove = 2
	hubStepAreas       = 3
)

func (m Model) updateHubWizardStep(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyEscape && !m.wizard.areaAdding {
		m.mode = modeV3NetSetupFork
		return m, nil
	}

	switch m.wizard.step {
	case hubStepNetwork:
		return m.updateHubStepNetwork(msg)
	case hubStepPort:
		return m.updateHubStepPort(msg)
	case hubStepAutoApprove:
		return m.updateHubStepAutoApprove(msg)
	case hubStepAreas:
		return m.updateHubStepAreas(msg)
	}
	return m, nil
}

func (m Model) updateHubStepNetwork(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// This step has two sub-fields: name (textInput) then description.
	// We track which sub-field with wizard.areaAdding (repurposed: false=name, true=desc).
	if msg.Type == tea.KeyEnter {
		if !m.wizard.areaAdding {
			// Committing name.
			val := strings.TrimSpace(m.wizard.netName)
			if val == "" {
				m.message = "Network name cannot be empty"
				return m, nil
			}
			// Validate: lowercase alphanumeric only.
			for _, c := range val {
				if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9')) {
					m.message = "Network name must be lowercase alphanumeric only"
					return m, nil
				}
			}
			m.wizard.netName = val
			m.wizard.areaAdding = true // now editing description
			m.textInput.SetValue(m.wizard.netDesc)
			m.textInput.Focus()
		} else {
			// Committing description.
			m.wizard.netDesc = strings.TrimSpace(m.wizard.netDesc)
			m.wizard.areaAdding = false
			m.wizard.step = hubStepPort
			m.textInput.SetValue(m.wizard.port)
			m.textInput.Focus()
		}
		return m, nil
	}
	m = m.updateWizardTextInput(msg)
	if !m.wizard.areaAdding {
		m.wizard.netName = m.textInput.Value()
	} else {
		m.wizard.netDesc = m.textInput.Value()
	}
	return m, nil
}

func (m Model) updateHubStepPort(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyEnter {
		val := strings.TrimSpace(m.wizard.port)
		p, err := strconv.Atoi(val)
		if err != nil || p < 1 || p > 65535 {
			m.message = "Port must be a number between 1 and 65535"
			return m, nil
		}
		m.wizard.port = val
		m.wizard.step = hubStepAutoApprove
		m.textInput.Reset()
		return m, nil
	}
	m = m.updateWizardTextInput(msg)
	m.wizard.port = m.textInput.Value()
	return m, nil
}

func (m Model) updateHubStepAutoApprove(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEnter:
		m.wizard.step = hubStepAreas
		m.textInput.Reset()
		return m, nil
	case tea.KeyRunes:
		switch strings.ToLower(string(msg.Runes)) {
		case "y":
			m.wizard.autoApprove = true
		case "n":
			m.wizard.autoApprove = false
		}
	}
	return m, nil
}

func (m Model) updateHubStepAreas(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Sub-form open: collect tag then name.
	if m.wizard.areaAdding {
		return m.updateHubAreaSubForm(msg)
	}

	switch msg.Type {
	case tea.KeyEnter:
		if len(m.wizard.areas) == 0 {
			m.message = "At least one area is required"
			return m, nil
		}
		return m.confirmHubWizard()
	case tea.KeyUp:
		if m.wizard.areaCursor > 0 {
			m.wizard.areaCursor--
		}
	case tea.KeyDown:
		if m.wizard.areaCursor < len(m.wizard.areas)-1 {
			m.wizard.areaCursor++
		}
	case tea.KeyRunes:
		switch strings.ToUpper(string(msg.Runes)) {
		case "A":
			m.wizard.areaAdding = true
			m.wizard.areaEditTag = ""
			m.wizard.areaEditName = ""
			m.textInput.Reset()
			m.textInput.Focus()
		case "D":
			if len(m.wizard.areas) > 0 {
				i := m.wizard.areaCursor
				m.wizard.areas = append(m.wizard.areas[:i], m.wizard.areas[i+1:]...)
				if m.wizard.areaCursor >= len(m.wizard.areas) && m.wizard.areaCursor > 0 {
					m.wizard.areaCursor--
				}
			}
		}
	}
	return m, nil
}

func (m Model) updateHubAreaSubForm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyEscape {
		m.wizard.areaAdding = false
		m.textInput.Reset()
		return m, nil
	}
	if msg.Type == tea.KeyEnter {
		val := strings.TrimSpace(m.textInput.Value())
		if m.wizard.areaEditTag == "" {
			// Committing tag.
			if err := protocol.ValidateAreaTag(val); err != nil {
				m.message = err.Error()
				return m, nil
			}
			m.wizard.areaEditTag = val
			m.textInput.Reset()
			m.textInput.Focus()
			return m, nil
		}
		// Committing name.
		if val == "" {
			m.message = "Area name cannot be empty"
			return m, nil
		}
		m.wizard.areas = append(m.wizard.areas, wizardArea{
			Tag:  m.wizard.areaEditTag,
			Name: val,
		})
		m.wizard.areaCursor = len(m.wizard.areas) - 1
		m.wizard.areaAdding = false
		m.wizard.areaEditTag = ""
		m.wizard.areaEditName = ""
		m.textInput.Reset()
		return m, nil
	}
	m = m.updateWizardTextInput(msg)
	if m.wizard.areaEditTag != "" {
		m.wizard.areaEditName = m.textInput.Value()
	}
	return m, nil
}

func (m Model) confirmHubWizard() (Model, tea.Cmd) {
	port, _ := strconv.Atoi(m.wizard.port) // Safe: port was validated in updateHubStepPort.

	var initialAreas []config.V3NetHubArea
	for _, a := range m.wizard.areas {
		initialAreas = append(initialAreas, config.V3NetHubArea{Tag: a.Tag, Name: a.Name})
	}

	m.configs.V3Net.Enabled = true
	if m.configs.V3Net.KeystorePath == "" {
		m.configs.V3Net.KeystorePath = "data/v3net.key"
	}
	if m.configs.V3Net.DedupDBPath == "" {
		m.configs.V3Net.DedupDBPath = "data/v3net_dedup.sqlite"
	}
	m.configs.V3Net.Hub = config.V3NetHubConfig{
		Enabled:     true,
		Port:        port,
		DataDir:     "data/v3net_hub",
		AutoApprove: m.wizard.autoApprove,
		Networks: []config.V3NetHubNetwork{
			{Name: m.wizard.netName, Description: m.wizard.netDesc},
		},
		InitialAreas: initialAreas,
	}
	m.dirty = true
	m.saveAll()
	m.message = "Saved — start the BBS to initialize your hub and seed the NAL."
	m.mode = modeTopMenu
	return m, nil
}
