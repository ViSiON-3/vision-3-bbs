package configeditor

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ViSiON-3/vision-3-bbs/internal/ftn"
)

// ftnAreaBrowserListVisible is the number of rows visible in the FTN area browser.
const ftnAreaBrowserListVisible = 12

// updateFTNAreaDownloading handles key events while the echolist is downloading.
func (m Model) updateFTNAreaDownloading(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyEscape {
		m.ftnAreaBrowserLoading = false
		m.mode = modeFTNWizardForm
	}
	return m, nil
}

// handleFTNEcholistMsg processes the echolist or file echo list download
// result.
func (m Model) handleFTNEcholistMsg(msg ftnEcholistMsg) (tea.Model, tea.Cmd) {
	// If the user pressed ESC during the download they've already returned to
	// the wizard form; drop this late result instead of yanking them into the
	// area browser. Likewise a late result for the other list, or for a
	// network the sysop has since moved off (cancelled, picked another, and
	// started its download): its areas must not be saved under this one.
	if m.mode != modeFTNAreaDownloading || msg.fileEchoes != m.ftnAreaBrowserFiles ||
		msg.generation != m.ftnAreaBrowserGeneration || msg.url != m.ftnWizard.listURL(msg.fileEchoes) {
		return m, nil
	}
	m.ftnAreaBrowserLoading = false

	if msg.err != nil {
		m.ftnAreaBrowserError = fmt.Sprintf("Download failed: %v", msg.err)
		if !msg.fileEchoes {
			m.ftnWizard.areasFetchErr = m.ftnAreaBrowserError
		}
		m.mode = modeFTNAreaBrowser
		return m, nil
	}

	// Populate wizard state.
	l := m.ftnWizard.list(msg.fileEchoes)
	*l.available = msg.areas
	*l.fetched = true
	if !msg.fileEchoes {
		m.ftnWizard.areasFetchErr = ""
	}

	// Preserve existing selections if re-downloading.
	if len(*l.selected) != len(msg.areas) {
		*l.selected = make([]bool, len(msg.areas))

		// Editing an existing network: start from what is actually
		// configured, so the list shows the current subscriptions rather
		// than an empty slate the sysop would have to re-tick from memory.
		for i, area := range msg.areas {
			if l.existing[strings.ToUpper(area.Tag)] {
				(*l.selected)[i] = true
			}
		}
	}

	// Copy to browser state. The selection must be a distinct copy so that
	// toggling in the browser and then pressing ESC discards the changes
	// (confirm writes the browser selection back to the wizard explicitly).
	m.ftnAreaBrowserAreas = *l.available
	m.ftnAreaBrowserSelected = append([]bool(nil), *l.selected...)
	m.ftnAreaBrowserCursor = 0
	m.ftnAreaBrowserScroll = 0
	m.ftnAreaBrowserError = ""
	m.mode = modeFTNAreaBrowser
	return m, nil
}

// updateFTNAreaBrowser handles key events in the FTN echo area browser.
func (m Model) updateFTNAreaBrowser(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	total := len(m.ftnAreaBrowserAreas)

	// If error state with no areas, only allow ESC or R.
	if m.ftnAreaBrowserError != "" && total == 0 {
		switch msg.Type {
		case tea.KeyEscape:
			m.mode = modeFTNWizardForm
			return m, nil
		default:
			key := strings.ToUpper(msg.String())
			if key == "R" {
				if cmd := m.ftnWizardListFetch(m.ftnAreaBrowserFiles); cmd != nil {
					m.ftnAreaBrowserLoading = true
					m.ftnAreaBrowserError = ""
					m.mode = modeFTNAreaDownloading
					return m, cmd
				}
			}
		}
		return m, nil
	}

	if cursor, ok := listNavKey(msg, m.ftnAreaBrowserCursor, total); ok {
		m.ftnAreaBrowserCursor = cursor
		m.ftnAreaBrowserScroll = clampListScroll(cursor, m.ftnAreaBrowserScroll, ftnAreaBrowserListVisible)
		return m, nil
	}

	switch msg.Type {
	case tea.KeySpace:
		if total > 0 && m.ftnAreaBrowserCursor < total {
			m.ftnAreaBrowserSelected[m.ftnAreaBrowserCursor] = !m.ftnAreaBrowserSelected[m.ftnAreaBrowserCursor]
		}

	case tea.KeyEnter:
		// Confirm selection, copy back to wizard state, return.
		*m.ftnWizard.list(m.ftnAreaBrowserFiles).selected = append([]bool(nil), m.ftnAreaBrowserSelected...)
		m.ftnWizardFields = m.fieldsFTNWizard() // refresh display
		m.mode = modeFTNWizardForm
		return m, nil

	case tea.KeyEscape:
		// Discard changes, return to wizard.
		m.mode = modeFTNWizardForm
		return m, nil

	default:
		key := strings.ToUpper(msg.String())
		switch key {
		case "A":
			// Select all.
			for i := range m.ftnAreaBrowserSelected {
				m.ftnAreaBrowserSelected[i] = true
			}
		case "N":
			// Deselect all.
			for i := range m.ftnAreaBrowserSelected {
				m.ftnAreaBrowserSelected[i] = false
			}
		}
	}
	return m, nil
}

// ftnWizardListFetch starts a fresh download attempt, clearing only the browser
// state so a failure cannot display entries from a previously browsed list.
// The generation lives on Model so it remains unique across wizard sessions.
func (m *Model) ftnWizardListFetch(fileEchoes bool) tea.Cmd {
	w := m.ftnWizard
	url := w.listURL(fileEchoes)
	if !ftn.EcholistIsDownloadable(url) {
		return nil
	}
	m.ftnAreaBrowserGeneration++
	m.ftnAreaBrowserAreas = nil
	m.ftnAreaBrowserSelected = nil
	m.ftnAreaBrowserCursor = 0
	m.ftnAreaBrowserScroll = 0
	if fileEchoes {
		return fetchFTNFileEchoList(url, m.ftnAreaBrowserGeneration)
	}
	return fetchFTNEcholist(url, w.registryEntry, m.ftnAreaBrowserGeneration)
}
