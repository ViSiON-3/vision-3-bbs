package configeditor

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// enterFTNWizard initializes the FTN setup wizard and transitions to the form.
func (m Model) enterFTNWizard() (Model, tea.Cmd) {
	origin := ""
	if m.configs != nil {
		origin = m.configs.Server.BoardName
		if host := m.configs.Server.SSHHost; host != "" {
			origin += " - " + host
		} else if host := m.configs.Server.TelnetHost; host != "" {
			origin += " - " + host
		}
	}

	m.ftnWizard = &ftnWizardState{
		hubPort:    24554,
		originLine: origin,
	}
	m.ftnWizardFields = m.fieldsFTNWizard()
	m.editField = 0
	m.fieldScroll = 0
	m.mode = modeFTNWizardForm
	return m, nil
}

// updateFTNWizardForm handles key events in the FTN wizard form navigation mode.
func (m Model) updateFTNWizardForm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if len(m.ftnWizardFields) == 0 {
		if msg.Type == tea.KeyEscape {
			m.mode = modeCategoryMenu
		}
		return m, nil
	}

	switch msg.Type {
	case tea.KeyTab, tea.KeyEnter:
		f := m.ftnWizardFields[m.editField]

		// "Network" field → open network browser.
		if f.Type == ftDisplay && f.Label == "Network" {
			return m.enterFTNNetworkBrowser()
		}

		// "Echo Areas" field → download echolist and open area browser.
		if f.Type == ftDisplay && f.Label == "Echo Areas" {
			return m.enterFTNAreaBrowser()
		}

		// Other display fields → just advance.
		if f.Type == ftDisplay {
			m.editField = m.nextFTNWizardField(1)
			m.clampFieldScroll(m.ftnWizardFields)
			return m, nil
		}

		// Editable field → start text input.
		return m.startFTNWizardFieldEdit()

	case tea.KeyDown:
		m.editField = m.nextFTNWizardField(1)
		m.clampFieldScroll(m.ftnWizardFields)

	case tea.KeyUp:
		m.editField = m.nextFTNWizardField(-1)
		m.clampFieldScroll(m.ftnWizardFields)

	case tea.KeyEscape:
		if m.ftnWizard.hasData() {
			m.confirmYes = true
			m.mode = modeWizardExitConfirm
			return m, nil
		}
		m.mode = modeCategoryMenu
		return m, nil

	case tea.KeyPgDown:
		return m.submitFTNWizardForm()

	default:
		key := strings.ToUpper(msg.String())
		if key == "S" {
			return m.submitFTNWizardForm()
		}
	}
	return m, nil
}

// updateFTNWizardField handles key events when editing a text field in the FTN wizard.
func (m Model) updateFTNWizardField(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	f := m.ftnWizardFields[m.editField]

	switch msg.Type {
	case tea.KeyEnter, tea.KeyTab, tea.KeyDown:
		if err := m.applyFTNWizardFieldValue(f); err != nil {
			m.message = fmt.Sprintf("Invalid: %v", err)
			return m, nil
		}
		m.textInput.Blur()
		m.mode = modeFTNWizardForm
		m.editField = m.nextFTNWizardField(1)
		m.clampFieldScroll(m.ftnWizardFields)
		return m, nil

	case tea.KeyUp:
		if err := m.applyFTNWizardFieldValue(f); err != nil {
			m.message = fmt.Sprintf("Invalid: %v", err)
			return m, nil
		}
		m.textInput.Blur()
		m.mode = modeFTNWizardForm
		m.editField = m.nextFTNWizardField(-1)
		m.clampFieldScroll(m.ftnWizardFields)
		return m, nil

	case tea.KeyEscape:
		m.textInput.Blur()
		m.mode = modeFTNWizardForm
		return m, nil

	default:
		if f.Type == ftInteger {
			if len(msg.Runes) == 1 {
				ch := msg.Runes[0]
				if (ch < '0' || ch > '9') && ch != '-' {
					return m, nil
				}
			}
		}

		var cmd tea.Cmd
		m.textInput, cmd = m.textInput.Update(msg)
		return m, cmd
	}
}

// nextFTNWizardField returns the next field index in the given direction,
// wrapping around.
func (m Model) nextFTNWizardField(dir int) int {
	n := len(m.ftnWizardFields)
	if n == 0 {
		return 0
	}
	idx := m.editField + dir
	if idx > n-1 {
		idx = 0
	} else if idx < 0 {
		idx = n - 1
	}
	return idx
}

// startFTNWizardFieldEdit begins text input for the current FTN wizard field.
func (m Model) startFTNWizardFieldEdit() (Model, tea.Cmd) {
	f := m.ftnWizardFields[m.editField]
	if f.Type == ftDisplay {
		return m, nil
	}

	val := f.Get()
	m.mode = modeFTNWizardField
	m.textInput.SetValue(val)
	m.textInput.CharLimit = f.Width
	m.textInput.Width = f.Width
	m.textInput.EchoMode = textinput.EchoNormal
	m.textInput.Placeholder = ""
	m.textInput.CursorEnd()
	m.textInput.Focus()

	return m, textinput.Blink
}

// applyFTNWizardFieldValue validates and applies the current text input value.
func (m *Model) applyFTNWizardFieldValue(f fieldDef) error {
	val := m.textInput.Value()

	switch f.Type {
	case ftInteger:
		n, err := strconv.Atoi(val)
		if err != nil {
			return fmt.Errorf("not a number")
		}
		if n < f.Min || n > f.Max {
			return fmt.Errorf("must be %d-%d", f.Min, f.Max)
		}
	}

	if f.Set != nil {
		if err := f.Set(val); err != nil {
			return err
		}
		m.message = ""
	}
	return nil
}

// submitFTNWizardForm validates the form and triggers save.
func (m Model) submitFTNWizardForm() (Model, tea.Cmd) {
	if err := m.validateFTNWizard(); err != nil {
		m.message = err.Error()
		return m, nil
	}
	if m.ftnWizard.selectedAreaCount() == 0 {
		m.message = "Select at least one echo area"
		return m, nil
	}
	return m.confirmFTNWizard()
}

// enterFTNAreaBrowser starts the echolist download or opens the browser
// if areas are already cached.
func (m Model) enterFTNAreaBrowser() (Model, tea.Cmd) {
	w := m.ftnWizard

	// If areas already fetched, go straight to browser.
	if w.areasFetched {
		m.ftnAreaBrowserAreas = w.availableAreas
		m.ftnAreaBrowserSelected = w.selectedAreas
		m.ftnAreaBrowserCursor = 0
		m.ftnAreaBrowserScroll = 0
		m.ftnAreaBrowserError = ""
		m.mode = modeFTNAreaBrowser
		return m, nil
	}

	// Need an echolist URL.
	url := w.echolistURL
	if url == "" {
		m.message = "No echolist URL available for this network"
		return m, nil
	}

	m.ftnAreaBrowserLoading = true
	m.ftnAreaBrowserError = ""
	m.mode = modeFTNAreaDownloading
	return m, fetchFTNEcholist(url, w.registryEntry)
}
