package configeditor

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// fetchNALMsg is the result of fetching the NAL from a hub.
type fetchNALMsg struct {
	areas []protocol.Area
	err   error
}

// subscribeAreasMsg is the result of a subscribe call.
type subscribeAreasMsg struct {
	statuses []protocol.AreaSubscriptionStatus
	err      error
}

// fetchHubNAL returns a tea.Cmd that GETs /v3net/v1/{network}/nal from the hub.
// This endpoint is public (no auth required).
func fetchHubNAL(hubURL, network string) tea.Cmd {
	return func() tea.Msg {
		client := &http.Client{Timeout: 10 * time.Second}
		url := strings.TrimRight(hubURL, "/") + "/v3net/v1/" + network + "/nal"
		resp, err := client.Get(url)
		if err != nil {
			return fetchNALMsg{err: err}
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fetchNALMsg{err: fmt.Errorf("hub returned status %d", resp.StatusCode)}
		}
		var nal protocol.NAL
		if err := json.NewDecoder(resp.Body).Decode(&nal); err != nil {
			return fetchNALMsg{err: fmt.Errorf("decode NAL: %w", err)}
		}
		return fetchNALMsg{areas: nal.Areas}
	}
}

// subscribeToAreas returns a tea.Cmd that POSTs /v3net/v1/subscribe with area tags.
// The subscribe endpoint is unauthenticated (bootstrap step). The keystore is
// only needed to populate node_id and pubkey_b64 in the request body.
func subscribeToAreas(hubURL, network string, areaTags []string,
	nodeID, pubKeyB64, bbsName, bbsHost string) tea.Cmd {
	return func() tea.Msg {
		req := protocol.SubscribeRequest{
			Network:   network,
			NodeID:    nodeID,
			PubKeyB64: pubKeyB64,
			BBSName:   bbsName,
			BBSHost:   bbsHost,
			AreaTags:  areaTags,
		}
		data, err := json.Marshal(req)
		if err != nil {
			return subscribeAreasMsg{err: fmt.Errorf("marshal subscribe: %w", err)}
		}

		client := &http.Client{Timeout: 10 * time.Second}
		url := strings.TrimRight(hubURL, "/") + "/v3net/v1/subscribe"
		httpReq, err := http.NewRequest("POST", url, strings.NewReader(string(data)))
		if err != nil {
			return subscribeAreasMsg{err: err}
		}
		httpReq.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(httpReq)
		if err != nil {
			return subscribeAreasMsg{err: err}
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return subscribeAreasMsg{err: fmt.Errorf("subscribe returned status %d", resp.StatusCode)}
		}

		var sr protocol.SubscribeWithAreasResponse
		if err := json.NewDecoder(resp.Body).Decode(&sr); err != nil {
			return subscribeAreasMsg{err: fmt.Errorf("decode subscribe response: %w", err)}
		}
		return subscribeAreasMsg{statuses: sr.Areas}
	}
}

// defaultLocalBoardName generates a default local board name from the network
// and area display name. e.g. "felonynet" + "General" → "FelonyNet General".
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
	m.message = "Subscription updated"
	return m, nil
}

// updateV3NetAreaBrowser handles key events in the area browser.
func (m Model) updateV3NetAreaBrowser(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.areaBrowserLoading {
		if msg.Type == tea.KeyEscape {
			m.areaBrowserLoading = false
			m.mode = m.areaBrowserReturn
		}
		return m, nil
	}

	if m.areaBrowserEditing {
		return m.updateAreaBrowserEdit(msg)
	}

	if m.areaBrowserManual {
		return m.updateAreaBrowserManual(msg)
	}

	total := len(m.areaBrowserAreas)

	switch msg.Type {
	case tea.KeyUp:
		if m.areaBrowserCursor > 0 {
			m.areaBrowserCursor--
		}
		m.clampAreaBrowserScroll()

	case tea.KeyDown:
		if m.areaBrowserCursor < total-1 {
			m.areaBrowserCursor++
		}
		m.clampAreaBrowserScroll()

	case tea.KeyHome:
		m.areaBrowserCursor = 0
		m.clampAreaBrowserScroll()

	case tea.KeyEnd:
		if total > 0 {
			m.areaBrowserCursor = total - 1
		}
		m.clampAreaBrowserScroll()

	case tea.KeySpace:
		if total == 0 || m.areaBrowserCursor >= total {
			return m, nil
		}
		item := &m.areaBrowserAreas[m.areaBrowserCursor]
		if item.Subscribed {
			item.Subscribed = false
			item.Status = ""
			item.LocalBoard = ""
			m.message = fmt.Sprintf("Unsubscribed from %s", item.Tag)
			return m, nil
		}
		item.Subscribed = true
		item.LocalBoard = defaultLocalBoardName(m.areaBrowserNetwork, item.Name)

		ks, err := m.loadOrCreateIdentityKeystore()
		if err != nil {
			m.message = fmt.Sprintf("Keystore error: %v", err)
			item.Subscribed = false
			item.LocalBoard = ""
			return m, nil
		}

		var tags []string
		for _, a := range m.areaBrowserAreas {
			if a.Subscribed {
				tags = append(tags, a.Tag)
			}
		}

		bbsName := ""
		bbsHost := ""
		if m.configs != nil {
			bbsName = m.configs.Server.BoardName
			bbsHost = m.configs.Server.SSHHost
			if bbsHost == "" {
				bbsHost = m.configs.Server.TelnetHost
			}
		}

		m.message = "Subscribing..."
		return m, subscribeToAreas(
			m.areaBrowserHub, m.areaBrowserNetwork, tags,
			ks.NodeID(), ks.PubKeyBase64(), bbsName, bbsHost,
		)

	case tea.KeyEscape:
		return m.exitAreaBrowser()

	default:
		key := strings.ToUpper(msg.String())
		switch key {
		case "E":
			if total == 0 || m.areaBrowserCursor >= total {
				return m, nil
			}
			item := &m.areaBrowserAreas[m.areaBrowserCursor]
			if !item.Subscribed {
				m.message = "Subscribe first to set a local board name"
				return m, nil
			}
			m.areaBrowserEditing = true
			m.textInput.SetValue(item.LocalBoard)
			m.textInput.CharLimit = 40
			m.textInput.Width = 40
			m.textInput.CursorEnd()
			m.textInput.Focus()
			return m, nil

		case "M":
			m.areaBrowserManual = true
			var tags []string
			for _, a := range m.areaBrowserAreas {
				if a.Subscribed {
					tags = append(tags, a.Tag)
				}
			}
			m.textInput.SetValue(strings.Join(tags, ","))
			m.textInput.CharLimit = 200
			m.textInput.Width = 60
			m.textInput.CursorEnd()
			m.textInput.Focus()
			m.message = ""
			return m, nil

		case "R":
			if m.areaBrowserError != "" {
				m.areaBrowserLoading = true
				m.areaBrowserError = ""
				return m, fetchHubNAL(m.areaBrowserHub, m.areaBrowserNetwork)
			}
		}
	}
	return m, nil
}

// updateAreaBrowserEdit handles key events while editing a local board name.
func (m Model) updateAreaBrowserEdit(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEnter:
		val := m.textInput.Value()
		if val == "" {
			m.message = "Board name cannot be empty"
			return m, nil
		}
		m.areaBrowserAreas[m.areaBrowserCursor].LocalBoard = val
		m.textInput.Blur()
		m.areaBrowserEditing = false
		m.message = ""
		return m, nil
	case tea.KeyEscape:
		m.textInput.Blur()
		m.areaBrowserEditing = false
		m.message = ""
		return m, nil
	default:
		var cmd tea.Cmd
		m.textInput, cmd = m.textInput.Update(msg)
		return m, cmd
	}
}

// updateAreaBrowserManual handles key events in manual text entry mode.
func (m Model) updateAreaBrowserManual(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEnter:
		val := m.textInput.Value()
		var tags []string
		for _, t := range strings.Split(val, ",") {
			if tag := strings.TrimSpace(t); tag != "" {
				tags = append(tags, tag)
			}
		}
		tagSet := make(map[string]bool)
		for _, t := range tags {
			tagSet[t] = true
		}
		for i := range m.areaBrowserAreas {
			m.areaBrowserAreas[i].Subscribed = tagSet[m.areaBrowserAreas[i].Tag]
			if m.areaBrowserAreas[i].Subscribed && m.areaBrowserAreas[i].LocalBoard == "" {
				m.areaBrowserAreas[i].LocalBoard = defaultLocalBoardName(
					m.areaBrowserNetwork, m.areaBrowserAreas[i].Name)
			}
		}
		existing := make(map[string]bool)
		for _, a := range m.areaBrowserAreas {
			existing[a.Tag] = true
		}
		for _, t := range tags {
			if !existing[t] {
				m.areaBrowserAreas = append(m.areaBrowserAreas, areaBrowserItem{
					Tag:        t,
					Name:       t,
					Subscribed: true,
					LocalBoard: defaultLocalBoardName(m.areaBrowserNetwork, t),
				})
			}
		}
		m.textInput.Blur()
		m.areaBrowserManual = false
		m.message = fmt.Sprintf("%d area(s) set", len(tags))
		return m, nil
	case tea.KeyEscape:
		m.textInput.Blur()
		m.areaBrowserManual = false
		m.message = ""
		return m, nil
	default:
		var cmd tea.Cmd
		m.textInput, cmd = m.textInput.Update(msg)
		return m, cmd
	}
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
	m.configs.MsgAreas = append(m.configs.MsgAreas, message.MessageArea{
		ID:       newID,
		Position: maxPos + 1,
		Tag:      tag,
		Name:     name,
		AreaType: "v3net",
		Network:  network,
		EchoTag:  tag,
		AutoJoin: true,
		ACSRead:  "s10",
		ACSWrite: "s20",
		BasePath: "msgbases/" + tag,
	})
}

// clampAreaBrowserScroll ensures the cursor is visible in the 10-row window.
func (m *Model) clampAreaBrowserScroll() {
	visible := 10
	if m.areaBrowserCursor < m.areaBrowserScroll {
		m.areaBrowserScroll = m.areaBrowserCursor
	}
	if m.areaBrowserCursor >= m.areaBrowserScroll+visible {
		m.areaBrowserScroll = m.areaBrowserCursor - visible + 1
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
	m.areaBrowserManual = false
	m.areaBrowserEditing = false
	m.areaBrowserReturn = returnMode
	m.message = ""
	m.mode = modeV3NetAreaBrowser
	return m, fetchHubNAL(hubURL, network)
}
