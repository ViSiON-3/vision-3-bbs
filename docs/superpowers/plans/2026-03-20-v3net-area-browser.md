# V3Net Area Browser Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an interactive area browser to the config TUI that polls V3Net hubs for available areas and lets sysops subscribe with live feedback.

**Architecture:** New `modeV3NetAreaBrowser` editor mode with view/update files following the existing `modeV3NetHubAreas` pattern. Two entry points: the leaf wizard (replacing the "Board Tag" field) and the leaf subscription edit view (new "Browse Areas" field). The browser fetches the NAL via unauthenticated GET, makes live subscribe calls, and auto-creates MsgArea entries.

**Tech Stack:** Go, Bubble Tea (charmbracelet/bubbletea), lipgloss, net/http, encoding/json

**Spec:** `docs/superpowers/specs/2026-03-20-v3net-area-browser-design.md`

---

## File Structure

| File | Responsibility |
|---|---|
| `internal/configeditor/model.go` | New mode constant, `areaBrowserItem` type, browser state fields on `Model` |
| `internal/configeditor/update_v3net_area_browser.go` | **NEW** — Key handler, `fetchHubNAL`/`subscribeToAreas` tea.Cmds, message types, area browser update logic |
| `internal/configeditor/view_v3net_area_browser.go` | **NEW** — `viewV3NetAreaBrowser()` render function |
| `internal/configeditor/fields_wizard.go` | Replace "Board Tag" with "Areas" ftDisplay field; add `selectedAreas` to `wizardState`; update validation and completion |
| `internal/configeditor/fields_v3net.go` | Add "Browse Areas" ftDisplay field to `fieldsV3NetLeaf()` |
| `internal/configeditor/update_wizard_form.go` | Handle Enter on "Areas" field; update `confirmLeafWizard()`, `wizardHasData()`, `submitWizardForm()` |
| `internal/configeditor/update_records.go` | Handle Enter on "Browse Areas" ftDisplay in leaf edit view |

---

### Task 1: Add Mode, Types, and State to Model

**Files:**
- Modify: `internal/configeditor/model.go:38-48` (mode constants), `model.go:68-102` (wizardState), `model.go:104-204` (Model fields)

- [ ] **Step 1: Add `modeV3NetAreaBrowser` mode constant**

In `model.go`, add after `modeWizardExitConfirm` (line 48):

```go
	modeV3NetAreaBrowser                     // Area browser (NAL fetch + subscribe)
```

- [ ] **Step 2: Add `areaBrowserItem` type**

In `model.go`, after the `wizardArea` struct (line 74), add:

```go
// areaBrowserItem represents one area in the NAL area browser.
type areaBrowserItem struct {
	Tag         string // NAL area tag (e.g. "fel.general")
	Name        string // Display name (e.g. "General")
	Description string
	Status      string // "", "ACTIVE", "PENDING", "DENIED"
	Subscribed  bool   // toggled by Space
	LocalBoard  string // auto-generated or user-edited local board name
}
```

- [ ] **Step 3: Add `selectedAreas` to `wizardState`**

In `model.go`, in the `wizardState` struct after `fetchError` (line 87), add:

```go
	selectedAreas []areaBrowserItem // areas selected during wizard flow
```

- [ ] **Step 4: Add browser state fields to `Model`**

In `model.go`, after the hub area insert form state block (lines 190-191), add:

```go
	// V3Net area browser state
	areaBrowserHub      string             // hub URL being browsed
	areaBrowserNetwork  string             // network name
	areaBrowserAreas    []areaBrowserItem  // fetched areas with status
	areaBrowserCursor   int                // highlighted row
	areaBrowserScroll   int                // scroll offset
	areaBrowserLoading  bool               // true while NAL fetch in flight
	areaBrowserError    string             // error from fetch/subscribe
	areaBrowserManual   bool               // true when in manual fallback mode
	areaBrowserEditing  bool               // true when editing local board name
	areaBrowserReturn   editorMode         // mode to return to on ESC
```

- [ ] **Step 5: Verify it compiles**

Run: `go build ./internal/configeditor/...`
Expected: success (no references to new types yet)

- [ ] **Step 6: Commit**

```bash
git add internal/configeditor/model.go
git commit -m "feat(configeditor): add area browser mode, types, and state fields"
```

---

### Task 2: Create the NAL Fetch and Subscribe Commands

**Files:**
- Create: `internal/configeditor/update_v3net_area_browser.go`

- [ ] **Step 1: Create file with message types and fetch command**

Create `internal/configeditor/update_v3net_area_browser.go`:

```go
package configeditor

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

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
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./internal/configeditor/...`
Expected: success (message types defined but not yet handled)

- [ ] **Step 3: Commit**

```bash
git add internal/configeditor/update_v3net_area_browser.go
git commit -m "feat(configeditor): add NAL fetch and subscribe tea.Cmds"
```

---

### Task 3: Implement the Area Browser Update Handler

**Files:**
- Modify: `internal/configeditor/update_v3net_area_browser.go`
- Modify: `internal/configeditor/model.go` (Update switch + message handling)

- [ ] **Step 1: Add the update handler to `update_v3net_area_browser.go`**

Append to the file:

```go
// defaultLocalBoardName generates a default local board name from the network
// and area display name. e.g. "felonynet" + "General" → "FelonyNet General".
func defaultLocalBoardName(network, areaName string) string {
	if network == "" || areaName == "" {
		return areaName
	}
	// Title-case the network name.
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

	// Build browser items from NAL areas, merging with any existing subscriptions.
	subscribed := make(map[string]bool)
	localNames := make(map[string]string)

	// Check existing boards from wizard selectedAreas or leaf config.
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
			// Find local board names from MsgAreas.
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
	// Update status for each area from the response.
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
	// If loading, only allow ESC.
	if m.areaBrowserLoading {
		if msg.Type == tea.KeyEscape {
			m.areaBrowserLoading = false
			m.mode = m.areaBrowserReturn
		}
		return m, nil
	}

	// If editing local board name, route to textInput.
	if m.areaBrowserEditing {
		return m.updateAreaBrowserEdit(msg)
	}

	// If in manual mode, route to textInput.
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
			// Unsubscribe locally.
			item.Subscribed = false
			item.Status = ""
			item.LocalBoard = ""
			m.message = fmt.Sprintf("Unsubscribed from %s", item.Tag)
			return m, nil
		}
		// Subscribe: set local board name and fire subscribe call.
		item.Subscribed = true
		item.LocalBoard = defaultLocalBoardName(m.areaBrowserNetwork, item.Name)

		// Load keystore for node identity.
		ks, err := m.loadOrCreateIdentityKeystore()
		if err != nil {
			m.message = fmt.Sprintf("Keystore error: %v", err)
			item.Subscribed = false
			item.LocalBoard = ""
			return m, nil
		}

		// Collect all currently subscribed tags.
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
			// Edit local board name for subscribed area.
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
			// Switch to manual text entry mode.
			m.areaBrowserManual = true
			// Pre-fill with current subscribed tags.
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
			// Retry NAL fetch.
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
		// Parse comma-separated tags and mark them as subscribed.
		val := m.textInput.Value()
		var tags []string
		for _, t := range strings.Split(val, ",") {
			if tag := strings.TrimSpace(t); tag != "" {
				tags = append(tags, tag)
			}
		}
		// Update browser items.
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
		// Also add tags not in the NAL as manually-entered items.
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
	// Write selections back.
	if m.wizard != nil && m.wizard.flow == "leaf" {
		// Wizard mode: store in selectedAreas.
		m.wizard.selectedAreas = make([]areaBrowserItem, len(m.areaBrowserAreas))
		copy(m.wizard.selectedAreas, m.areaBrowserAreas)
	} else if m.areaBrowserReturn == modeRecordEdit {
		// Edit mode: update leaf config boards and create MsgAreas.
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
```

- [ ] **Step 2: Add the `message` import**

The file needs the `message` package import for `message.MessageArea`. Add to the import block:

```go
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
```

- [ ] **Step 3: Wire message handling into `model.go` Update switch**

In `model.go`, in the `Update` method, add cases after the `fetchNetworksMsg` case (line 277):

```go
	case fetchNALMsg:
		return m.handleFetchNALMsg(msg)

	case subscribeAreasMsg:
		return m.handleSubscribeAreasMsg(msg)
```

- [ ] **Step 4: Wire key routing into `model.go` Update switch**

In `model.go`, in the `tea.KeyMsg` switch, add after `modeWizardExitConfirm` (line 324):

```go
		case modeV3NetAreaBrowser:
			return m.updateV3NetAreaBrowser(msg)
```

- [ ] **Step 5: Verify it compiles**

Run: `go build ./internal/configeditor/...`
Expected: success

- [ ] **Step 6: Commit**

```bash
git add internal/configeditor/update_v3net_area_browser.go internal/configeditor/model.go
git commit -m "feat(configeditor): implement area browser update handler and routing"
```

---

### Task 4: Create the Area Browser View

**Files:**
- Create: `internal/configeditor/view_v3net_area_browser.go`
- Modify: `internal/configeditor/view.go` (add routing)

- [ ] **Step 1: Create the view file**

Create `internal/configeditor/view_v3net_area_browser.go`:

```go
package configeditor

import (
	"fmt"
	"strings"
)

// viewV3NetAreaBrowser renders the area browser screen.
func (m Model) viewV3NetAreaBrowser() string {
	var b strings.Builder
	b.WriteString(m.globalHeaderLine())
	b.WriteByte('\n')

	bgLine := bgFillStyle.Render(strings.Repeat("░", m.width))
	boxW := 70
	listVisible := 10
	total := len(m.areaBrowserAreas)

	// Fixed rows: header(1) + border(1) + title(1) + colheader(1) + sep(1)
	//           + list(10) + border(1) + msg(1) + help(1)
	fixedRows := listVisible + 8
	extraV := maxInt(0, m.height-fixedRows)
	topPad := extraV / 2
	bottomPad := extraV - topPad

	for i := 0; i < topPad; i++ {
		b.WriteString(bgLine)
		b.WriteByte('\n')
	}

	padL := maxInt(0, (m.width-boxW-2)/2)
	padR := maxInt(0, m.width-padL-boxW-2)

	border := func(s string) string {
		return bgFillStyle.Render(strings.Repeat("░", padL)) + s +
			bgFillStyle.Render(strings.Repeat("░", maxInt(0, padR)))
	}

	// Top border.
	b.WriteString(border(menuBorderStyle.Render("┌" + strings.Repeat("─", boxW) + "┐")))
	b.WriteByte('\n')

	// Title.
	title := fmt.Sprintf("Area Browser — %s", m.areaBrowserNetwork)
	b.WriteString(border(menuBorderStyle.Render("│") +
		menuHeaderStyle.Render(centerText(title, boxW)) +
		menuBorderStyle.Render("│")))
	b.WriteByte('\n')

	// Handle special states.
	if m.areaBrowserLoading {
		b.WriteString(border(menuBorderStyle.Render("│") +
			menuItemStyle.Render(centerText("Fetching areas...", boxW)) +
			menuBorderStyle.Render("│")))
		b.WriteByte('\n')
		for i := 0; i < listVisible+1; i++ {
			b.WriteString(border(menuBorderStyle.Render("│") +
				menuItemStyle.Render(strings.Repeat(" ", boxW)) +
				menuBorderStyle.Render("│")))
			b.WriteByte('\n')
		}
		b.WriteString(border(menuBorderStyle.Render("└" + strings.Repeat("─", boxW) + "┘")))
		b.WriteByte('\n')
		for i := 0; i < bottomPad+2; i++ {
			b.WriteString(bgLine)
			b.WriteByte('\n')
		}
		b.WriteString(helpBarStyle.Render(centerText("ESC - Cancel", m.width)))
		return b.String()
	}

	if m.areaBrowserManual {
		return m.viewAreaBrowserManual()
	}

	if m.areaBrowserError != "" && total == 0 {
		b.WriteString(border(menuBorderStyle.Render("│") +
			flashMessageStyle.Render(padRight(" "+m.areaBrowserError, boxW)) +
			menuBorderStyle.Render("│")))
		b.WriteByte('\n')
		for i := 0; i < listVisible+1; i++ {
			b.WriteString(border(menuBorderStyle.Render("│") +
				menuItemStyle.Render(strings.Repeat(" ", boxW)) +
				menuBorderStyle.Render("│")))
			b.WriteByte('\n')
		}
		b.WriteString(border(menuBorderStyle.Render("└" + strings.Repeat("─", boxW) + "┘")))
		b.WriteByte('\n')
		for i := 0; i < bottomPad; i++ {
			b.WriteString(bgLine)
			b.WriteByte('\n')
		}
		b.WriteString(bgLine)
		b.WriteByte('\n')
		helpStr := "R - Retry  |  M - Manual Entry  |  ESC - Back"
		b.WriteString(helpBarStyle.Render(centerText(helpStr, m.width)))
		return b.String()
	}

	// Column header.
	colHeader := fmt.Sprintf("   %-4s %-16s %-16s %-8s %s", " ", "Tag", "Name", "Status", "Local Board")
	b.WriteString(border(menuBorderStyle.Render("│") +
		menuHeaderStyle.Render(padRight(colHeader, boxW)) +
		menuBorderStyle.Render("│")))
	b.WriteByte('\n')

	// Separator.
	b.WriteString(border(menuBorderStyle.Render("│") +
		separatorStyle.Render(strings.Repeat("─", boxW)) +
		menuBorderStyle.Render("│")))
	b.WriteByte('\n')

	// List rows.
	for row := 0; row < listVisible; row++ {
		visIdx := m.areaBrowserScroll + row
		var content string

		if visIdx >= 0 && visIdx < total {
			a := m.areaBrowserAreas[visIdx]
			check := "[ ]"
			if a.Subscribed {
				check = "[x]"
			}
			tag := padRight(a.Tag, 16)
			if len(tag) > 16 {
				tag = tag[:16]
			}
			name := padRight(a.Name, 16)
			if len(name) > 16 {
				name = name[:16]
			}
			status := padRight(a.Status, 8)
			localBoard := a.LocalBoard
			maxBoard := boxW - 4 - 16 - 16 - 8 - 5
			if len(localBoard) > maxBoard {
				localBoard = localBoard[:maxBoard]
			}
			content = fmt.Sprintf("   %s %-16s %-16s %-8s %s",
				check, tag, name, status, localBoard)
		}

		if content == "" {
			content = strings.Repeat(" ", boxW)
		}
		if len(content) < boxW {
			content += strings.Repeat(" ", boxW-len(content))
		} else if len(content) > boxW {
			content = content[:boxW]
		}

		var styled string
		if visIdx == m.areaBrowserCursor {
			styled = menuHighlightStyle.Render(content)
		} else {
			styled = menuItemStyle.Render(content)
		}

		b.WriteString(border(menuBorderStyle.Render("│") + styled + menuBorderStyle.Render("│")))
		b.WriteByte('\n')
	}

	// Bottom border.
	b.WriteString(border(menuBorderStyle.Render("└" + strings.Repeat("─", boxW) + "┘")))
	b.WriteByte('\n')

	// Message line.
	if m.message != "" {
		msgLine := bgFillStyle.Render(strings.Repeat("░", padL)) +
			flashMessageStyle.Render(" "+padRight(m.message, boxW)) +
			bgFillStyle.Render(strings.Repeat("░", maxInt(0, padR+1)))
		b.WriteString(msgLine)
	} else {
		b.WriteString(bgLine)
	}
	b.WriteByte('\n')

	for i := 0; i < bottomPad; i++ {
		b.WriteString(bgLine)
		b.WriteByte('\n')
	}

	// Help bar.
	var helpStr string
	if m.areaBrowserEditing {
		helpStr = "Enter - Save  |  ESC - Cancel"
	} else {
		helpStr = "Space - Subscribe/Unsubscribe  |  E - Edit Board Name  |  M - Manual  |  ESC - Done"
	}
	b.WriteString(helpBarStyle.Render(centerText(helpStr, m.width)))

	return b.String()
}

// viewAreaBrowserManual renders the manual text entry mode.
func (m Model) viewAreaBrowserManual() string {
	var b strings.Builder
	b.WriteString(m.globalHeaderLine())
	b.WriteByte('\n')

	bgLine := bgFillStyle.Render(strings.Repeat("░", m.width))
	boxW := 70

	extraV := maxInt(0, m.height-10)
	topPad := extraV / 2
	bottomPad := extraV - topPad

	for i := 0; i < topPad; i++ {
		b.WriteString(bgLine)
		b.WriteByte('\n')
	}

	padL := maxInt(0, (m.width-boxW-2)/2)
	padR := maxInt(0, m.width-padL-boxW-2)

	border := func(s string) string {
		return bgFillStyle.Render(strings.Repeat("░", padL)) + s +
			bgFillStyle.Render(strings.Repeat("░", maxInt(0, padR)))
	}

	row := func(content string) string {
		return border(editBorderStyle.Render("│") +
			fieldDisplayStyle.Width(boxW).Render(content) +
			editBorderStyle.Render("│"))
	}

	b.WriteString(border(editBorderStyle.Render("┌" + strings.Repeat("─", boxW) + "┐")))
	b.WriteByte('\n')
	b.WriteString(border(editBorderStyle.Render("│") +
		menuHeaderStyle.Render(centerText("Manual Area Entry", boxW)) +
		editBorderStyle.Render("│")))
	b.WriteByte('\n')
	b.WriteString(row(strings.Repeat(" ", boxW)))
	b.WriteByte('\n')
	b.WriteString(row("  Enter comma-separated area tags (e.g. fel.general,fel.tech):"))
	b.WriteByte('\n')
	b.WriteString(row("  " + m.textInput.View()))
	b.WriteByte('\n')
	b.WriteString(row(strings.Repeat(" ", boxW)))
	b.WriteByte('\n')
	b.WriteString(border(editBorderStyle.Render("└" + strings.Repeat("─", boxW) + "┘")))
	b.WriteByte('\n')

	for i := 0; i < bottomPad; i++ {
		b.WriteString(bgLine)
		b.WriteByte('\n')
	}

	helpStr := "Enter - Apply  |  ESC - Cancel"
	b.WriteString(helpBarStyle.Render(centerText(helpStr, m.width)))

	return b.String()
}
```

- [ ] **Step 2: Add view routing to `view.go`**

In `view.go`, add a case after `modeV3NetAreaRenameJAM` (line 65):

```go
	case modeV3NetAreaBrowser:
		return m.viewV3NetAreaBrowser()
```

- [ ] **Step 3: Verify it compiles**

Run: `go build ./internal/configeditor/...`
Expected: success

- [ ] **Step 4: Commit**

```bash
git add internal/configeditor/view_v3net_area_browser.go internal/configeditor/view.go
git commit -m "feat(configeditor): add area browser view rendering"
```

---

### Task 5: Wire Entry Point A — Leaf Wizard

**Files:**
- Modify: `internal/configeditor/fields_wizard.go:13-74` (leaf wizard fields)
- Modify: `internal/configeditor/update_wizard_form.go:17-84` (wizard form handler), `update_wizard_form.go:232-279` (confirmLeafWizard), `update_wizard_form.go:281-295` (wizardHasData), `update_wizard_form.go:328-349` (submitWizardForm)

- [ ] **Step 1: Replace "Board Tag" with "Areas" display field in `fieldsLeafWizard()`**

In `fields_wizard.go`, replace the "Board Tag" field definition (lines 40-50) with:

```go
		{
			Label: "Areas", Help: "Press Enter to browse and subscribe to network areas", Type: ftDisplay, Col: 3, Row: 3, Width: 45,
			Get: func() string {
				n := 0
				for _, a := range w.selectedAreas {
					if a.Subscribed {
						n++
					}
				}
				if n == 0 {
					return "(none — press Enter to browse)"
				}
				return fmt.Sprintf("%d area(s) selected", n)
			},
		},
```

- [ ] **Step 2: Handle Enter on "Areas" field in wizard form**

In `update_wizard_form.go`, in `updateWizardForm()`, after the hub wizard "Initial Areas" ftDisplay handling (lines 36-41), add a new block for the leaf wizard "Areas" field:

```go
		// Leaf wizard "Areas" field opens the area browser.
		if f.Type == ftDisplay && m.wizard.flow == "leaf" && f.Label == "Areas" {
			if m.wizard.hubURL == "" {
				m.message = "Enter a Hub URL first"
				return m, nil
			}
			if m.wizard.networkName == "" {
				m.message = "Enter a Network name first"
				return m, nil
			}
			return m.enterAreaBrowser(m.wizard.hubURL, m.wizard.networkName, modeWizardForm)
		}
```

- [ ] **Step 3: Update `confirmLeafWizard()` to use `selectedAreas`**

In `update_wizard_form.go`, in `confirmLeafWizard()`, replace the leaf config creation (lines 249-255):

```go
	// Build boards from selected areas.
	var boards []string
	for _, a := range m.wizard.selectedAreas {
		if a.Subscribed {
			boards = append(boards, a.Tag)
		}
	}

	leaf := config.V3NetLeafConfig{
		HubURL:       m.wizard.hubURL,
		Network:      m.wizard.networkName,
		Boards:       boards,
		PollInterval: m.wizard.pollInterval,
		Origin:       m.wizard.origin,
	}
```

Also, after `m.saveAll()`, add MsgArea creation from selected areas. After line 262 (`m.saveAll()`):

```go
	// Create MsgAreas for selected areas.
	for _, a := range m.wizard.selectedAreas {
		if !a.Subscribed {
			continue
		}
		m.createBrowserMsgAreaIfNeeded(a.Tag, a.LocalBoard, m.wizard.networkName)
	}
	m.saveAll()
```

This uses `createBrowserMsgAreaIfNeeded` already defined in Task 3.

- [ ] **Step 4: Update `wizardHasData()` for new field**

In `update_wizard_form.go`, in `wizardHasData()`, replace the `"leaf"` case (lines 291-293):

```go
	case "leaf":
		return m.wizard.hubURL != "" || m.wizard.networkName != "" ||
			len(m.wizard.selectedAreas) > 0
```

- [ ] **Step 5: Update `submitWizardForm()` with area count guard**

In `update_wizard_form.go`, in `submitWizardForm()`, after the leaf validation (line 333), add:

```go
		subscribed := 0
		for _, a := range m.wizard.selectedAreas {
			if a.Subscribed {
				subscribed++
			}
		}
		if subscribed == 0 {
			m.message = "Select at least one area to subscribe to"
			return m, nil
		}
```

- [ ] **Step 6: Remove the `boardTag` field from `wizardState`** (optional cleanup)

In `model.go`, remove `boardTag string` from the `wizardState` struct (line 84). If anything still references it, the compiler will catch it.

- [ ] **Step 7: Verify it compiles**

Run: `go build ./internal/configeditor/...`
Expected: success

- [ ] **Step 8: Commit**

```bash
git add internal/configeditor/fields_wizard.go internal/configeditor/update_wizard_form.go internal/configeditor/update_v3net_area_browser.go internal/configeditor/model.go
git commit -m "feat(configeditor): wire area browser into leaf wizard"
```

---

### Task 6: Wire Entry Point B — Leaf Subscription Edit View

**Files:**
- Modify: `internal/configeditor/fields_v3net.go:62-115` (fieldsV3NetLeaf)
- Modify: `internal/configeditor/update_records.go:204-216` (updateRecordEdit ftDisplay handler)

- [ ] **Step 1: Add "Browse Areas" field to `fieldsV3NetLeaf()`**

In `fields_v3net.go`, in `fieldsV3NetLeaf()`, add a new field after the "Origin" field (after line 113, before the closing `}`):

```go
		{
			Label: "Browse Areas", Help: "Press Enter to browse and subscribe to hub areas", Type: ftDisplay, Col: 3, Row: 7, Width: 49,
			Get: func() string {
				n := len(l.Boards)
				if n == 0 {
					return "(none — press Enter to browse)"
				}
				return fmt.Sprintf("%d area(s) subscribed — Enter to manage", n)
			},
		},
```

- [ ] **Step 2: Handle Enter on "Browse Areas" in `updateRecordEdit()`**

In `update_records.go`, in the `ftDisplay` block of `updateRecordEdit()`, after the hub network "Areas" handler (lines 208-213), add:

```go
			// V3Net leaf "Browse Areas" field opens the area browser.
			if m.recordType == "v3netleaf" && f.Label == "Browse Areas" {
				if m.recordEditIdx >= 0 && m.recordEditIdx < len(m.configs.V3Net.Leaves) {
					leaf := m.configs.V3Net.Leaves[m.recordEditIdx]
					return m.enterAreaBrowser(leaf.HubURL, leaf.Network, modeRecordEdit)
				}
			}
```

- [ ] **Step 3: Verify it compiles**

Run: `go build ./internal/configeditor/...`
Expected: success

- [ ] **Step 4: Commit**

```bash
git add internal/configeditor/fields_v3net.go internal/configeditor/update_records.go
git commit -m "feat(configeditor): wire area browser into leaf subscription edit view"
```

---

### Task 7: Integration Testing

**Files:**
- Test manually (no automated test file — this is TUI integration)

- [ ] **Step 1: Run the full test suite**

Run: `go test ./internal/configeditor/... -v`
Expected: all existing tests pass

- [ ] **Step 2: Run vet and formatting checks**

Run: `go vet ./internal/configeditor/... && gofmt -l internal/configeditor/`
Expected: no issues, no files listed

- [ ] **Step 3: Run the full project test suite**

Run: `go test ./...`
Expected: all tests pass

- [ ] **Step 4: Commit any fix-ups**

If any lint/vet/test issues were found and fixed:

```bash
git add -A
git commit -m "fix(configeditor): address lint and test issues in area browser"
```

---

### Task 8: Final Cleanup and Documentation

**Files:**
- Verify all new files are under 300 lines per CLAUDE.md guidelines

- [ ] **Step 1: Check file sizes**

Run: `wc -l internal/configeditor/update_v3net_area_browser.go internal/configeditor/view_v3net_area_browser.go`
Expected: both under 300 lines. If either exceeds 300, split into focused sub-files.

- [ ] **Step 2: Run full build**

Run: `go build ./...`
Expected: success

- [ ] **Step 3: Final commit if any cleanup was needed**

```bash
git add -A
git commit -m "chore(configeditor): area browser cleanup and file size compliance"
```
