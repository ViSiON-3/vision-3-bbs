package configeditor

import (
	"fmt"
	"os"
	"strings"
	"time"

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
			m.message = "No V3Net key file found"
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
			m.message = "No V3Net key file found"
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
			m.message = "No V3Net key file found"
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

// loadIdentityKeystore loads the keystore for display purposes.
// Returns (nil, nil) if the key file does not exist.
func (m Model) loadIdentityKeystore() (*keystore.Keystore, error) {
	path := m.configs.V3Net.KeystorePath
	if path == "" {
		path = "data/v3net.key"
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, nil
	}
	ks, _, err := keystore.Load(path)
	if err != nil {
		return nil, fmt.Errorf("key file corrupt: %w", err)
	}
	return ks, nil
}

func (m Model) updateSeedInterstitial(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := strings.ToUpper(msg.String())
	switch key {
	case "E":
		ks, _ := m.loadIdentityKeystore()
		if ks != nil {
			if err := m.writeRecoveryFile("v3net-recovery.txt", ks); err != nil {
				m.message = fmt.Sprintf("Export error: %v", err)
			} else {
				m.message = "Saved to v3net-recovery.txt — move off-server and delete the local copy"
			}
		}
		m.showSeedInterstitial = false
		m.seedInterstitialPhrase = ""
		m.mode = modeRecordList
		return m, nil
	default:
		m.showSeedInterstitial = false
		m.seedInterstitialPhrase = ""
		m.mode = modeRecordList
		return m, nil
	}
}

// writeRecoveryFile writes the seed phrase export file.
func (m Model) writeRecoveryFile(path string, ks *keystore.Keystore) error {
	phrase, err := ks.Mnemonic()
	if err != nil {
		return err
	}

	words := strings.Split(phrase, " ")
	if len(words) != 24 {
		return fmt.Errorf("unexpected word count: %d", len(words))
	}

	var b strings.Builder
	b.WriteString("V3Net Recovery Seed Phrase\n")
	b.WriteString("==========================\n")
	b.WriteString(fmt.Sprintf("Node ID: %s\n", ks.NodeID()))
	b.WriteString(fmt.Sprintf("Generated: %s\n", time.Now().UTC().Format("2006-01-02")))
	b.WriteString("\nWords:\n")

	// 4 columns x 6 rows
	for row := 0; row < 6; row++ {
		b.WriteString(fmt.Sprintf("  %2d. %-12s %2d. %-12s %2d. %-12s %2d. %-12s\n",
			row+1, words[row],
			row+7, words[row+6],
			row+13, words[row+12],
			row+19, words[row+18],
		))
	}

	b.WriteString("\nStore this file safely and delete it from this server.\n")
	b.WriteString("Anyone with these words can impersonate your BBS node.\n")

	return os.WriteFile(path, []byte(b.String()), 0600)
}
