package configeditor

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// hubStepAreas is the only step constant still used (for the areas sub-form).
const hubStepAreas = 3

// updateHubWizardStep handles the hub areas sub-form (the only remaining
// modeV3NetWizardStep usage). All other hub fields are now in the wizard form.
func (m Model) updateHubWizardStep(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Escape returns to the wizard form (not the fork screen).
	if msg.Type == tea.KeyEscape && !m.wizard.areaAdding {
		m.wizardFields = m.fieldsHubWizard()
		m.mode = modeWizardForm
		return m, nil
	}

	if m.wizard.step == hubStepAreas {
		return m.updateHubStepAreas(msg)
	}
	return m, nil
}

func (m Model) updateHubStepAreas(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Sub-form open: collect tag then name.
	if m.wizard.areaAdding {
		return m.updateHubAreaSubForm(msg)
	}

	switch msg.Type {
	case tea.KeyEnter:
		// Return to wizard form; save happens via S key there.
		m.wizardFields = m.fieldsHubWizard()
		m.mode = modeWizardForm
		return m, nil
	case tea.KeyUp:
		if m.wizard.areaCursor > 0 {
			m.wizard.areaCursor--
		}
	case tea.KeyDown:
		if m.wizard.areaCursor < len(m.wizard.areas)-1 {
			m.wizard.areaCursor++
		}
	case tea.KeyRunes:
		switch strings.ToUpper(string(msg.Runes)) {
		case "A":
			m.wizard.areaAdding = true
			m.wizard.areaEditTag = ""
			m.wizard.areaEditName = ""
			m.textInput.Reset()
			m.textInput.Focus()
		case "D":
			if len(m.wizard.areas) > 0 {
				i := m.wizard.areaCursor
				m.wizard.areas = append(m.wizard.areas[:i], m.wizard.areas[i+1:]...)
				if m.wizard.areaCursor >= len(m.wizard.areas) && m.wizard.areaCursor > 0 {
					m.wizard.areaCursor--
				}
			}
		}
	}
	return m, nil
}

func (m Model) updateHubAreaSubForm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyEscape {
		m.wizard.areaAdding = false
		m.textInput.Reset()
		return m, nil
	}
	if msg.Type == tea.KeyEnter {
		val := strings.TrimSpace(m.textInput.Value())
		if m.wizard.areaEditTag == "" {
			// Committing tag.
			if err := protocol.ValidateAreaTag(val); err != nil {
				m.message = err.Error()
				return m, nil
			}
			m.wizard.areaEditTag = val
			m.textInput.Reset()
			m.textInput.Focus()
			return m, nil
		}
		// Committing name.
		if val == "" {
			m.message = "Area name cannot be empty"
			return m, nil
		}
		m.wizard.areas = append(m.wizard.areas, wizardArea{
			Tag:  m.wizard.areaEditTag,
			Name: val,
		})
		m.wizard.areaCursor = len(m.wizard.areas) - 1
		m.wizard.areaAdding = false
		m.wizard.areaEditTag = ""
		m.wizard.areaEditName = ""
		m.textInput.Reset()
		return m, nil
	}
	m = m.updateWizardTextInput(msg)
	if m.wizard.areaEditTag != "" {
		m.wizard.areaEditName = m.textInput.Value()
	}
	return m, nil
}

// confirmHubWizard creates the hub network configuration and saves.
func (m Model) confirmHubWizard() (Model, tea.Cmd) {
	port, _ := strconv.Atoi(m.wizard.port) // Safe: validated by field Set.

	var initialAreas []config.V3NetHubArea
	for _, a := range m.wizard.areas {
		initialAreas = append(initialAreas, config.V3NetHubArea{Tag: a.Tag, Name: a.Name})
	}

	path := m.configs.V3Net.KeystorePath
	if path == "" {
		path = "data/v3net.key"
	}
	_, statErr := os.Stat(path)
	m.keyExistedBeforeSave = statErr == nil

	m.configs.V3Net.Enabled = true
	if m.configs.V3Net.KeystorePath == "" {
		m.configs.V3Net.KeystorePath = "data/v3net.key"
	}
	if m.configs.V3Net.DedupDBPath == "" {
		m.configs.V3Net.DedupDBPath = "data/v3net_dedup.sqlite"
	}
	m.configs.V3Net.Hub = config.V3NetHubConfig{
		Enabled:     true,
		Port:        port,
		DataDir:     "data/v3net_hub",
		AutoApprove: m.wizard.autoApprove,
		Networks: []config.V3NetHubNetwork{
			{Name: m.wizard.netName, Description: m.wizard.netDesc},
		},
		InitialAreas: initialAreas,
	}

	// Create local message areas for each initial hub area.
	m.createHubMessageAreas(m.wizard.netName, m.wizard.areas)

	// Create a self-leaf subscription so the hub's own BBS
	// participates in the network (syncs messages via localhost).
	m.addSelfLeaf(m.wizard.netName, port)

	m.dirty = true
	m.saveAll()
	if strings.HasPrefix(m.message, "SAVE ERROR") {
		return m, nil
	}
	m.message = "Hub saved. Start BBS to initialize."
	m.recordCursor = len(m.configs.V3Net.Hub.Networks) - 1
	m.recordScroll = 0
	if !m.keyExistedBeforeSave {
		ks, err := m.loadIdentityKeystore()
		if err == nil && ks != nil {
			if phrase, err := ks.Mnemonic(); err == nil {
				m.showSeedInterstitial = true
				m.seedInterstitialPhrase = phrase
				m.seedInterstitialNodeID = ks.NodeID()
				return m, nil
			}
		}
	}
	m.mode = modeRecordList
	return m, nil
}

// createHubMessageAreas adds a v3net message area for each initial hub area
// that does not already exist in the message area list.
func (m *Model) createHubMessageAreas(network string, areas []wizardArea) {
	existing := make(map[string]bool)
	for _, a := range m.configs.MsgAreas {
		existing[a.Tag] = true
	}

	for _, a := range areas {
		if existing[a.Tag] {
			continue
		}

		newID := 1
		maxPos := 0
		for _, ma := range m.configs.MsgAreas {
			if ma.ID >= newID {
				newID = ma.ID + 1
			}
			if ma.Position > maxPos {
				maxPos = ma.Position
			}
		}

		m.configs.MsgAreas = append(m.configs.MsgAreas, message.MessageArea{
			ID:       newID,
			Position: maxPos + 1,
			Tag:      a.Tag,
			Name:     a.Name,
			AreaType: "v3net",
			Network:  network,
			EchoTag:  a.Tag,
			AutoJoin: true,
			ACSRead:  "s10",
			ACSWrite: "s20",
			BasePath: fmt.Sprintf("msgbases/area_%d", newID),
		})
		existing[a.Tag] = true
	}
}

// addSelfLeaf appends a leaf subscription to the hub's own network
// via localhost, unless one already exists.
func (m *Model) addSelfLeaf(network string, port int) {
	hubURL := fmt.Sprintf("http://localhost:%d", port)
	for _, l := range m.configs.V3Net.Leaves {
		if l.Network == network && l.HubURL == hubURL {
			return
		}
	}
	m.configs.V3Net.Leaves = append(m.configs.V3Net.Leaves, config.V3NetLeafConfig{
		HubURL:       hubURL,
		Network:      network,
		Board:        network,
		PollInterval: "5m",
	})
}
