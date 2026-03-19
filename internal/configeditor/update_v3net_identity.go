package configeditor

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/keystore"
)

const (
	identityMain           = 0
	identityShowPhrase     = 1
	identityExportPrompt   = 2
	identityRecoverInput   = 3
	identityRecoverConfirm = 4
)

func (m Model) updateV3NetIdentity(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.identitySubState {
	case identityMain:
		return m.updateIdentityMain(msg)
	case identityShowPhrase:
		return m.updateIdentityShowPhrase(msg)
	case identityExportPrompt:
		return m.updateIdentityExportPrompt(msg)
	case identityRecoverInput:
		return m.updateIdentityRecoverInput(msg)
	case identityRecoverConfirm:
		return m.updateIdentityRecoverConfirm(msg)
	}
	return m, nil
}

func (m Model) updateIdentityMain(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := strings.ToUpper(msg.String())

	switch key {
	case "S":
		ks, err := m.loadIdentityKeystore()
		if err != nil {
			m.message = fmt.Sprintf("Error: %v", err)
			return m, nil
		}
		if ks == nil {
			m.message = "No V3Net key file found — run the V3Net setup wizard first, or use [R]ecover"
			return m, nil
		}
		phrase, err := ks.Mnemonic()
		if err != nil {
			m.message = fmt.Sprintf("Error: %v", err)
			return m, nil
		}
		m.identityPhrase = phrase
		m.identitySubState = identityShowPhrase
		return m, nil

	case "E":
		ks, err := m.loadIdentityKeystore()
		if err != nil {
			m.message = fmt.Sprintf("Error: %v", err)
			return m, nil
		}
		if ks == nil {
			m.message = "No V3Net key file found — run the V3Net setup wizard first, or use [R]ecover"
			return m, nil
		}
		phrase, err := ks.Mnemonic()
		if err != nil {
			m.message = fmt.Sprintf("Error: %v", err)
			return m, nil
		}
		m.identityPhrase = phrase
		m.identitySubState = identityExportPrompt
		m.textInput.SetValue("v3net-recovery.txt")
		m.textInput.CharLimit = 80
		m.textInput.Width = 40
		m.textInput.CursorEnd()
		m.textInput.Focus()
		return m, textinput.Blink

	case "R":
		m.identitySubState = identityRecoverInput
		m.identityRecoverInput = ""
		m.textInput.SetValue("")
		m.textInput.CharLimit = 500
		m.textInput.Width = 60
		m.textInput.Placeholder = "Enter 24 words separated by spaces"
		m.textInput.Focus()
		return m, textinput.Blink

	case "Q":
		m.identitySubState = identityMain
		m.identityPhrase = ""
		m.mode = m.backMode()
		return m, nil
	}

	if msg.Type == tea.KeyEscape {
		m.identitySubState = identityMain
		m.identityPhrase = ""
		m.mode = m.backMode()
		return m, nil
	}

	return m, nil
}

func (m Model) updateIdentityShowPhrase(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Any key returns to main identity screen.
	m.identitySubState = identityMain
	m.identityPhrase = ""
	return m, nil
}

func (m Model) updateIdentityExportPrompt(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEnter:
		path := m.textInput.Value()
		m.textInput.Blur()

		if strings.Contains(path, "..") {
			m.message = "Path must not contain '..'"
			m.identitySubState = identityMain
			return m, nil
		}

		ks, ksErr := m.loadIdentityKeystore()
		if ksErr != nil {
			m.message = fmt.Sprintf("Error: %v", ksErr)
			m.identitySubState = identityMain
			return m, nil
		}
		if ks == nil {
			m.message = "No V3Net key file found — run the V3Net setup wizard first, or use [R]ecover"
			m.identitySubState = identityMain
			return m, nil
		}

		if _, statErr := os.Stat(path); statErr == nil {
			m.message = fmt.Sprintf("File %q already exists — choose a different name", path)
			m.identitySubState = identityMain
			return m, nil
		}

		if err := m.writeRecoveryFile(path, ks); err != nil {
			m.message = fmt.Sprintf("Export error: %v", err)
			m.identitySubState = identityMain
			return m, nil
		}

		m.message = fmt.Sprintf("Saved to %s — move off-server and delete the local copy", path)
		m.identitySubState = identityMain
		m.identityPhrase = ""
		return m, nil

	case tea.KeyEscape:
		m.textInput.Blur()
		m.identitySubState = identityMain
		return m, nil

	default:
		var cmd tea.Cmd
		m.textInput, cmd = m.textInput.Update(msg)
		return m, cmd
	}
}

func (m Model) updateIdentityRecoverInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEnter:
		phrase := m.textInput.Value()
		m.textInput.Blur()

		recovered, err := keystore.FromMnemonic(phrase)
		if err != nil {
			m.message = fmt.Sprintf("Invalid: %v", err)
			m.identitySubState = identityMain
			return m, nil
		}

		m.identityRecoverInput = phrase
		m.identityRecoverNodeID = recovered.NodeID()
		m.identitySubState = identityRecoverConfirm
		return m, nil

	case tea.KeyEscape:
		m.textInput.Blur()
		m.identitySubState = identityMain
		return m, nil

	default:
		var cmd tea.Cmd
		m.textInput, cmd = m.textInput.Update(msg)
		return m, cmd
	}
}

func (m Model) updateIdentityRecoverConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := strings.ToUpper(msg.String())

	switch key {
	case "Y":
		path := m.configs.V3Net.KeystorePath
		if path == "" {
			path = "data/v3net.key"
		}

		if _, err := keystore.RecoverToFile(m.identityRecoverInput, path); err != nil {
			m.message = fmt.Sprintf("Recovery error: %v", err)
		} else {
			m.message = fmt.Sprintf("Identity recovered. Node ID: %s. Restart BBS to activate.", m.identityRecoverNodeID)
		}

		m.identityRecoverInput = ""
		m.identityRecoverNodeID = ""
		m.identitySubState = identityMain
		return m, nil

	case "N":
		m.identityRecoverInput = ""
		m.identityRecoverNodeID = ""
		m.identitySubState = identityMain
		return m, nil
	}

	if msg.Type == tea.KeyEscape {
		m.identityRecoverInput = ""
		m.identityRecoverNodeID = ""
		m.identitySubState = identityMain
		return m, nil
	}

	return m, nil
}
