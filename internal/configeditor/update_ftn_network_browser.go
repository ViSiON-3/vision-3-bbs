package configeditor

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ViSiON-3/vision-3-bbs/internal/ftn"
)

// ftnNetBrowserListVisible is the number of rows visible in the network browser.
const ftnNetBrowserListVisible = 12

// enterFTNNetworkBrowser loads the embedded registry and opens the browser.
func (m Model) enterFTNNetworkBrowser() (Model, tea.Cmd) {
	networks, err := ftn.LoadRegistry()
	if err != nil {
		m.message = fmt.Sprintf("Failed to load network registry: %v", err)
		return m, nil
	}

	// Also load sysop override file if it exists.
	overrides := m.loadFTNOverrideNetworks()
	if len(overrides) > 0 {
		networks = append(networks, overrides...)
	}

	m.ftnNetBrowserEntries = networks
	m.ftnNetBrowserCursor = 0
	m.ftnNetBrowserScroll = 0
	m.mode = modeFTNNetworkBrowser
	return m, nil
}

// loadFTNOverrideNetworks attempts to load ftn_networks.json from the config dir.
func (m Model) loadFTNOverrideNetworks() []ftn.RegistryNetwork {
	// Try to load from configs directory.
	data, err := ftn.LoadOverrideRegistry(m.configPath)
	if err != nil {
		return nil
	}
	return data
}

// updateFTNNetworkBrowser handles key events in the FTN network browser.
func (m Model) updateFTNNetworkBrowser(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	total := len(m.ftnNetBrowserEntries)

	if cursor, ok := listNavKey(msg, m.ftnNetBrowserCursor, total); ok {
		m.ftnNetBrowserCursor = cursor
		m.ftnNetBrowserScroll = clampListScroll(cursor, m.ftnNetBrowserScroll, ftnNetBrowserListVisible)
		return m, nil
	}

	switch msg.Type {
	case tea.KeyEnter:
		// Select the highlighted network and populate wizard fields.
		if total > 0 && m.ftnNetBrowserCursor < total {
			net := m.ftnNetBrowserEntries[m.ftnNetBrowserCursor]
			if m.ftnNetworkExists(net.Name) {
				m.message = fmt.Sprintf("%s is already configured — choose another network or press C for custom", net.Name)
				return m, nil
			}
			m.populateFTNWizardFromRegistry(&net)
		}
		m.mode = modeFTNWizardForm
		return m, nil

	case tea.KeyEscape:
		m.mode = modeFTNWizardForm
		return m, nil

	default:
		key := strings.ToUpper(msg.String())
		if key == "C" {
			// Custom network — return to wizard with no pre-fill.
			m.mode = modeFTNWizardForm
			return m, nil
		}
	}
	return m, nil
}

// populateFTNWizardFromRegistry fills wizard fields from a registry entry.
func (m *Model) populateFTNWizardFromRegistry(net *ftn.RegistryNetwork) {
	w := m.ftnWizard
	w.zone = net.Zone
	w.networkName = net.Name
	w.networkDesc = net.Description
	w.coordinator = net.Coordinator
	w.coordinatorEmail = net.CoordinatorEmail
	w.infoURL = net.InfoURL
	w.hubAddress = net.HubAddress
	w.hubHostname = net.HubHostname
	if net.HubPort > 0 {
		w.hubPort = net.HubPort
	}
	w.echolistURL = net.EcholistURL
	w.registryEntry = net

	// Pre-fill zone in own address.
	if w.ownAddress == "" && net.Zone > 0 {
		w.ownAddress = fmt.Sprintf("%d:", net.Zone)
	}

	// Clear cached areas when switching networks.
	w.availableAreas = nil
	w.selectedAreas = nil
	w.areasFetched = false
	w.areasFetchErr = ""

	// Refresh field definitions so closures point to updated state.
	m.ftnWizardFields = m.fieldsFTNWizard()
}
