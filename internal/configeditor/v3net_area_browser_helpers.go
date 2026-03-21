package configeditor

import (
	"fmt"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ViSiON-3/vision-3-bbs/internal/conference"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
)

// areaBrowserListVisible is the number of rows shown in the area browser list.
const areaBrowserListVisible = 10

// defaultLocalBoardName generates a default local board name from the network
// and area display name. e.g. "felonynet" + "General" → "FelonyNet General".
// Network names are validated as lowercase ASCII alphanumeric, so single-byte
// indexing is safe here.
func defaultLocalBoardName(network, areaName string) string {
	if network == "" || areaName == "" {
		return areaName
	}
	titled := strings.ToUpper(network[:1]) + network[1:]
	return titled + " " + areaName
}

// handleFetchNALMsg processes the NAL fetch result.
func (m Model) handleFetchNALMsg(msg fetchNALMsg) (tea.Model, tea.Cmd) {
	m.areaBrowserLoading = false
	if msg.err != nil {
		m.areaBrowserError = fmt.Sprintf("Could not fetch areas: %v", msg.err)
		return m, nil
	}

	subscribed := make(map[string]bool)
	localNames := make(map[string]string)

	if m.wizard != nil && m.wizard.flow == "leaf" {
		for _, sa := range m.wizard.selectedAreas {
			if sa.Subscribed {
				subscribed[sa.Tag] = true
				localNames[sa.Tag] = sa.LocalBoard
			}
		}
	} else if m.areaBrowserReturn == modeRecordEdit {
		idx := m.recordEditIdx
		if idx >= 0 && idx < len(m.configs.V3Net.Leaves) {
			for _, b := range m.configs.V3Net.Leaves[idx].Boards {
				subscribed[b] = true
			}
			for _, ma := range m.configs.MsgAreas {
				if ma.AreaType == "v3net" && ma.Network == m.areaBrowserNetwork {
					localNames[ma.EchoTag] = ma.Name
				}
			}
		}
	}

	items := make([]areaBrowserItem, 0, len(msg.areas))
	for _, a := range msg.areas {
		item := areaBrowserItem{
			Tag:         a.Tag,
			Name:        a.Name,
			Description: a.Description,
			Subscribed:  subscribed[a.Tag],
		}
		if subscribed[a.Tag] {
			item.Status = "SUB"
		}
		if name, ok := localNames[a.Tag]; ok {
			item.LocalBoard = name
		} else if item.Subscribed {
			item.LocalBoard = defaultLocalBoardName(m.areaBrowserNetwork, a.Name)
		}
		items = append(items, item)
	}
	m.areaBrowserAreas = items
	m.areaBrowserCursor = 0
	m.areaBrowserScroll = 0
	m.areaBrowserError = ""
	return m, nil
}

// handleSubscribeAreasMsg processes the subscribe response.
func (m Model) handleSubscribeAreasMsg(msg subscribeAreasMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.message = fmt.Sprintf("Subscribe failed: %v", msg.err)
		return m, nil
	}
	statusMap := make(map[string]string)
	for _, s := range msg.statuses {
		statusMap[s.Tag] = strings.ToUpper(s.Status)
	}
	for i := range m.areaBrowserAreas {
		if st, ok := statusMap[m.areaBrowserAreas[i].Tag]; ok {
			m.areaBrowserAreas[i].Status = st
		}
	}
	if m.mode == modeV3NetAreaBrowser {
		m.message = "Subscription updated"
	}
	return m, nil
}

// exitAreaBrowser saves selections and returns to the previous mode.
func (m Model) exitAreaBrowser() (tea.Model, tea.Cmd) {
	if m.wizard != nil && m.wizard.flow == "leaf" {
		m.wizard.selectedAreas = make([]areaBrowserItem, len(m.areaBrowserAreas))
		copy(m.wizard.selectedAreas, m.areaBrowserAreas)
	} else if m.areaBrowserReturn == modeRecordEdit {
		idx := m.recordEditIdx
		if idx >= 0 && idx < len(m.configs.V3Net.Leaves) {
			var boards []string
			for _, a := range m.areaBrowserAreas {
				if a.Subscribed {
					boards = append(boards, a.Tag)
				}
			}
			m.configs.V3Net.Leaves[idx].Boards = boards
			m.createBrowserMessageAreas()
			m.dirty = true
			m.saveAll()
			if strings.HasPrefix(m.message, "SAVE ERROR") {
				return m, nil
			}
			m.recordFields = m.buildRecordFields()
		}
	}
	m.mode = m.areaBrowserReturn
	return m, nil
}

// createBrowserMessageAreas creates MsgArea entries for subscribed areas
// that don't already exist. Delegates to createBrowserMsgAreaIfNeeded.
func (m *Model) createBrowserMessageAreas() {
	for _, a := range m.areaBrowserAreas {
		if !a.Subscribed {
			continue
		}
		m.createBrowserMsgAreaIfNeeded(a.Tag, a.LocalBoard, m.areaBrowserNetwork)
	}
}

// sanitizeTag strips path separators, traversal sequences, reserved
// characters, and null bytes from a tag so it is safe to use as a
// filesystem path component.
func sanitizeTag(tag string) string {
	s := strings.TrimSpace(tag)
	// Remove null bytes.
	s = strings.ReplaceAll(s, "\x00", "")
	// Remove path separators and reserved characters.
	for _, c := range []string{"/", "\\", ":", "..", "<", ">", "|", "*", "?"} {
		s = strings.ReplaceAll(s, c, "")
	}
	s = strings.TrimSpace(s)
	return s
}

// createBrowserMsgAreaIfNeeded creates a single MsgArea if one doesn't already
// exist with the given EchoTag.
func (m *Model) createBrowserMsgAreaIfNeeded(tag, name, network string) {
	for _, ma := range m.configs.MsgAreas {
		if ma.EchoTag == tag {
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
	safeName := sanitizeTag(tag)
	if safeName == "" {
		safeName = fmt.Sprintf("unnamed_%d", newID)
	}
	confID := m.findOrCreateNetworkConference(network)
	m.configs.MsgAreas = append(m.configs.MsgAreas, message.MessageArea{
		ID:           newID,
		Position:     maxPos + 1,
		Tag:          tag,
		Name:         name,
		AreaType:     "v3net",
		Network:      network,
		EchoTag:      tag,
		AutoJoin:     true,
		ACSRead:      "s10",
		ACSWrite:     "s20",
		BasePath:     filepath.Join("msgbases", safeName),
		ConferenceID: confID,
	})
}

// findOrCreateNetworkConference returns the conference ID for a V3Net network,
// creating one if none exists. It looks for an existing conference whose tag
// matches the uppercase network name.
func (m *Model) findOrCreateNetworkConference(network string) int {
	upperNet := strings.ToUpper(network)
	for _, c := range m.configs.Conferences {
		if strings.ToUpper(c.Tag) == upperNet {
			return c.ID
		}
	}
	// Create a new conference for this network.
	newID := 1
	maxPos := 0
	for _, c := range m.configs.Conferences {
		if c.ID >= newID {
			newID = c.ID + 1
		}
		if c.Position > maxPos {
			maxPos = c.Position
		}
	}
	m.configs.Conferences = append(m.configs.Conferences, conference.Conference{
		ID:          newID,
		Position:    maxPos + 1,
		Tag:         upperNet,
		Name:        network,
		Description: network + " message network",
		ACS:         "s10",
	})
	return newID
}

// clampAreaBrowserScroll ensures the cursor is visible in the list window.
func (m *Model) clampAreaBrowserScroll() {
	if m.areaBrowserCursor < m.areaBrowserScroll {
		m.areaBrowserScroll = m.areaBrowserCursor
	}
	if m.areaBrowserCursor >= m.areaBrowserScroll+areaBrowserListVisible {
		m.areaBrowserScroll = m.areaBrowserCursor - areaBrowserListVisible + 1
	}
}

// enterAreaBrowser initializes the area browser and starts the NAL fetch.
func (m Model) enterAreaBrowser(hubURL, network string, returnMode editorMode) (tea.Model, tea.Cmd) {
	m.areaBrowserHub = hubURL
	m.areaBrowserNetwork = network
	m.areaBrowserAreas = nil
	m.areaBrowserCursor = 0
	m.areaBrowserScroll = 0
	m.areaBrowserLoading = true
	m.areaBrowserError = ""
	m.areaBrowserReturn = returnMode
	m.message = ""
	m.mode = modeV3NetAreaBrowser
	return m, fetchHubNAL(hubURL, network)
}
