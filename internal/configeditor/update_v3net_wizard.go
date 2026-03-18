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
	m.textInput, _ = m.textInput.Update(msg)
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
