package configeditor

import (
	"fmt"
	"os"
	"path/filepath"
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

// loadOrCreateIdentityKeystore loads or generates the keystore. Unlike
// loadIdentityKeystore, this creates a new keypair when the file is absent
// (used after first-time wizard saves to show the seed interstitial).
func (m Model) loadOrCreateIdentityKeystore() (*keystore.Keystore, error) {
	path := m.configs.V3Net.KeystorePath
	if path == "" {
		path = "data/v3net.key"
	}
	ks, _, err := keystore.Load(path)
	if err != nil {
		return nil, fmt.Errorf("key file: %w", err)
	}
	return ks, nil
}

// updateSeedInterstitial handles the seed phrase interstitial shown after
// first-time wizard save.
func (m Model) updateSeedInterstitial(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := strings.ToUpper(msg.String())
	switch key {
	case "E":
		exportPath := "v3net-recovery.txt"
		if _, statErr := os.Stat(exportPath); statErr == nil {
			m.message = fmt.Sprintf("File %q already exists — use the Identity screen to export", exportPath)
		} else {
			ks, err := m.loadIdentityKeystore()
			if err != nil {
				m.message = fmt.Sprintf("Export error: %v", err)
			} else if ks != nil {
				if err := m.writeRecoveryFile(exportPath, ks); err != nil {
					m.message = fmt.Sprintf("Export error: %v", err)
				} else {
					absPath, _ := filepath.Abs(exportPath)
					m.message = fmt.Sprintf("Saved to %s — move off-server and delete the local copy", absPath)
				}
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

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".v3net-recovery-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()

	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if _, err := tmp.WriteString(b.String()); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("sync temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("rename %s -> %s: %w", tmpName, path, err)
	}
	return nil
}
