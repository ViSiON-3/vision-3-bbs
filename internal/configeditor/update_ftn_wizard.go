package configeditor

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/ViSiON-3/vision-3-bbs/internal/ftn"
)

// ftnWizardPickerVisible is how many picker rows fit on screen at once.
const ftnWizardPickerVisible = 12

// enterFTNWizard opens the FTN setup wizard. With networks already configured
// it offers the choice of adding another or editing one of them; on a fresh
// system it goes straight to the blank form, since there is nothing to pick
// between.
func (m Model) enterFTNWizard() (Model, tea.Cmd) {
	if keys := m.configuredFTNNetworkKeys(); len(keys) > 0 {
		m.ftnWizardPickerKeys = keys
		m.ftnWizardPickerCursor = 0
		m.ftnWizardPickerScroll = 0
		m.mode = modeFTNWizardPicker
		return m, nil
	}
	return m.startFTNWizardAdd()
}

// configuredFTNNetworkKeys returns the ftn.json network keys, sorted.
func (m Model) configuredFTNNetworkKeys() []string {
	if m.configs == nil || len(m.configs.FTN.Networks) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m.configs.FTN.Networks))
	for k := range m.configs.FTN.Networks {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// startFTNWizardAdd initializes a blank wizard for adding a new network.
func (m Model) startFTNWizardAdd() (Model, tea.Cmd) {
	origin := ""
	if m.configs != nil {
		origin = m.configs.Server.BoardName
		if host := m.configs.Server.SSHHost; host != "" {
			origin += " - " + host
		} else if host := m.configs.Server.TelnetHost; host != "" {
			origin += " - " + host
		}
	}

	m.ftnWizard = &ftnWizardState{
		hubPort:       24554,
		originLine:    origin,
		autoJoinAreas: true,
	}
	m.ftnWizardFields = m.fieldsFTNWizard()
	m.editField = 0
	m.fieldScroll = 0
	m.mode = modeFTNWizardForm
	return m, nil
}

// updateFTNWizardPicker handles the add-new vs edit-existing choice. Entry 0
// is "add a new network"; the rest are configured networks.
func (m Model) updateFTNWizardPicker(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	total := len(m.ftnWizardPickerKeys) + 1

	if cursor, ok := listNavKey(msg, m.ftnWizardPickerCursor, total); ok {
		m.ftnWizardPickerCursor = cursor
		m.ftnWizardPickerScroll = clampListScroll(cursor, m.ftnWizardPickerScroll, ftnWizardPickerVisible)
		return m, nil
	}

	switch msg.Type {
	case tea.KeyEnter:
		if m.ftnWizardPickerCursor == 0 {
			return m.startFTNWizardAdd()
		}
		key := m.ftnWizardPickerKeys[m.ftnWizardPickerCursor-1]
		return m.startFTNWizardEdit(key)

	case tea.KeyEscape:
		m.mode = modeCategoryMenu
		return m, nil
	}

	if strings.EqualFold(msg.String(), "n") {
		return m.startFTNWizardAdd()
	}
	return m, nil
}

// startFTNWizardEdit loads an already-configured network into the wizard so the
// sysop can see what was set up and change it.
func (m Model) startFTNWizardEdit(netKey string) (Model, tea.Cmd) {
	net, ok := m.configs.FTN.Networks[netKey]
	if !ok {
		m.message = fmt.Sprintf("Network %q is no longer configured", netKey)
		m.mode = modeCategoryMenu
		return m, nil
	}

	w := &ftnWizardState{
		editingKey:     netKey,
		networkName:    netKey,
		ownAddress:     net.OwnAddress,
		hubPort:        24554,
		autoJoinAreas:  true,
		subscribedTags: make(map[string]bool),
	}

	// Zone comes from the configured address, since ftn.json does not store
	// it separately and binkd.conf needs it on save.
	if zone, err := ftn.ParseAddress(net.OwnAddress); err == nil {
		w.zone = zone.Zone
	}

	// Hub details live on the first link; later links are other systems this
	// node feeds and are left untouched.
	if len(net.Links) > 0 {
		link := net.Links[0]
		w.hubAddress = link.Address
		w.hubHostname = link.Hostname
		if link.Port > 0 {
			w.hubPort = link.Port
		}
		w.areafixPassword = link.AreafixPassword
		w.sessionPassword = link.SessionPassword
		w.packetPassword = link.PacketPassword
	}

	// Description comes from the network's conference, which is where the
	// wizard put it on the way in.
	for _, c := range m.configs.Conferences {
		if strings.EqualFold(c.Name, netKey) || strings.EqualFold(c.Name, net.OwnAddress) {
			w.networkDesc = c.Description
			break
		}
	}

	// Existing subscriptions, and the newscan default they were created with.
	autoJoinSeen := false
	for _, area := range m.configs.MsgAreas {
		if !strings.EqualFold(area.Network, netKey) || area.EchoTag == "" {
			continue
		}
		w.subscribedTags[strings.ToUpper(area.EchoTag)] = true
		if !autoJoinSeen {
			w.autoJoinAreas = area.AutoJoin
			autoJoinSeen = true
		}
	}

	// Registry data (echolist and nodelist URLs, description) if this network
	// is one we ship an entry for, so Echo Areas and Node Lookup still work.
	if regNets, err := ftn.LoadRegistry(); err == nil {
		for i := range regNets {
			if !strings.EqualFold(regNets[i].Name, netKey) {
				continue
			}
			reg := regNets[i]
			w.registryEntry = &reg
			w.echolistURL = reg.EcholistURL
			w.nodelistURL = reg.NodelistURL
			w.coordinator = reg.Coordinator
			w.coordinatorEmail = reg.CoordinatorEmail
			w.infoURL = reg.InfoURL
			if w.networkDesc == "" {
				w.networkDesc = reg.Description
			}
			if w.zone == 0 {
				w.zone = reg.Zone
			}
			break
		}
	}

	m.ftnWizard = w
	m.ftnWizardFields = m.fieldsFTNWizard()
	m.editField = 0
	m.fieldScroll = 0
	m.mode = modeFTNWizardForm
	return m, nil
}

// updateFTNWizardForm handles key events in the FTN wizard form navigation mode.
func (m Model) updateFTNWizardForm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if len(m.ftnWizardFields) == 0 {
		if msg.Type == tea.KeyEscape {
			m.mode = modeCategoryMenu
		}
		return m, nil
	}

	switch msg.Type {
	case tea.KeyTab, tea.KeyEnter:
		f := m.ftnWizardFields[m.editField]

		if f.Type == ftYesNo {
			m.toggleFTNWizardYesNo(f)
			return m, nil
		}

		// "Network" field → open network browser. Not while editing: the
		// wizard is bound to one configured network, and swapping the identity
		// underneath the loaded fields would write them to the wrong one.
		if f.Type == ftDisplay && f.Label == "Network" {
			if m.ftnWizard.editing() {
				m.message = "Network name is fixed while editing — ESC and choose Add a new network instead"
				return m, nil
			}
			return m.enterFTNNetworkBrowser()
		}

		// "Echo Areas" field → download echolist and open area browser.
		if f.Type == ftDisplay && f.Label == "Echo Areas" {
			return m.enterFTNAreaBrowser()
		}

		// "Node Lookup" field → run nodelist lookup.
		if f.Type == ftDisplay && f.Label == "Node Lookup" {
			return m.startFTNNodeLookup()
		}

		// Other display fields → just advance.
		if f.Type == ftDisplay {
			m.editField = m.nextFTNWizardField(1)
			m.clampFieldScroll(m.ftnWizardFields)
			return m, nil
		}

		// Editable field → start text input.
		return m.startFTNWizardFieldEdit()

	case tea.KeySpace:
		f := m.ftnWizardFields[m.editField]
		if f.Type == ftYesNo {
			m.toggleFTNWizardYesNo(f)
		}
		return m, nil

	case tea.KeyDown:
		m.editField = m.nextFTNWizardField(1)
		m.clampFieldScroll(m.ftnWizardFields)

	case tea.KeyUp:
		m.editField = m.nextFTNWizardField(-1)
		m.clampFieldScroll(m.ftnWizardFields)

	case tea.KeyEscape:
		if m.ftnWizard.hasData() {
			m.confirmYes = true
			m.mode = modeWizardExitConfirm
			return m, nil
		}
		m.mode = modeCategoryMenu
		return m, nil

	case tea.KeyPgDown:
		return m.submitFTNWizardForm()

	default:
		key := strings.ToUpper(msg.String())
		if key == "S" {
			return m.submitFTNWizardForm()
		}
	}
	return m, nil
}

// updateFTNWizardField handles key events when editing a text field in the FTN wizard.
func (m Model) updateFTNWizardField(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	f := m.ftnWizardFields[m.editField]

	switch msg.Type {
	case tea.KeyEnter, tea.KeyTab, tea.KeyDown:
		if err := m.applyFTNWizardFieldValue(f); err != nil {
			m.message = fmt.Sprintf("Invalid: %v", err)
			return m, nil
		}
		m.textInput.Blur()
		m.mode = modeFTNWizardForm
		m.editField = m.nextFTNWizardField(1)
		m.clampFieldScroll(m.ftnWizardFields)
		return m, nil

	case tea.KeyUp:
		if err := m.applyFTNWizardFieldValue(f); err != nil {
			m.message = fmt.Sprintf("Invalid: %v", err)
			return m, nil
		}
		m.textInput.Blur()
		m.mode = modeFTNWizardForm
		m.editField = m.nextFTNWizardField(-1)
		m.clampFieldScroll(m.ftnWizardFields)
		return m, nil

	case tea.KeyEscape:
		m.textInput.Blur()
		m.mode = modeFTNWizardForm
		return m, nil

	default:
		if f.Type == ftYesNo {
			if len(msg.Runes) == 1 {
				ch := msg.Runes[0]
				switch ch {
				case 'y', 'Y':
					m.textInput.SetValue("Y")
				case 'n', 'N':
					m.textInput.SetValue("N")
				}
				if err := m.applyFTNWizardFieldValue(f); err == nil {
					m.textInput.Blur()
					m.mode = modeFTNWizardForm
					m.editField = m.nextFTNWizardField(1)
					m.clampFieldScroll(m.ftnWizardFields)
				}
				return m, nil
			}
			return m, nil
		}

		if f.Type == ftInteger {
			if len(msg.Runes) == 1 {
				ch := msg.Runes[0]
				if (ch < '0' || ch > '9') && ch != '-' {
					return m, nil
				}
			}
		}

		var cmd tea.Cmd
		m.textInput, cmd = m.textInput.Update(msg)
		return m, cmd
	}
}

// toggleFTNWizardYesNo flips the value of a boolean (yes/no) FTN wizard field.
func (m *Model) toggleFTNWizardYesNo(f fieldDef) {
	if f.Get != nil && f.Set != nil {
		if f.Get() == "Y" {
			_ = f.Set("N") // Y/N field setters never fail
		} else {
			_ = f.Set("Y") // Y/N field setters never fail
		}
		m.message = ""
	}
}

// nextFTNWizardField returns the next field index in the given direction,
// wrapping around.
func (m Model) nextFTNWizardField(dir int) int {
	n := len(m.ftnWizardFields)
	if n == 0 {
		return 0
	}
	idx := m.editField + dir
	if idx > n-1 {
		idx = 0
	} else if idx < 0 {
		idx = n - 1
	}
	return idx
}

// startFTNWizardFieldEdit begins text input for the current FTN wizard field.
func (m Model) startFTNWizardFieldEdit() (Model, tea.Cmd) {
	f := m.ftnWizardFields[m.editField]
	if f.Type == ftDisplay {
		return m, nil
	}

	val := f.Get()
	m.mode = modeFTNWizardField
	m.textInput.SetValue(val)
	m.textInput.CharLimit = f.Width
	m.textInput.Width = f.Width
	m.textInput.EchoMode = textinput.EchoNormal
	m.textInput.Placeholder = ""
	m.textInput.CursorEnd()
	m.textInput.Focus()

	return m, textinput.Blink
}

// applyFTNWizardFieldValue validates and applies the current text input value.
func (m *Model) applyFTNWizardFieldValue(f fieldDef) error {
	val := m.textInput.Value()

	switch f.Type {
	case ftInteger:
		n, err := strconv.Atoi(val)
		if err != nil {
			return fmt.Errorf("not a number")
		}
		if n < f.Min || n > f.Max {
			return fmt.Errorf("must be %d-%d", f.Min, f.Max)
		}
	case ftYesNo:
		upper := strings.ToUpper(val)
		if upper != "Y" && upper != "N" {
			return fmt.Errorf("must be Y or N")
		}
		val = upper
	}

	if f.Set != nil {
		if err := f.Set(val); err != nil {
			return err
		}
		m.message = ""
	}
	return nil
}

// submitFTNWizardForm validates the form and triggers save.
func (m Model) submitFTNWizardForm() (Model, tea.Cmd) {
	if err := m.validateFTNWizard(); err != nil {
		m.message = err.Error()
		return m, nil
	}
	if m.ftnWizard.selectedAreaCount() == 0 {
		m.message = "Select at least one echo area"
		return m, nil
	}
	return m.confirmFTNWizard()
}

// enterFTNAreaBrowser starts the echolist download or opens the browser
// if areas are already cached.
func (m Model) enterFTNAreaBrowser() (Model, tea.Cmd) {
	w := m.ftnWizard

	// If areas already fetched, go straight to browser.
	if w.areasFetched {
		m.ftnAreaBrowserAreas = w.availableAreas
		m.ftnAreaBrowserSelected = w.selectedAreas
		m.ftnAreaBrowserCursor = 0
		m.ftnAreaBrowserScroll = 0
		m.ftnAreaBrowserError = ""
		m.mode = modeFTNAreaBrowser
		return m, nil
	}

	// Need an echolist URL.
	url := w.echolistURL
	if url == "" {
		m.message = "No echolist URL available for this network"
		return m, nil
	}

	m.ftnAreaBrowserLoading = true
	m.ftnAreaBrowserError = ""
	m.mode = modeFTNAreaDownloading
	return m, fetchFTNEcholist(url, w.registryEntry)
}
