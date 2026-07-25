package configeditor

import (
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ViSiON-3/vision-3-bbs/internal/ftn"
)

// startFTNNodeLookup validates the wizard state and either applies a
// cached nodelist lookup synchronously or starts the async download.
func (m Model) startFTNNodeLookup() (Model, tea.Cmd) {
	w := m.ftnWizard

	if w.nodelistURL == "" {
		m.message = "No nodelist available for this network"
		return m, nil
	}
	addr, err := ftn.ParseAddress(w.ownAddress)
	if err != nil {
		m.message = "Enter your FTN address first"
		return m, nil
	}
	if w.zone > 0 && addr.Zone != w.zone {
		m.message = fmt.Sprintf("Address zone %d doesn't match network zone %d", addr.Zone, w.zone)
		return m, nil
	}

	// Cached from an earlier lookup: no need to re-download.
	if w.nodelist != nil {
		m.applyFTNNodeLookup()
		return m, nil
	}

	w.lookupLoading = true
	w.lookupErr = ""
	m.mode = modeFTNNodelistLookup
	ctx, cancel := context.WithCancel(context.Background())
	w.lookupCancel = cancel
	return m, fetchFTNNodelist(ctx, w.nodelistURL)
}

// applyFTNNodeLookup runs the lookup against the cached nodelist and, on
// success, overwrites the hub fields (an explicit lookup is an explicit
// request to replace whatever was there).
func (m *Model) applyFTNNodeLookup() {
	w := m.ftnWizard

	addr, err := ftn.ParseAddress(w.ownAddress)
	if err != nil {
		m.message = "Enter your FTN address first"
		return
	}
	dnsSuffix := ""
	if w.registryEntry != nil {
		dnsSuffix = w.registryEntry.DNSSuffix
	}
	res, err := w.nodelist.Lookup(addr, dnsSuffix)
	if err != nil {
		w.lookupResult = nil
		w.lookupErr = err.Error()
		m.message = "Nodelist lookup failed: " + err.Error()
		return
	}

	w.lookupResult = res
	w.lookupErr = ""
	w.hubAddress = res.Uplink.Address.String()
	w.hubHostname = res.Hostname
	w.hubPort = res.Port
	w.hubAutofilled = true

	if res.Self != nil {
		m.message = fmt.Sprintf("Found %s — hub %s (%s:%d)",
			res.Self.Name, w.hubAddress, w.hubHostname, w.hubPort)
	} else {
		m.message = fmt.Sprintf("Node not listed yet — hub %s (%s:%d) inferred from net %d",
			w.hubAddress, w.hubHostname, w.hubPort, addr.Net)
	}
	m.ftnWizardFields = m.fieldsFTNWizard()
}

// updateFTNNodelistLookup handles keys while the nodelist download runs.
func (m Model) updateFTNNodelistLookup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyEscape {
		if m.ftnWizard.lookupCancel != nil {
			m.ftnWizard.lookupCancel()
			m.ftnWizard.lookupCancel = nil
		}
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
	// A stale result from a network we've since navigated away from: drop it
	// before touching any state.
	if msg.url != m.ftnWizard.nodelistURL {
		return m, nil
	}
	m.ftnWizard.lookupLoading = false
	m.ftnWizard.lookupCancel = nil
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
