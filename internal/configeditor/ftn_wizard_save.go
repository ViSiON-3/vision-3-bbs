package configeditor

import (
	"fmt"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/ftn"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
)

// confirmFTNWizard creates FTN network, link, conference, areas, and
// updates binkd.conf atomically.
func (m Model) confirmFTNWizard() (Model, tea.Cmd) {
	w := m.ftnWizard

	// Derive network key (lowercase).
	netKey := strings.ToLower(w.networkName)

	// Duplicate check.
	if m.configs.FTN.Networks != nil {
		if _, exists := m.configs.FTN.Networks[netKey]; exists {
			m.message = fmt.Sprintf("Network %q already exists in ftn.json", netKey)
			return m, nil
		}
	}

	// 1. Create FTN network entry.
	if m.configs.FTN.Networks == nil {
		m.configs.FTN.Networks = make(map[string]config.FTNNetworkConfig)
	}

	// Set default paths if empty (first FTN network).
	if m.configs.FTN.InboundPath == "" {
		m.configs.FTN.InboundPath = "data/ftn/in"
	}
	if m.configs.FTN.SecureInboundPath == "" {
		m.configs.FTN.SecureInboundPath = "data/ftn/secure_in"
	}
	if m.configs.FTN.OutboundPath == "" {
		m.configs.FTN.OutboundPath = "data/ftn/outbound"
	}
	if m.configs.FTN.BinkdOutboundPath == "" {
		m.configs.FTN.BinkdOutboundPath = "data/ftn/binkd_outbound"
	}
	if m.configs.FTN.TempPath == "" {
		m.configs.FTN.TempPath = "data/ftn/temp"
	}
	if m.configs.FTN.DupeDBPath == "" {
		m.configs.FTN.DupeDBPath = "data/ftn/dupes.json"
	}

	link := config.FTNLinkConfig{
		Address:         w.hubAddress,
		PacketPassword:  w.packetPassword,
		SessionPassword: w.sessionPassword,
		AreafixPassword: w.areafixPassword,
		Name:            w.networkName + " Hub",
		Flavour:         "Crash",
	}

	m.configs.FTN.Networks[netKey] = config.FTNNetworkConfig{
		InternalTosserEnabled: true,
		OwnAddress:            w.ownAddress,
		PollSeconds:           300,
		Tearline:              "ViSiON/3",
		Links:                 []config.FTNLinkConfig{link},
	}

	// 2. Create conference for the network.
	confID := m.findOrCreateNetworkConference(netKey)

	// Update the conference description if it was auto-created.
	for i, c := range m.configs.Conferences {
		if c.ID == confID && c.Description == netKey+" message network" {
			m.configs.Conferences[i].Name = w.networkName
			m.configs.Conferences[i].Description = w.networkDesc
		}
	}

	// 3. Create netmail area.
	m.createFTNMsgAreaIfNeeded(
		netKey+"_netmail",
		w.networkName+" Netmail",
		"netmail",
		netKey,
		"",
		w.ownAddress,
		confID,
		filepath.Join("msgbases", "fn."+netKey+"_netmail"),
	)

	// 4. Create message areas for each selected echo.
	for i, sel := range w.selectedAreas {
		if !sel || i >= len(w.availableAreas) {
			continue
		}
		area := w.availableAreas[i]
		areaTag := strings.ToLower(netKey + "_" + strings.ToLower(area.Tag))
		basePath := filepath.Join("msgbases", "fn."+strings.ToLower(area.Tag))

		desc := area.Description
		if desc == "" {
			desc = area.Tag
		}

		m.createFTNMsgAreaIfNeeded(
			areaTag,
			desc,
			"echomail",
			netKey,
			area.Tag,
			w.ownAddress,
			confID,
			basePath,
		)
	}

	// 5. Update binkd.conf.
	bbsRoot := filepath.Join(m.configPath, "..")
	absRoot, err := filepath.Abs(bbsRoot)
	if err != nil {
		absRoot = bbsRoot
	}
	binkdPath := filepath.Join(absRoot, "data", "ftn", "binkd.conf")

	binkdCfg := ftn.BinkdConfig{
		BBSRoot:   absRoot,
		BoardName: m.configs.Server.BoardName,
		SysopName: m.configs.Server.SysOpName,
		Location:  m.configs.Server.BBSLocation,
		Domains:   map[string]int{netKey: w.zone},
		Addresses: []string{fmt.Sprintf("%s@%s", w.ownAddress, netKey)},
		Node: ftn.BinkdNode{
			Address:     fmt.Sprintf("%s@%s", w.hubAddress, netKey),
			Hostname:    fmt.Sprintf("%s:%d", w.hubHostname, w.hubPort),
			SessionPwd:  w.sessionPassword,
			NetworkName: w.networkName,
		},
	}
	if err := ftn.UpdateBinkdConf(binkdPath, binkdCfg); err != nil {
		// Non-fatal: binkd.conf update is best-effort.
		m.message = fmt.Sprintf("Warning: binkd.conf update failed: %v (network saved OK)", err)
	}

	// 6. Save everything.
	m.dirty = true
	m.saveAll()
	if strings.HasPrefix(m.message, "SAVE ERROR") {
		return m, nil
	}

	selectedCount := w.selectedAreaCount()
	m.message = fmt.Sprintf("FTN network %q saved — %d area(s) created. Restart BBS to activate.", w.networkName, selectedCount)
	m.mode = modeCategoryMenu
	return m, nil
}

// createFTNMsgAreaIfNeeded creates a message area if one with the given tag
// doesn't already exist.
func (m *Model) createFTNMsgAreaIfNeeded(tag, name, areaType, network, echoTag, originAddr string, confID int, basePath string) {
	for _, ma := range m.configs.MsgAreas {
		if ma.Tag == tag {
			return
		}
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
		ID:           newID,
		Position:     maxPos + 1,
		Tag:          tag,
		Name:         name,
		AreaType:     areaType,
		Network:      network,
		EchoTag:      echoTag,
		OriginAddr:   originAddr,
		AutoJoin:     true,
		ACSRead:      "s10",
		ACSWrite:     "s20",
		BasePath:     basePath,
		ConferenceID: confID,
	})
}
