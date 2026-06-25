package configeditor

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
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

// handleFTNEcholistMsg processes the echolist download result.
func (m Model) handleFTNEcholistMsg(msg ftnEcholistMsg) (tea.Model, tea.Cmd) {
	// If the user pressed ESC during the download they've already returned to
	// the wizard form; drop this late result instead of yanking them into the
	// area browser.
	if m.mode != modeFTNAreaDownloading {
		return m, nil
	}
	m.ftnAreaBrowserLoading = false

	if msg.err != nil {
		m.ftnAreaBrowserError = fmt.Sprintf("Download failed: %v", msg.err)
		m.ftnWizard.areasFetchErr = m.ftnAreaBrowserError
		m.mode = modeFTNAreaBrowser
		return m, nil
	}

	// Populate wizard state.
	m.ftnWizard.availableAreas = msg.areas
	m.ftnWizard.areasFetched = true
	m.ftnWizard.areasFetchErr = ""

	// Preserve existing selections if re-downloading.
	if len(m.ftnWizard.selectedAreas) != len(msg.areas) {
		m.ftnWizard.selectedAreas = make([]bool, len(msg.areas))
	}

	// Copy to browser state.
	m.ftnAreaBrowserAreas = m.ftnWizard.availableAreas
	m.ftnAreaBrowserSelected = m.ftnWizard.selectedAreas
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
			if key == "R" && m.ftnWizard.echolistURL != "" {
				m.ftnAreaBrowserLoading = true
				m.ftnAreaBrowserError = ""
				m.mode = modeFTNAreaDownloading
				return m, fetchFTNEcholist(m.ftnWizard.echolistURL, m.ftnWizard.registryEntry)
			}
		}
		return m, nil
	}

	switch msg.Type {
	case tea.KeyUp:
		if m.ftnAreaBrowserCursor > 0 {
			m.ftnAreaBrowserCursor--
		}
		m.clampFTNAreaBrowserScroll()

	case tea.KeyDown:
		if m.ftnAreaBrowserCursor < total-1 {
			m.ftnAreaBrowserCursor++
		}
		m.clampFTNAreaBrowserScroll()

	case tea.KeyHome:
		m.ftnAreaBrowserCursor = 0
		m.clampFTNAreaBrowserScroll()

	case tea.KeyEnd:
		if total > 0 {
			m.ftnAreaBrowserCursor = total - 1
		}
		m.clampFTNAreaBrowserScroll()

	case tea.KeySpace:
		if total > 0 && m.ftnAreaBrowserCursor < total {
			m.ftnAreaBrowserSelected[m.ftnAreaBrowserCursor] = !m.ftnAreaBrowserSelected[m.ftnAreaBrowserCursor]
		}

	case tea.KeyEnter:
		// Confirm selection, copy back to wizard state, return.
		m.ftnWizard.selectedAreas = m.ftnAreaBrowserSelected
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

// clampFTNAreaBrowserScroll ensures the cursor is visible.
func (m *Model) clampFTNAreaBrowserScroll() {
	if m.ftnAreaBrowserCursor < m.ftnAreaBrowserScroll {
		m.ftnAreaBrowserScroll = m.ftnAreaBrowserCursor
	}
	if m.ftnAreaBrowserCursor >= m.ftnAreaBrowserScroll+ftnAreaBrowserListVisible {
		m.ftnAreaBrowserScroll = m.ftnAreaBrowserCursor - ftnAreaBrowserListVisible + 1
	}
}
