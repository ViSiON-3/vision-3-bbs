package configeditor

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/registry"
)

// updateRegistryBrowser handles key events in the registry browser.
func (m Model) updateRegistryBrowser(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.regBrowserLoading {
		if msg.Type == tea.KeyEscape {
			if m.regBrowserCancel != nil {
				m.regBrowserCancel()
				m.regBrowserCancel = nil
			}
			m.regBrowserLoading = false
			m.mode = m.regBrowserReturn
		}
		return m, nil
	}

	total := len(m.regBrowserEntries)

	switch msg.Type {
	case tea.KeyUp:
		if m.regBrowserCursor > 0 {
			m.regBrowserCursor--
		}
		m.clampRegBrowserScroll()

	case tea.KeyDown:
		if m.regBrowserCursor < total-1 {
			m.regBrowserCursor++
		}
		m.clampRegBrowserScroll()

	case tea.KeyHome:
		m.regBrowserCursor = 0
		m.clampRegBrowserScroll()

	case tea.KeyEnd:
		if total > 0 {
			m.regBrowserCursor = total - 1
		}
		m.clampRegBrowserScroll()

	case tea.KeyEnter:
		if total == 0 || m.regBrowserCursor >= total {
			return m, nil
		}
		entry := m.regBrowserEntries[m.regBrowserCursor]
		if m.isLeafSubscribed(entry.Name) {
			m.message = "Already subscribed to " + entry.Name
			return m, nil
		}
		return m.selectRegistryEntry(entry)

	case tea.KeyEscape:
		m.mode = m.regBrowserReturn
		return m, nil

	default:
		switch msg.String() {
		case "r", "R":
			if m.regBrowserError != "" {
				return m.enterRegistryBrowser(m.regBrowserReturn)
			}
		}
	}

	return m, nil
}

// handleFetchRegistryMsg processes the result of a registry fetch.
func (m Model) handleFetchRegistryMsg(msg fetchRegistryMsg) (tea.Model, tea.Cmd) {
	// Ignore stale responses from a previous (cancelled or retried) fetch.
	if msg.requestID != m.regBrowserRequestID {
		return m, nil
	}
	m.regBrowserLoading = false
	if m.regBrowserCancel != nil {
		m.regBrowserCancel()
		m.regBrowserCancel = nil
	}
	if msg.err != nil {
		cause := msg.err
		var urlErr *url.Error
		if errors.As(msg.err, &urlErr) {
			cause = urlErr.Err
		}
		m.regBrowserError = fmt.Sprintf("Could not fetch registry: %v", sanitizeRegistryField(cause.Error()))
		return m, nil
	}
	m.regBrowserError = ""
	m.regBrowserEntries = msg.entries
	return m, nil
}

// enterRegistryBrowser opens the registry browser, fetching from the configured
// registry URL (or the default).
func (m Model) enterRegistryBrowser(returnMode editorMode) (tea.Model, tea.Cmd) {
	url := registry.DefaultURL
	if m.configs != nil && m.configs.V3Net.RegistryURL != "" {
		url = m.configs.V3Net.RegistryURL
	}
	// Cancel any previous in-flight fetch.
	if m.regBrowserCancel != nil {
		m.regBrowserCancel()
	}
	m.regBrowserRequestID++
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	m.regBrowserEntries = nil
	m.regBrowserCursor = 0
	m.regBrowserScroll = 0
	m.regBrowserLoading = true
	m.regBrowserCancel = cancel
	m.regBrowserError = ""
	m.regBrowserReturn = returnMode
	m.message = ""
	m.mode = modeRegistryBrowser
	return m, fetchRegistry(ctx, m.regBrowserRequestID, url)
}

// selectRegistryEntry fills the wizard fields from a selected registry entry
// and returns to the wizard form. If no wizard is active (browsing from the
// record list), a new leaf wizard is created pre-filled with the entry.
func (m Model) selectRegistryEntry(entry protocol.RegistryEntry) (tea.Model, tea.Cmd) {
	if m.wizard == nil {
		// Launched from the record list — create a pre-filled leaf wizard.
		var boardName string
		if m.configs != nil {
			boardName = m.configs.Server.BoardName
		}
		m.wizard = &wizardState{
			flow:         "leaf",
			hubURL:       entry.HubURL,
			networkName:  entry.Name,
			pollInterval: "5m",
			origin:       boardName,
		}
		m.wizardTitle = "Leaf Setup — Join a Network"
		m.wizardFields = m.fieldsLeafWizard()
		m.editField = 0
		m.fieldScroll = 0
		m.message = "Selected " + entry.Name
		m.mode = modeWizardForm
		return m, nil
	}

	// Already in wizard — update its fields and return.
	m.wizard.hubURL = entry.HubURL
	m.wizard.networkName = entry.Name
	m.wizardFields = m.fieldsLeafWizard()
	m.message = "Selected " + entry.Name
	m.mode = m.regBrowserReturn
	return m, nil
}

// enterRegistryBrowserForLeafList opens the registry browser from the leaf
// subscription list. On selection it creates a pre-filled leaf wizard.
func (m Model) enterRegistryBrowserForLeafList() (tea.Model, tea.Cmd) {
	return m.enterRegistryBrowser(modeRecordList)
}

// isLeafSubscribed returns true if a leaf subscription already exists for the
// given network name.
func (m Model) isLeafSubscribed(network string) bool {
	if m.configs == nil {
		return false
	}
	for _, l := range m.configs.V3Net.Leaves {
		if l.Network == network {
			return true
		}
	}
	return false
}

// clampRegBrowserScroll ensures the cursor is visible in the registry list.
func (m *Model) clampRegBrowserScroll() {
	visible := regBrowserListVisible
	if m.regBrowserScroll > m.regBrowserCursor {
		m.regBrowserScroll = m.regBrowserCursor
	}
	if m.regBrowserCursor >= m.regBrowserScroll+visible {
		m.regBrowserScroll = m.regBrowserCursor - visible + 1
	}
	if m.regBrowserScroll < 0 {
		m.regBrowserScroll = 0
	}
}
