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
	editing := w.editing()
	if editing {
		// The name is fixed while editing, so the key the fields describe is
		// the key we loaded from.
		netKey = w.editingKey
	}

	// Adding a network that already exists would silently merge into it, so
	// refuse. Editing is the supported way to change one.
	if !editing && m.configs.FTN.Networks != nil {
		if _, exists := m.configs.FTN.Networks[netKey]; exists {
			m.message = fmt.Sprintf("Network %q already exists — go back and choose it to edit instead", netKey)
			return m, nil
		}
	}

	// 1. Create or update the FTN network entry.
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
		Hostname:        w.hubHostname,
		Port:            w.hubPort,
	}

	if existing, ok := m.configs.FTN.Networks[netKey]; ok && editing {
		// Keep the settings this wizard does not ask about. Tosser, poll
		// interval and origin are all editable under Echomail Networks, and
		// rewriting them with the wizard's create-time defaults would throw
		// away whatever the sysop set there.
		existing.OwnAddress = w.ownAddress
		existing.Links = replaceHubLink(existing.Links, link)
		m.configs.FTN.Networks[netKey] = existing

		// Areas carry the origin address they were created with, and
		// createFTNMsgAreaIfNeeded leaves existing ones alone. Without this,
		// editing the address updates ftn.json while every area keeps stamping
		// outbound mail with the old one.
		for i := range m.configs.MsgAreas {
			if strings.EqualFold(m.configs.MsgAreas[i].Network, netKey) {
				m.configs.MsgAreas[i].OriginAddr = w.ownAddress
			}
		}
	} else {
		m.configs.FTN.Networks[netKey] = config.FTNNetworkConfig{
			InternalTosserEnabled: true,
			OwnAddress:            w.ownAddress,
			PollSeconds:           300,
			// Origin left empty: echomail then falls back to the board name.
			// The tearline is not configurable — the software stamps it.
			Links: []config.FTNLinkConfig{link},
		}
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
		// Scope the msgbase dir by network too, so two networks carrying the
		// same echo tag don't share (and cross-contaminate) one message base.
		basePath := filepath.Join("msgbases", "fn."+strings.ToLower(netKey)+"_"+strings.ToLower(area.Tag))

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
	// Non-fatal: binkd.conf update is best-effort, but the operator has to be
	// told, because the final status below otherwise says "restart to
	// activate" for a mailer that never got the new details.
	binkdWarning := ""
	if err := ftn.UpdateBinkdConf(binkdPath, binkdCfg); err != nil {
		binkdWarning = fmt.Sprintf(" Warning: binkd.conf update failed: %v — fix it before restarting.", err)
	}

	// 6. Wire scheduler events for mail flow (hub poll + supporting events).
	wireFTNEvents(&m.configs.Events, netKey, w.hubAddress)

	// 7. Save everything.
	m.dirty = true
	m.saveAll()
	if strings.HasPrefix(m.message, "SAVE ERROR") {
		m.message += binkdWarning
		return m, nil
	}

	selectedCount := w.selectedAreaCount()
	if editing {
		// Areas are only ever added here. Removing one would mean deleting a
		// message base with real mail in it, which is not something to do as a
		// side effect of saving a form, so say so rather than quietly ignoring
		// the untick.
		dropped := w.unsubscribedTagCount()
		m.message = fmt.Sprintf("FTN network %q updated — %d area(s) subscribed. Restart BBS to activate.",
			netKey, selectedCount)
		if dropped > 0 {
			m.message = fmt.Sprintf("FTN network %q updated — %d area(s) subscribed. "+
				"%d existing area(s) left in place: remove them under Message Areas. Restart BBS to activate.",
				netKey, selectedCount, dropped)
		}
	} else if selectedCount == 0 {
		// Saving with no echoes is allowed (the echolist may be unavailable),
		// so point at where they get added rather than leaving the operator
		// wondering whether the save was incomplete.
		m.message = fmt.Sprintf("FTN network %q saved with netmail only — add echo areas under Message Areas "+
			"or re-run the wizard. Restart BBS to activate.", w.networkName)
	} else {
		m.message = fmt.Sprintf("FTN network %q saved — %d area(s) created. Restart BBS to activate.", w.networkName, selectedCount)
	}
	m.message += binkdWarning
	m.mode = modeCategoryMenu
	return m, nil
}

// replaceHubLink updates the hub entry in a link list, leaving every other link
// alone. The hub is the first link the wizard wrote; any links after it are
// downstream systems this node feeds, which the wizard does not manage and must
// not drop. Matching by address first keeps the right entry when the list has
// been reordered under Echomail Networks.
func replaceHubLink(links []config.FTNLinkConfig, hub config.FTNLinkConfig) []config.FTNLinkConfig {
	for i := range links {
		if strings.EqualFold(links[i].Address, hub.Address) {
			// Preserve fields the wizard does not ask about.
			hub.Flavour = links[i].Flavour
			if links[i].Name != "" {
				hub.Name = links[i].Name
			}
			links[i] = hub
			return links
		}
	}
	if len(links) == 0 {
		return []config.FTNLinkConfig{hub}
	}
	// The hub address itself changed: replace the first entry, which is where
	// the wizard puts the hub.
	hub.Flavour = links[0].Flavour
	links[0] = hub
	return links
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
		AutoJoin:     m.ftnWizard.autoJoinAreas,
		ACSRead:      "s10",
		ACSWrite:     "s20",
		BasePath:     basePath,
		ConferenceID: confID,
	})
}
