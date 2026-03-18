package configeditor

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// fetchNetworksMsg is the result of an auto-fetch of hub networks.
type fetchNetworksMsg struct {
	names []string
	err   error
}

// fetchHubNetworks returns a tea.Cmd that GETs /v3net/v1/networks from the hub.
func fetchHubNetworks(hubURL string) tea.Cmd {
	return func() tea.Msg {
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Get(strings.TrimRight(hubURL, "/") + "/v3net/v1/networks")
		if err != nil {
			return fetchNetworksMsg{err: err}
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fetchNetworksMsg{err: fmt.Errorf("status %d", resp.StatusCode)}
		}
		var summaries []struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&summaries); err != nil {
			return fetchNetworksMsg{err: err}
		}
		var names []string
		for _, s := range summaries {
			names = append(names, s.Name)
		}
		return fetchNetworksMsg{names: names}
	}
}

// --- Fork screen ---

func (m Model) updateV3NetSetupFork(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEscape:
		m.mode = modeTopMenu
		return m, nil
	case tea.KeyRunes:
		switch strings.ToLower(string(msg.Runes)) {
		case "j":
			var boardName string
			if m.configs != nil {
				boardName = m.configs.Server.BoardName
			}
			m.wizard = wizardState{
				flow:         "leaf",
				step:         0,
				pollInterval: "5m",
				origin:       boardName,
			}
			m.mode = modeV3NetWizardStep
			m.textInput.Reset()
			m.textInput.Focus()
		case "h":
			m.wizard = wizardState{
				flow: "hub",
				step: 0,
				port: "8765",
			}
			m.mode = modeV3NetWizardStep
			m.textInput.Reset()
			m.textInput.Focus()
		}
	}
	return m, nil
}

// --- Wizard step dispatcher ---

func (m Model) updateV3NetWizardStep(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.wizard.flow == "leaf" {
		return m.updateLeafWizardStep(msg)
	}
	return m.updateHubWizardStep(msg)
}

// updateWizardTextInput handles generic text input for wizard steps that
// use the shared textInput field. Returns the updated model.
func (m Model) updateWizardTextInput(msg tea.KeyMsg) Model {
	var cmd tea.Cmd
	m.textInput, cmd = m.textInput.Update(msg)
	_ = cmd
	return m
}

// handleFetchNetworksMsg is called from model.go's Update() via the
// fetchNetworksMsg case added to the outer type switch.
func (m Model) handleFetchNetworksMsg(msg fetchNetworksMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil || len(msg.names) == 0 {
		m.wizard.fetchError = "(could not reach hub — enter network name manually)"
		m.textInput.Reset()
		m.textInput.Focus()
		return m, nil
	}
	if len(msg.names) == 1 {
		m.wizard.networkName = msg.names[0]
		m.textInput.SetValue(msg.names[0])
		m.textInput.Focus()
		return m, nil
	}
	// Multiple networks — show picker.
	var items []LookupItem
	for _, name := range msg.names {
		items = append(items, LookupItem{Value: name, Display: name})
	}
	m.pickerItems = items
	m.pickerCursor = 0
	m.pickerScroll = 0
	m.pickerReturnMode = modeV3NetWizardStep
	m.mode = modeLookupPicker
	return m, nil
}

// --- Leaf wizard ---

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

// --- Hub wizard ---

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
	port, _ := strconv.Atoi(m.wizard.port)

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
