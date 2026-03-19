package configeditor

import (
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/keystore"
)

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

// updateSeedInterstitial handles the seed phrase interstitial shown after
// first-time wizard save.
func (m Model) updateSeedInterstitial(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := strings.ToUpper(msg.String())
	switch key {
	case "E":
		ks, err := m.loadIdentityKeystore()
		if err != nil {
			m.message = fmt.Sprintf("Export error: %v", err)
		} else if ks != nil {
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
