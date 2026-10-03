package configeditor

import (
	"fmt"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/file"
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
		m.configs.FTN.BinkdOutboundPath = "data/ftn/out"
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
		IPFamily:        w.hubIPFamily,
	}

	if existing, ok := m.configs.FTN.Networks[netKey]; ok && editing {
		// Keep the settings this wizard does not ask about. Tosser enable
		// and origin are editable under Echomail Networks, and
		// rewriting them with the wizard's create-time defaults would throw
		// away whatever the sysop set there.
		existing.OwnAddress = w.ownAddress
		existing.Origin = w.originLine
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
		netCfg := config.FTNNetworkConfig{
			InternalTosserEnabled: true,
			OwnAddress:            w.ownAddress,
			// Origin as entered in the wizard; blank falls back to the board
			// name. The tearline is not configurable — the software stamps it.
			Origin: w.originLine,
			Links:  []config.FTNLinkConfig{link},
		}
		// A second or later network gets its own BSO outbound rather than
		// sharing the global one with the networks already there (see
		// config.FTNConfig.AssignSharedOutbounds for why sharing breaks).
		// A new network has nothing queued, so moving it costs nothing.
		if len(m.configs.FTN.Networks) > 0 {
			var inUse []string
			for _, other := range m.configs.FTN.Networks {
				inUse = append(inUse, other.BinkdOutboundPath)
			}
			netCfg.BinkdOutboundPath = config.FreeNetworkOutboundPath(m.configs.FTN.BinkdOutboundPath, netKey, inUse)
		}
		m.configs.FTN.Networks[netKey] = netCfg
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

	// 3b. Bad and dupe areas, shared by every network.
	if w.rejectAreas {
		m.ensureFTNRejectAreas()
	}

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

	// 4b. File areas for each selected file echo.
	fileAreasAdded, fileEchoNote := m.createFTNFileEchoAreas(netKey, confID)

	// 5. Update binkd.conf.
	bbsRoot := filepath.Join(m.configPath, "..")
	absRoot, err := filepath.Abs(bbsRoot)
	if err != nil {
		absRoot = bbsRoot
	}
	binkdPath := filepath.Join(absRoot, "data", "ftn", "binkd.conf")

	binkdCfg := ftn.BinkdConfig{
		BBSRoot:         absRoot,
		BoardName:       m.configs.Server.BoardName,
		SysopName:       m.configs.Server.SysOpName,
		Location:        m.configs.Server.BBSLocation,
		Domains:         map[string]int{netKey: w.zone},
		Addresses:       []string{fmt.Sprintf("%s@%s", w.ownAddress, netKey)},
		OutboundPath:    m.configs.FTN.BinkdOutboundPath,
		NetworkOutbound: ftn.NetworkOutbounds(m.configs.FTN),
		Node: ftn.BinkdNode{
			Address:     fmt.Sprintf("%s@%s", w.hubAddress, netKey),
			Hostname:    config.JoinBinkpHostPort(w.hubHostname, w.hubPort),
			SessionPwd:  w.sessionPassword,
			NetworkName: w.networkName,
			IPFamily:    w.hubIPFamily,
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
	if !m.saveAll() {
		m.message += binkdWarning
		return m, nil
	}
	// The save syncs binkd.conf as well, and reports a failure only in the
	// status message that the result below replaces. When the update above
	// failed too it is the same fault, already reported.
	if binkdWarning == "" && m.binkdSyncErr != nil {
		binkdWarning = fmt.Sprintf(" Warning: binkd.conf sync failed: %v — fix it before restarting.", m.binkdSyncErr)
	}

	nodelistNote := m.saveFTNWizardNodelist(netKey)

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
	m.message += fileEchoNote + nodelistNote + binkdWarning
	if fileAreasAdded > 0 {
		m.message += fmt.Sprintf(" Subscribe to the file echoes at your %s hub.", w.networkName)
	}
	m.mode = modeCategoryMenu
	return m, nil
}

// saveFTNWizardNodelist compiles the nodelist the wizard downloaded for Node
// Lookup into <data>/ftn/nodelist/<netKey>.json, so the BBS can look systems
// up from the start instead of waiting for the first one to arrive by file
// echo. It returns a note for the status message, or "" when there was no
// nodelist to save. A failure is reported in the note and never undoes the
// save: the nodelist can still be imported later.
func (m Model) saveFTNWizardNodelist(netKey string) string {
	w := m.ftnWizard
	if w.nodelist == nil {
		return ""
	}
	// The zone of the address being saved, not w.zone: the address can be
	// edited after the lookup, and v3mail toss checks a delivered nodelist
	// against the own address too.
	own, err := ftn.ParseAddress(w.ownAddress)
	if err != nil {
		return ""
	}
	compiled := ftn.CompileNodelist(w.nodelist, netKey, w.nodelistURL)
	if !compiled.HasZone(own.Zone) {
		// Another network's list; saving it would make lookups answer for
		// the wrong systems.
		return ""
	}
	saved, _, err := ftn.SaveCompiledNodelist(ftn.NodelistDir(m.dataPath()), netKey, compiled, false)
	switch {
	case err != nil:
		return fmt.Sprintf(" Nodelist not saved: %v.", err)
	case saved:
		return fmt.Sprintf(" Nodelist saved (%d systems).", len(compiled.Nodes))
	}
	return "" // a newer compiled nodelist is already there
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

	newID, pos := m.nextMsgAreaSlot()
	m.configs.MsgAreas = append(m.configs.MsgAreas, message.MessageArea{
		ID:           newID,
		Position:     pos,
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

// nextMsgAreaSlot returns the ID and position for a message area appended to
// the end of the list.
func (m *Model) nextMsgAreaSlot() (id, position int) {
	id = 1
	for _, ma := range m.configs.MsgAreas {
		if ma.ID >= id {
			id = ma.ID + 1
		}
		if ma.Position > position {
			position = ma.Position
		}
	}
	return id, position + 1
}

// ftnRejectArea describes one of the two areas the tosser routes rejected
// mail to.
type ftnRejectArea struct {
	tag      *string // ftn.json field naming the area
	newTag   string
	name     string
	basePath string
}

// ftnRejectAreas returns the bad and dupe area settings with the tag, name
// and message base each gets when the wizard creates it.
func (m *Model) ftnRejectAreas() []ftnRejectArea {
	fc := &m.configs.FTN
	return []ftnRejectArea{
		{&fc.BadAreaTag, "ftn_bad", "FTN Bad Mail", filepath.Join("msgbases", "ftn_bad")},
		{&fc.DupeAreaTag, "ftn_dupe", "FTN Duplicates", filepath.Join("msgbases", "ftn_dupe")},
	}
}

// ftnRejectAreasMissing reports whether the bad or dupe area is unset, or set
// to a tag no message area has. The tosser treats either the same as unset.
func (m *Model) ftnRejectAreasMissing() bool {
	if m.configs == nil {
		return true
	}
	for _, r := range m.ftnRejectAreas() {
		if _, ok := m.msgAreaTag(*r.tag); !ok {
			return true
		}
	}
	return false
}

// ensureFTNRejectAreas points the bad and dupe settings at real message areas,
// creating them where needed. A setting that already names an area is left
// alone. The areas are local rather than echomail, so v3mail scan never
// exports what lands in them, ungrouped because they serve every network, and
// sysop-only because they hold mail nobody else was meant to see yet.
func (m *Model) ensureFTNRejectAreas() {
	for _, r := range m.ftnRejectAreas() {
		if tag, ok := m.msgAreaTag(*r.tag); ok {
			*r.tag = tag
			continue
		}
		// Reuse an area left by an earlier run whose setting was cleared.
		if tag, ok := m.msgAreaTag(r.newTag); ok {
			*r.tag = tag
			continue
		}
		id, pos := m.nextMsgAreaSlot()
		m.configs.MsgAreas = append(m.configs.MsgAreas, message.MessageArea{
			ID:       id,
			Position: pos,
			Tag:      r.newTag,
			Name:     r.name,
			AreaType: "local",
			ACSRead:  "SYSOP",
			ACSWrite: "SYSOP",
			BasePath: r.basePath,
		})
		*r.tag = r.newTag
	}
}

// createFTNFileEchoAreas adds a file area, linked to its echo, for each
// selected file echo this network does not already feed to one — what helper
// fileecho does from the command line. It returns how many it added and a
// note for the status message. Unticking an echo already carried never
// removes its area, which may hold files.
func (m *Model) createFTNFileEchoAreas(netKey string, confID int) (int, string) {
	w := m.ftnWizard
	var echoes []ftn.EchoArea
	for i, sel := range w.selectedFileEchoes {
		if sel && i < len(w.availableFileEchoes) {
			echoes = append(echoes, w.availableFileEchoes[i])
		}
	}
	dropped := ""
	if n := w.uncarriedFileEchoCount(); n > 0 {
		dropped = fmt.Sprintf(" %d existing file area(s) left in place: remove them under File Areas.", n)
	}
	if len(echoes) == 0 {
		return 0, dropped
	}
	// The network key becomes a directory under the file base.
	if err := file.CheckFilename(netKey); err != nil || strings.Trim(netKey, ".") == "" {
		return 0, fmt.Sprintf(" File areas not created: network name %q cannot be a directory name.", netKey) + dropped
	}
	added, _ := ftn.PlanFileEchoAreas(m.configs.FileAreas, echoes, ftn.FileEchoAreaOptions{
		Network:      netKey,
		ConferenceID: confID,
		ACSList:      "s10",
		ACSDownload:  "s20",
		// Fed by the network, not by callers.
		ACSUpload: "s250",
	})
	if len(added) == 0 {
		return 0, dropped
	}
	m.configs.FileAreas = append(m.configs.FileAreas, added...)
	return len(added), fmt.Sprintf(" %d file area(s) created.", len(added)) + dropped
}
