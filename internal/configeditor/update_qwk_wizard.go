package configeditor

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/qwk"
	"github.com/ViSiON-3/vision-3-bbs/internal/qwknet"
)

// qwkListVisible is how many rows the QWK browsers show at once.
const qwkListVisible = 12

// enterQWKWizard opens the wizard: blank for a new network, or loaded from
// qwknet.json when editKey names a configured one.
func (m Model) enterQWKWizard(editKey string) (Model, tea.Cmd) {
	w := &qwkWizardState{port: 21, schedule: defaultQWKPollSchedule, autoJoin: true}
	if editKey != "" {
		nc, ok := m.configs.QWKNet.Networks[editKey]
		if !ok {
			m.message = fmt.Sprintf("QWK network %q is no longer configured", editKey)
			return m, nil
		}
		w.editingKey = editKey
		w.networkKey = editKey
		w.networkName = nc.Name
		if w.networkName == "" {
			w.networkName = editKey
		}
		w.hubID = nc.HubID
		w.host = nc.Host
		if nc.Port > 0 {
			w.port = nc.Port
		}
		w.loginName = nc.Username
		w.password = nc.Password
		w.tagline = nc.Tagline
		w.existingConfs = m.qwkConferenceNumbersFor(editKey)
		for i := range m.configs.Events.Events {
			if m.configs.Events.Events[i].ID == qwkPollEventPrefix+editKey {
				w.schedule = m.configs.Events.Events[i].Schedule
			}
		}
		if nets, err := qwknet.LoadRegistry(); err == nil {
			for i := range nets {
				if strings.EqualFold(nets[i].HubID, nc.HubID) && strings.EqualFold(nets[i].Host, nc.Host) {
					w.known = &nets[i]
					break
				}
			}
		}
	} else if m.configs != nil && m.configs.Server.BoardName != "" {
		w.tagline = m.configs.Server.BoardName
	}
	m.qwkWizard = w
	m.qwkWizardFields = m.fieldsQWKWizard()
	m.editField = 0
	m.fieldScroll = 0
	m.mode = modeQWKWizardForm
	return m, nil
}

// updateQWKWizardForm handles navigation on the wizard form.
func (m Model) updateQWKWizardForm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if len(m.qwkWizardFields) == 0 {
		if msg.Type == tea.KeyEscape {
			m.mode = modeCategoryMenu
		}
		return m, nil
	}
	switch msg.Type {
	case tea.KeyTab, tea.KeyEnter:
		f := m.qwkWizardFields[m.editField]
		if f.Type == ftYesNo {
			m.toggleFTNWizardYesNo(f)
			return m, nil
		}
		if f.Type == ftDisplay {
			switch f.Label {
			case "Network":
				if m.qwkWizard.editing() {
					m.message = "Network is fixed while editing — ESC and choose QWK Network Wizard to add another"
					return m, nil
				}
				return m.enterQWKNetworkBrowser()
			case "Conferences":
				return m.enterQWKConfBrowser()
			}
			m.editField = m.nextQWKWizardField(1)
			m.clampFieldScroll(m.qwkWizardFields)
			return m, nil
		}
		return m.startQWKWizardFieldEdit()

	case tea.KeySpace:
		if f := m.qwkWizardFields[m.editField]; f.Type == ftYesNo {
			m.toggleFTNWizardYesNo(f)
		}
		return m, nil

	case tea.KeyDown:
		m.editField = m.nextQWKWizardField(1)
		m.clampFieldScroll(m.qwkWizardFields)

	case tea.KeyUp:
		m.editField = m.nextQWKWizardField(-1)
		m.clampFieldScroll(m.qwkWizardFields)

	case tea.KeyEscape:
		if m.qwkWizard.hasData() {
			m.confirmYes = true
			m.wizardExitSource = modeQWKWizardForm
			m.mode = modeWizardExitConfirm
			return m, nil
		}
		m.mode = modeCategoryMenu
		return m, nil

	case tea.KeyPgDown:
		return m.submitQWKWizardForm()

	default:
		if strings.EqualFold(msg.String(), "s") {
			return m.submitQWKWizardForm()
		}
	}
	return m, nil
}

// updateQWKWizardField handles text entry on one wizard field.
func (m Model) updateQWKWizardField(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	f := m.qwkWizardFields[m.editField]
	apply := func(dir int) (tea.Model, tea.Cmd) {
		if err := m.applyQWKWizardFieldValue(f); err != nil {
			m.message = fmt.Sprintf("Invalid: %v", err)
			return m, nil
		}
		m.textInput.Blur()
		m.mode = modeQWKWizardForm
		m.editField = m.nextQWKWizardField(dir)
		m.clampFieldScroll(m.qwkWizardFields)
		return m, nil
	}
	switch msg.Type {
	case tea.KeyEnter, tea.KeyTab, tea.KeyDown:
		return apply(1)
	case tea.KeyUp:
		return apply(-1)
	case tea.KeyEscape:
		m.textInput.Blur()
		m.mode = modeQWKWizardForm
		return m, nil
	default:
		if f.Type == ftInteger && len(msg.Runes) == 1 {
			if ch := msg.Runes[0]; ch < '0' || ch > '9' {
				return m, nil
			}
		}
		var cmd tea.Cmd
		m.textInput, cmd = m.textInput.Update(msg)
		return m, cmd
	}
}

// nextQWKWizardField steps the cursor, wrapping around.
func (m Model) nextQWKWizardField(dir int) int {
	n := len(m.qwkWizardFields)
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

// startQWKWizardFieldEdit begins text input on the current field.
func (m Model) startQWKWizardFieldEdit() (Model, tea.Cmd) {
	f := m.qwkWizardFields[m.editField]
	if f.Type == ftDisplay {
		return m, nil
	}
	m.mode = modeQWKWizardField
	m.textInput.SetValue(f.Get())
	m.textInput.CharLimit = f.Width
	m.textInput.Width = f.Width
	m.textInput.EchoMode = textinput.EchoNormal
	m.textInput.Placeholder = ""
	m.textInput.CursorEnd()
	m.textInput.Focus()
	return m, textinput.Blink
}

// applyQWKWizardFieldValue validates and stores the text input value.
func (m *Model) applyQWKWizardFieldValue(f fieldDef) error {
	val := m.textInput.Value()
	if f.Type == ftInteger {
		n, err := strconv.Atoi(val)
		if err != nil {
			return fmt.Errorf("not a number")
		}
		if n < f.Min || n > f.Max {
			return fmt.Errorf("must be %d-%d", f.Min, f.Max)
		}
	}
	if f.Set != nil {
		if err := f.Set(val); err != nil {
			return err
		}
		m.message = ""
	}
	return nil
}

// submitQWKWizardForm validates and saves.
func (m Model) submitQWKWizardForm() (Model, tea.Cmd) {
	if err := m.validateQWKWizard(); err != nil {
		m.message = err.Error()
		return m, nil
	}
	return m.confirmQWKWizard()
}

// --- Known network browser ---

// enterQWKNetworkBrowser opens the list of networks the registry knows.
func (m Model) enterQWKNetworkBrowser() (Model, tea.Cmd) {
	nets, err := qwknet.LoadRegistry()
	if err != nil {
		m.message = fmt.Sprintf("Failed to load QWK network registry: %v", err)
		return m, nil
	}
	m.qwkNetBrowserNets = nets
	m.qwkNetBrowserCur = 0
	m.qwkNetBrowserScrl = 0
	m.mode = modeQWKNetworkBrowser
	return m, nil
}

// updateQWKNetworkBrowser picks a known network or returns for a custom one.
func (m Model) updateQWKNetworkBrowser(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	total := len(m.qwkNetBrowserNets)
	if cursor, ok := listNavKey(msg, m.qwkNetBrowserCur, total); ok {
		m.qwkNetBrowserCur = cursor
		m.qwkNetBrowserScrl = clampListScroll(cursor, m.qwkNetBrowserScrl, qwkListVisible)
		return m, nil
	}
	switch msg.Type {
	case tea.KeyEnter:
		if total > 0 && m.qwkNetBrowserCur < total {
			net := m.qwkNetBrowserNets[m.qwkNetBrowserCur]
			if _, exists := m.configs.QWKNet.Networks[net.Key]; exists {
				m.message = fmt.Sprintf("%s is already configured — edit it under QWK Networks, or press C for a custom hub", net.Name)
				return m, nil
			}
			m.populateQWKWizardFromRegistry(net)
		}
		m.mode = modeQWKWizardForm
		return m, nil
	case tea.KeyEscape:
		m.mode = modeQWKWizardForm
		return m, nil
	}
	if strings.EqualFold(msg.String(), "c") {
		m.qwkWizard.known = nil
		m.qwkWizardFields = m.fieldsQWKWizard()
		m.mode = modeQWKWizardForm
	}
	return m, nil
}

// populateQWKWizardFromRegistry fills the form from a known network.
func (m *Model) populateQWKWizardFromRegistry(net qwknet.KnownNetwork) {
	w := m.qwkWizard
	n := net
	w.known = &n
	w.networkName = net.Name
	w.networkKey = net.Key
	w.hubID = net.HubID
	w.host = net.Host
	if net.Port > 0 {
		w.port = net.Port
	}
	w.available = nil
	w.selected = nil
	w.confsFetched = false
	w.confsErr = ""
	m.qwkWizardFields = m.fieldsQWKWizard()
}

// --- Conference browser ---

// qwkConfsMsg is the result of asking the hub for its conference list.
type qwkConfsMsg struct {
	gen   uint64
	confs []qwk.ConferenceInfo
	err   error
}

// enterQWKConfBrowser opens the conference picker. A known network's list
// is used as-is; otherwise the hub is asked, which needs the connection
// details filled in first.
func (m Model) enterQWKConfBrowser() (Model, tea.Cmd) {
	w := m.qwkWizard
	if w.confsFetched {
		return m.openQWKConfBrowser(), nil
	}
	if w.known != nil && len(w.known.Conferences) > 0 {
		m.setQWKConferences(w.known.Conferences)
		return m.openQWKConfBrowser(), nil
	}
	if w.hubID == "" || w.host == "" || w.password == "" {
		m.message = "Fill in Hub QWK-ID, Hub Host and Password first — the list comes from the hub"
		return m, nil
	}
	nc := config.QWKNetworkConfig{
		HubID: w.hubID, Host: w.host, Port: w.port, Username: w.loginName, Password: w.password,
		TimeoutSeconds: 60,
	}
	paths := m.configs.QWKNet
	root, err := filepath.Abs(filepath.Join(m.configPath, ".."))
	if err != nil {
		root = filepath.Join(m.configPath, "..")
	}
	paths.ResolvePaths(root)
	key := w.networkKey
	if key == "" {
		key = "qwkwizard"
	}
	node, err := qwknet.New(key, nc, paths, m.systemQWKID(), nil, nil)
	if err != nil {
		m.message = err.Error()
		return m, nil
	}
	w.fetchGen++
	w.fetching = true
	w.confsErr = ""
	m.qwkConfBrowserErr = ""
	m.mode = modeQWKConfFetching
	gen := w.fetchGen
	return m, func() tea.Msg {
		confs, err := node.FetchConferences(context.Background())
		return qwkConfsMsg{gen: gen, confs: confs, err: err}
	}
}

// setQWKConferences installs a conference list, ticking the ones already
// mirrored by an area and keeping ticks from an earlier visit.
func (m *Model) setQWKConferences(confs []qwk.ConferenceInfo) {
	w := m.qwkWizard
	prev := make(map[int]bool)
	for i, sel := range w.selected {
		if sel && i < len(w.available) {
			prev[w.available[i].Number] = true
		}
	}
	w.available = confs
	w.selected = make([]bool, len(confs))
	for i, c := range confs {
		w.selected[i] = prev[c.Number] || w.existingConfs[c.Number]
	}
	w.confsFetched = true
	w.confsErr = ""
}

// openQWKConfBrowser shows the picker over a working copy of the ticks.
func (m Model) openQWKConfBrowser() Model {
	m.qwkConfBrowserSel = append([]bool(nil), m.qwkWizard.selected...)
	m.qwkConfBrowserCur = 0
	m.qwkConfBrowserScrl = 0
	m.mode = modeQWKConfBrowser
	return m
}

// handleQWKConfsMsg receives the hub's answer.
func (m Model) handleQWKConfsMsg(msg qwkConfsMsg) (tea.Model, tea.Cmd) {
	w := m.qwkWizard
	if w == nil || msg.gen != w.fetchGen || m.mode != modeQWKConfFetching {
		return m, nil // stale, or the sysop pressed ESC
	}
	w.fetching = false
	if msg.err != nil {
		w.confsErr = msg.err.Error()
		m.qwkConfBrowserErr = w.confsErr
		m.mode = modeQWKConfBrowser
		return m, nil
	}
	m.setQWKConferences(msg.confs)
	return m.openQWKConfBrowser(), nil
}

// updateQWKConfFetching waits for the hub; ESC abandons the attempt.
func (m Model) updateQWKConfFetching(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyEscape {
		m.qwkWizard.fetching = false
		m.qwkWizard.fetchGen++
		m.mode = modeQWKWizardForm
	}
	return m, nil
}

// updateQWKConfBrowser toggles conferences; Enter commits, ESC discards.
func (m Model) updateQWKConfBrowser(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	w := m.qwkWizard
	total := len(w.available)
	if m.qwkConfBrowserErr != "" && total == 0 {
		switch {
		case msg.Type == tea.KeyEscape:
			m.mode = modeQWKWizardForm
		case strings.EqualFold(msg.String(), "r"):
			w.confsFetched = false
			return m.enterQWKConfBrowser()
		}
		return m, nil
	}
	if cursor, ok := listNavKey(msg, m.qwkConfBrowserCur, total); ok {
		m.qwkConfBrowserCur = cursor
		m.qwkConfBrowserScrl = clampListScroll(cursor, m.qwkConfBrowserScrl, qwkListVisible)
		return m, nil
	}
	switch msg.Type {
	case tea.KeySpace:
		if total > 0 && m.qwkConfBrowserCur < total {
			i := m.qwkConfBrowserCur
			m.qwkConfBrowserSel[i] = !m.qwkConfBrowserSel[i]
		}
	case tea.KeyEnter:
		w.selected = m.qwkConfBrowserSel
		m.qwkWizardFields = m.fieldsQWKWizard()
		m.mode = modeQWKWizardForm
	case tea.KeyEscape:
		m.mode = modeQWKWizardForm
	default:
		switch strings.ToUpper(msg.String()) {
		case "A":
			for i := range m.qwkConfBrowserSel {
				m.qwkConfBrowserSel[i] = true
			}
		case "N":
			for i := range m.qwkConfBrowserSel {
				m.qwkConfBrowserSel[i] = false
			}
		}
	}
	return m, nil
}
