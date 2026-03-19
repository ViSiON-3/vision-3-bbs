package configeditor

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
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

// enterLeafWizard opens the leaf setup wizard form.
func (m Model) enterLeafWizard() (tea.Model, tea.Cmd) {
	var boardName string
	if m.configs != nil {
		boardName = m.configs.Server.BoardName
	}
	m.wizard = &wizardState{
		flow:         "leaf",
		pollInterval: "5m",
		origin:       boardName,
	}
	m.wizardTitle = "Leaf Setup — Join a Network"
	m.wizardFields = m.fieldsLeafWizard()
	m.editField = 0
	m.fieldScroll = 0
	m.mode = modeWizardForm
	return m, nil
}

// enterHubWizard opens the hub setup wizard form.
func (m Model) enterHubWizard() (tea.Model, tea.Cmd) {
	m.wizard = &wizardState{
		flow: "hub",
		port: "8765",
	}
	m.wizardTitle = "Hub Setup — Host a Network"
	m.wizardFields = m.fieldsHubWizard()
	m.editField = 0
	m.fieldScroll = 0
	m.mode = modeWizardForm
	return m, nil
}

// updateV3NetWizardStep handles the hub areas sub-form
// (the only remaining modeV3NetWizardStep usage).
func (m Model) updateV3NetWizardStep(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	return m.updateHubWizardStep(msg)
}

// updateWizardTextInput handles generic text input for wizard steps that
// use the shared textInput field. Returns the updated model.
func (m Model) updateWizardTextInput(msg tea.KeyMsg) Model {
	m.textInput, _ = m.textInput.Update(msg)
	return m
}

// handleFetchNetworksMsg is called from model.go's Update() via the
// fetchNetworksMsg case added to the outer type switch.
func (m Model) handleFetchNetworksMsg(msg fetchNetworksMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil || len(msg.names) == 0 {
		m.wizard.fetchError = "(could not reach hub — enter network name manually)"
		m.message = m.wizard.fetchError
		return m, nil
	}
	if len(msg.names) == 1 {
		m.wizard.networkName = msg.names[0]
		m.message = "Auto-detected network: " + msg.names[0]
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
	m.pickerReturnMode = modeWizardForm
	m.mode = modeLookupPicker
	return m, nil
}
