package configeditor

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

// --- Wizard Form Mode (field navigation) ---

func (m Model) updateWizardForm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if len(m.wizardFields) == 0 {
		if msg.Type == tea.KeyEscape {
			m.mode = modeRecordList
		}
		return m, nil
	}

	switch msg.Type {
	case tea.KeyTab, tea.KeyEnter:
		f := m.wizardFields[m.editField]
		if f.Type == ftYesNo {
			m.toggleWizardYesNo(f)
			return m, nil
		}
		// Hub wizard "Initial Areas" field opens the areas sub-form.
		if f.Type == ftDisplay && m.wizard.flow == "hub" && f.Label == "Initial Areas" {
			m.wizard.step = hubStepAreas
			m.wizard.areaAdding = false
			m.mode = modeV3NetWizardStep
			return m, nil
		}
		if f.Type == ftDisplay {
			m.editField = m.nextWizardEditableField(1)
			m.clampFieldScroll(m.wizardFields)
			return m, nil
		}
		return m.startWizardFieldEdit()

	case tea.KeySpace:
		f := m.wizardFields[m.editField]
		if f.Type == ftYesNo {
			m.toggleWizardYesNo(f)
		}
		return m, nil

	case tea.KeyDown:
		m.editField = m.nextWizardEditableField(1)
		m.clampFieldScroll(m.wizardFields)

	case tea.KeyUp:
		m.editField = m.nextWizardEditableField(-1)
		m.clampFieldScroll(m.wizardFields)

	case tea.KeyEscape:
		m.mode = modeRecordList
		return m, nil

	case tea.KeyPgDown:
		// Submit the form on PgDn (same key as "next record" in record editor).
		return m.submitWizardForm()

	default:
		key := strings.ToUpper(msg.String())
		if key == "S" {
			return m.submitWizardForm()
		}
	}
	return m, nil
}

// --- Wizard Form Field Editing Mode ---

func (m Model) updateWizardField(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	f := m.wizardFields[m.editField]

	switch msg.Type {
	case tea.KeyEnter, tea.KeyTab, tea.KeyDown:
		if err := m.applyWizardFieldValue(f); err != nil {
			m.message = fmt.Sprintf("Invalid: %v", err)
			return m, nil
		}
		m.textInput.Blur()
		m.mode = modeWizardForm
		m.editField = m.nextWizardEditableField(1)
		m.clampFieldScroll(m.wizardFields)

		// Auto-fetch network list after setting Hub URL in leaf wizard.
		if m.wizard.flow == "leaf" && f.Label == "Hub URL" && m.wizard.hubURL != "" {
			return m, fetchHubNetworks(m.wizard.hubURL)
		}
		return m, nil

	case tea.KeyUp:
		if err := m.applyWizardFieldValue(f); err != nil {
			m.message = fmt.Sprintf("Invalid: %v", err)
			return m, nil
		}
		m.textInput.Blur()
		m.mode = modeWizardForm
		m.editField = m.nextWizardEditableField(-1)
		m.clampFieldScroll(m.wizardFields)
		return m, nil

	case tea.KeyEscape:
		m.textInput.Blur()
		m.mode = modeWizardForm
		return m, nil

	default:
		if f.Type == ftYesNo {
			if len(msg.Runes) == 1 {
				ch := msg.Runes[0]
				if ch == 'y' || ch == 'Y' {
					m.textInput.SetValue("Y")
				} else if ch == 'n' || ch == 'N' {
					m.textInput.SetValue("N")
				}
				if err := m.applyWizardFieldValue(f); err == nil {
					m.textInput.Blur()
					m.mode = modeWizardForm
					m.editField = m.nextWizardEditableField(1)
					m.clampFieldScroll(m.wizardFields)
				}
				return m, nil
			}
			return m, nil
		}

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

func (m Model) nextWizardEditableField(dir int) int {
	n := len(m.wizardFields)
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

func (m *Model) toggleWizardYesNo(f fieldDef) {
	if f.Get != nil && f.Set != nil {
		if f.Get() == "Y" {
			f.Set("N")
		} else {
			f.Set("Y")
		}
		m.message = ""
	}
}

func (m Model) startWizardFieldEdit() (Model, tea.Cmd) {
	f := m.wizardFields[m.editField]
	if f.Type == ftDisplay {
		return m, nil
	}

	val := f.Get()
	m.mode = modeWizardField
	m.textInput.SetValue(val)
	m.textInput.CharLimit = f.Width
	m.textInput.Width = f.Width
	m.textInput.EchoMode = textinput.EchoNormal
	m.textInput.Placeholder = ""
	m.textInput.CursorEnd()
	m.textInput.Focus()

	return m, textinput.Blink
}

func (m *Model) applyWizardFieldValue(f fieldDef) error {
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
	case ftYesNo:
		upper := strings.ToUpper(val)
		if upper != "Y" && upper != "N" {
			return fmt.Errorf("must be Y or N")
		}
		val = upper
	}

	if f.Set != nil {
		if err := f.Set(val); err != nil {
			return err
		}
		m.message = ""
	}
	return nil
}

// confirmLeafWizard creates the leaf subscription and saves.
func (m Model) confirmLeafWizard() (Model, tea.Cmd) {
	// Duplicate check.
	for _, l := range m.configs.V3Net.Leaves {
		if l.HubURL == m.wizard.hubURL && l.Network == m.wizard.networkName {
			m.message = "Already subscribed to this network on this hub"
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
	if strings.HasPrefix(m.message, "SAVE ERROR") {
		return m, nil
	}
	m.message = "Leaf saved. Restart BBS to activate."
	m.recordCursor = len(m.configs.V3Net.Leaves) - 1
	m.recordScroll = 0
	m.mode = modeRecordList
	return m, nil
}

// submitWizardForm validates and saves the wizard form.
func (m Model) submitWizardForm() (Model, tea.Cmd) {
	switch m.wizard.flow {
	case "leaf":
		if err := m.validateLeafWizard(); err != nil {
			m.message = err.Error()
			return m, nil
		}
		return m.confirmLeafWizard()
	case "hub":
		if err := m.validateHubWizard(); err != nil {
			m.message = err.Error()
			return m, nil
		}
		if len(m.wizard.areas) == 0 {
			m.message = "At least one initial area is required"
			return m, nil
		}
		return m.confirmHubWizard()
	}
	return m, nil
}
