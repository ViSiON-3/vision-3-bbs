package configeditor

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/qwk"
	"github.com/ViSiON-3/vision-3-bbs/internal/qwknet"
)

// qwkTestModel is a model whose saveAll writes into a temp configs dir.
func qwkTestModel(t *testing.T) Model {
	t.Helper()
	root := t.TempDir()
	cfgDir := filepath.Join(root, "configs")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	return Model{
		configPath: cfgDir,
		configs: &allConfigs{
			Server: config.ServerConfig{BoardName: "Test Board", QWKID: "TESTBBS"},
			Events: templateEvents(),
		},
	}
}

func TestQWKWizard_SaveCreatesNetworkAreasAndEvent(t *testing.T) {
	m := qwkTestModel(t)
	m, _ = m.enterQWKWizard("")
	if m.mode != modeQWKWizardForm || m.qwkWizard == nil {
		t.Fatalf("mode = %v", m.mode)
	}
	nets, _ := m.enterQWKNetworkBrowser()
	if len(nets.qwkNetBrowserNets) == 0 {
		t.Fatal("registry empty")
	}
	m = nets
	m.populateQWKWizardFromRegistry(m.qwkNetBrowserNets[0])
	w := m.qwkWizard
	if w.networkKey != "dovenet" || w.hubID != "VERT" || w.host != "vert.synchro.net" {
		t.Fatalf("prefill: %+v", w)
	}
	w.password = "secret"
	w.schedule = "*/20 * * * *"

	// Choose two conferences from the preset list.
	m, _ = m.enterQWKConfBrowser()
	if m.mode != modeQWKConfBrowser || len(w.available) < 2 {
		t.Fatalf("conf browser: mode=%v available=%d", m.mode, len(w.available))
	}
	m.qwkConfBrowserSel[0] = true
	m.qwkConfBrowserSel[5] = true
	w.selected = m.qwkConfBrowserSel

	m, _ = m.submitQWKWizardForm()
	if !strings.Contains(m.message, "saved") || strings.HasPrefix(m.message, "SAVE ERROR") {
		t.Fatalf("message = %q", m.message)
	}

	nc, ok := m.configs.QWKNet.Networks["dovenet"]
	if !ok || !nc.Enabled || nc.Password != "secret" || nc.HubID != "VERT" {
		t.Fatalf("network config = %+v", nc)
	}
	if len(m.configs.MsgAreas) != 2 {
		t.Fatalf("areas = %+v", m.configs.MsgAreas)
	}
	a := m.configs.MsgAreas[0]
	if a.AreaType != "qwknet" || a.Network != "dovenet" || a.QWKConference != 2001 || a.Tag != "DOVENET_GENERAL" || a.EchoTag != "General" {
		t.Errorf("area 0 = %+v", a)
	}
	if a.BasePath != filepath.Join("msgbases", "qwk.dovenet_2001_general") || a.ConferenceID == 0 {
		t.Errorf("area 0 paths = %+v", a)
	}
	ev := findEvent(m.configs.Events, "qwknet_poll_dovenet")
	if ev == nil || !ev.Enabled || ev.Schedule != "*/20 * * * *" || ev.Args[0] != "qwk-poll" || ev.Args[2] != "dovenet" {
		t.Fatalf("event = %+v", ev)
	}
	if !m.configs.Events.Enabled {
		t.Error("scheduler not enabled")
	}
	for _, f := range []string{"qwknet.json", "message_areas.json", "events.json", "conferences.json"} {
		if _, err := os.Stat(filepath.Join(m.configPath, f)); err != nil {
			t.Errorf("%s not written", f)
		}
	}
	saved, err := config.LoadQWKNetConfig(m.configPath)
	if err != nil || saved.Networks["dovenet"].Host != "vert.synchro.net" {
		t.Fatalf("reload: %+v err=%v", saved, err)
	}
}

func TestQWKWizard_EditAddsOnlyNewConferences(t *testing.T) {
	m := qwkTestModel(t)
	m.configs.QWKNet.Networks = map[string]config.QWKNetworkConfig{
		"dovenet": {Enabled: true, Name: "DOVE-Net", HubID: "VERT", Host: "vert.synchro.net", Password: "pw", NoHeaders: true},
	}
	m.configs.MsgAreas = []message.MessageArea{
		{ID: 7, Tag: "DOVENET_GENERAL", AreaType: "qwknet", Network: "dovenet", QWKConference: 2001},
	}
	m, _ = m.enterQWKWizard("dovenet")
	w := m.qwkWizard
	if !w.editing() || w.known == nil || w.password != "pw" || !w.existingConfs[2001] {
		t.Fatalf("edit state = %+v", w)
	}
	m, _ = m.enterQWKConfBrowser()
	if !m.qwkConfBrowserSel[0] {
		t.Error("configured conference should start ticked")
	}
	m.qwkConfBrowserSel[1] = true
	w.selected = m.qwkConfBrowserSel
	m, _ = m.submitQWKWizardForm()
	if strings.HasPrefix(m.message, "SAVE ERROR") {
		t.Fatal(m.message)
	}
	if len(m.configs.MsgAreas) != 2 || m.configs.MsgAreas[1].QWKConference != 2002 {
		t.Fatalf("areas = %+v", m.configs.MsgAreas)
	}
	if !m.configs.QWKNet.Networks["dovenet"].NoHeaders {
		t.Error("edit dropped a setting the wizard does not ask about")
	}
}

// Adding a conference to a network the sysop switched off, or whose poll
// event they paused, must not turn either back on.
func TestQWKWizard_EditKeepsDisabledNetworkAndPausedEvent(t *testing.T) {
	for _, tc := range []struct {
		name       string
		netEnabled bool
	}{{"disabled network", false}, {"paused event only", true}} {
		t.Run(tc.name, func(t *testing.T) {
			m := qwkTestModel(t)
			m.configs.QWKNet.Networks = map[string]config.QWKNetworkConfig{
				"dovenet": {Enabled: tc.netEnabled, Name: "DOVE-Net", HubID: "VERT", Host: "vert.synchro.net", Password: "pw"},
			}
			paused := newQWKPollEvent("dovenet", "VERT", "")
			paused.Enabled = false
			m.configs.Events.Events = append(m.configs.Events.Events, paused)

			m, _ = m.enterQWKWizard("dovenet")
			w := m.qwkWizard
			m, _ = m.enterQWKConfBrowser()
			m.qwkConfBrowserSel[1] = true
			w.selected = m.qwkConfBrowserSel
			m, _ = m.submitQWKWizardForm()
			if strings.HasPrefix(m.message, "SAVE ERROR") {
				t.Fatal(m.message)
			}
			if got := m.configs.QWKNet.Networks["dovenet"].Enabled; got != tc.netEnabled {
				t.Errorf("network Enabled = %v, want %v", got, tc.netEnabled)
			}
			if e := findEvent(m.configs.Events, "qwknet_poll_dovenet"); e == nil || e.Enabled {
				t.Errorf("paused poll event was re-enabled: %+v", e)
			}
		})
	}
}

func TestQWKWizard_RejectsDuplicateKeyAndMissingPassword(t *testing.T) {
	m := qwkTestModel(t)
	m.configs.QWKNet.Networks = map[string]config.QWKNetworkConfig{"dovenet": {HubID: "VERT", Host: "h", Password: "p"}}
	m, _ = m.enterQWKWizard("")
	w := m.qwkWizard
	w.networkKey, w.hubID, w.host, w.password = "dovenet", "VERT", "h", "p"
	m, _ = m.submitQWKWizardForm()
	if !strings.Contains(m.message, "already exists") {
		t.Errorf("duplicate key accepted: %q", m.message)
	}
	w.networkKey, w.password = "other", ""
	m, _ = m.submitQWKWizardForm()
	if !strings.Contains(m.message, "Password") {
		t.Errorf("missing password accepted: %q", m.message)
	}
}

func TestQWKAreaSlugAndTag(t *testing.T) {
	cases := map[string]string{
		"General":                             "general",
		"Hardware/Software Help":              "hardware_software_help",
		"Synchronet Programming (JavaScript)": "synchronet_programming_j",
		"":                                    "c2001",
		"!!!":                                 "c2001",
	}
	for in, want := range cases {
		if got := qwkAreaSlug(in, 2001); got != want {
			t.Errorf("qwkAreaSlug(%q) = %q want %q", in, got, want)
		}
	}
	areas := []message.MessageArea{{Tag: "X_A"}, {Tag: "X_A2"}}
	if got := uniqueAreaTag(areas, "X_A"); got != "X_A3" {
		t.Errorf("uniqueAreaTag = %q", got)
	}
}

func TestQWKNetGlobalBadAreaMustExist(t *testing.T) {
	m := qwkTestModel(t)
	m.configs.MsgAreas = []message.MessageArea{{ID: 1, Tag: "LOCAL"}}
	m.recordType = "qwknet"
	m.recordEditIdx = -1
	var bad fieldDef
	for _, f := range m.buildRecordFields() {
		if f.Label == "Bad Area Tag" {
			bad = f
		}
	}
	if bad.Set == nil {
		t.Fatal("Bad Area Tag field missing")
	}
	if err := bad.Set("nope"); err == nil {
		t.Error("unknown area accepted")
	}
	if err := bad.Set("local"); err != nil || m.configs.QWKNet.BadAreaTag != "LOCAL" {
		t.Errorf("existing area rejected or spelling not canonicalized: %v %q", err, m.configs.QWKNet.BadAreaTag)
	}
	if err := bad.Set(""); err != nil || m.configs.QWKNet.BadAreaTag != "" {
		t.Errorf("blank rejected: %v", err)
	}
}

func TestQWKEvents_WireRefreshRename(t *testing.T) {
	ev := templateEvents()
	wireQWKEvents(&ev, "dovenet", "VERT", "", true, true)
	e := findEvent(ev, "qwknet_poll_dovenet")
	if e == nil || !e.Enabled || e.Schedule != defaultQWKPollSchedule || !ev.Enabled {
		t.Fatalf("wire: %+v enabled=%v", e, ev.Enabled)
	}
	// A tuned schedule survives a blank re-wire; the hub name refreshes.
	e.Schedule = "0 * * * *"
	e.Args = append(e.Args, "-v")
	wireQWKEvents(&ev, "dovenet", "VERT2", "", false, true)
	e = findEvent(ev, "qwknet_poll_dovenet")
	if e.Schedule != "0 * * * *" || !strings.Contains(e.Name, "VERT2") || e.Args[len(e.Args)-1] != "-v" {
		t.Errorf("re-wire lost tuning: %+v", e)
	}
	// An explicit schedule (the wizard's edited field) replaces it.
	wireQWKEvents(&ev, "dovenet", "VERT2", "*/10 * * * *", false, true)
	if e = findEvent(ev, "qwknet_poll_dovenet"); e.Schedule != "*/10 * * * *" {
		t.Errorf("edited schedule not applied: %+v", e)
	}

	// Refresh: a disabled network disables its event; a removed one too; an
	// enabled network without an event gets one.
	nets := map[string]config.QWKNetworkConfig{
		"dovenet": {Enabled: false, HubID: "VERT"},
		"fresh":   {Enabled: true, HubID: "HUB2"},
	}
	refreshQWKPollEvents(&ev, nets)
	if findEvent(ev, "qwknet_poll_dovenet").Enabled {
		t.Error("disabled network's event still enabled")
	}
	if f := findEvent(ev, "qwknet_poll_fresh"); f == nil || !f.Enabled {
		t.Error("enabled network without an event did not get one")
	}
	delete(nets, "fresh")
	refreshQWKPollEvents(&ev, nets)
	if findEvent(ev, "qwknet_poll_fresh").Enabled {
		t.Error("removed network's event still enabled")
	}

	renameQWKPollEvent(&ev, "dovenet", "dove")
	if findEvent(ev, "qwknet_poll_dovenet") != nil || findEvent(ev, "qwknet_poll_dove") == nil {
		t.Error("rename did not move the event")
	}
	if r := findEvent(ev, "qwknet_poll_dove"); r.Args[2] != "dove" {
		t.Errorf("rename did not retarget --network: %v", r.Args)
	}
}

func TestQWKNetworkRecordFieldsRenameCarriesAreasAndEvent(t *testing.T) {
	m := qwkTestModel(t)
	m.configs.QWKNet.Networks = map[string]config.QWKNetworkConfig{"old": {HubID: "VERT", Host: "h", Password: "p"}}
	m.configs.MsgAreas = []message.MessageArea{{ID: 1, Tag: "OLD_GEN", AreaType: "qwknet", Network: "old", QWKConference: 2001}}
	wireQWKEvents(&m.configs.Events, "old", "VERT", "", true, true)
	m.recordType = "qwknet"
	m.recordEditIdx = 0
	fields := m.buildRecordFields()
	if fields[0].Label != "Network Key" {
		t.Fatalf("first field = %q", fields[0].Label)
	}
	if err := fields[0].Set("new"); err != nil {
		t.Fatal(err)
	}
	if _, ok := m.configs.QWKNet.Networks["new"]; !ok || m.configs.MsgAreas[0].Network != "new" || findEvent(m.configs.Events, "qwknet_poll_new") == nil {
		t.Fatalf("rename incomplete: %+v areas=%+v", m.configs.QWKNet.Networks, m.configs.MsgAreas)
	}
	_ = qwk.ConferenceInfo{} // keep the import honest for the helper below
}

// F in the conference picker replaces a preset list with the hub's, keeping
// ticks by conference number; a failed refresh keeps the preset on screen.
func TestQWKWizard_RefreshPresetConferencesFromHub(t *testing.T) {
	m := qwkTestModel(t)
	m, _ = m.enterQWKWizard("")
	nets, err := qwknet.LoadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	m.populateQWKWizardFromRegistry(nets[0])
	m, _ = m.enterQWKConfBrowser()
	w := m.qwkWizard
	if m.mode != modeQWKConfBrowser || w.confsFromHub || len(w.available) != len(nets[0].Conferences) {
		t.Fatalf("preset list not shown: mode=%v fromHub=%v n=%d", m.mode, w.confsFromHub, len(w.available))
	}
	m.qwkConfBrowserSel[0] = true // 2001
	pressF := func(m Model) (Model, tea.Cmd) {
		t.Helper()
		next, cmd := m.updateQWKConfBrowser(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f")})
		return next.(Model), cmd
	}

	// No password yet: the refresh explains itself and fetches nothing.
	m, cmd := pressF(m)
	if cmd != nil || m.mode != modeQWKConfBrowser || !strings.Contains(m.message, "Password") {
		t.Fatalf("refresh without password: mode=%v msg=%q", m.mode, m.message)
	}

	w.password = "pw"
	m, cmd = pressF(m)
	if cmd == nil || m.mode != modeQWKConfFetching {
		t.Fatalf("refresh did not start a fetch: mode=%v", m.mode)
	}
	next, _ := m.handleQWKConfsMsg(qwkConfsMsg{gen: w.fetchGen, confs: []qwk.ConferenceInfo{
		{Number: 2001, Name: "General"}, {Number: 2099, Name: "Brand New"},
	}})
	m = next.(Model)
	if m.mode != modeQWKConfBrowser || !w.confsFromHub || len(w.available) != 2 {
		t.Fatalf("hub list not installed: mode=%v fromHub=%v avail=%+v", m.mode, w.confsFromHub, w.available)
	}
	if !m.qwkConfBrowserSel[0] || m.qwkConfBrowserSel[1] {
		t.Errorf("ticks did not carry over by number: %v", m.qwkConfBrowserSel)
	}

	// A failed refresh keeps the list and reports the error.
	m, _ = pressF(m)
	next, _ = m.handleQWKConfsMsg(qwkConfsMsg{gen: w.fetchGen, err: errors.New("530 login refused")})
	m = next.(Model)
	if m.mode != modeQWKConfBrowser || len(w.available) != 2 || !strings.Contains(m.qwkConfBrowserErr, "530") {
		t.Fatalf("failed refresh: mode=%v avail=%d err=%q", m.mode, len(w.available), m.qwkConfBrowserErr)
	}
	if !strings.Contains(m.View(), "Hub refresh failed") {
		t.Error("picker does not show the refresh error")
	}
}

// ESC in the V3Net wizard names that wizard as the exit dialog's source, so
// a source left over from an abandoned QWK wizard cannot capture its Y.
func TestWizardExitConfirm_V3NetWizardOverridesStaleSource(t *testing.T) {
	m := qwkTestModel(t)
	m.wizardExitSource = modeQWKWizardForm // left from an earlier QWK visit
	m.wizard = &wizardState{flow: "hub", netName: "mynet"}
	m.wizardFields = m.fieldsHubWizard()
	m.mode = modeWizardForm
	next, _ := m.updateWizardForm(tea.KeyMsg{Type: tea.KeyEscape})
	m = next.(Model)
	if m.mode != modeWizardExitConfirm || m.wizardExitSource != modeWizardForm {
		t.Fatalf("mode=%v source=%v, want the V3Net wizard as the source", m.mode, m.wizardExitSource)
	}
}

// A hub ID another network already uses is refused by the wizard and by the
// QWK Networks editor, since the two would share packet files.
func TestQWKHubIDMustBeUnique(t *testing.T) {
	m := qwkTestModel(t)
	m.configs.QWKNet.Networks = map[string]config.QWKNetworkConfig{
		"dovenet": {Enabled: true, Name: "DOVE-Net", HubID: "VERT", Host: "vert.synchro.net", Password: "pw"},
		"other":   {Enabled: true, Name: "Other", HubID: "HUB2", Host: "h", Password: "pw"},
	}
	m, _ = m.enterQWKWizard("")
	w := m.qwkWizard
	w.networkKey, w.networkName, w.hubID, w.host, w.password = "copy", "Copy", "VERT", "h", "pw"
	if err := m.validateQWKWizard(); err == nil || !strings.Contains(err.Error(), "dovenet") {
		t.Errorf("wizard accepted a hub ID in use: %v", err)
	}

	m.recordType = "qwknet"
	m.recordEditIdx = 1 // "other"
	for _, f := range m.buildRecordFields() {
		if f.Label != "Hub QWK-ID" {
			continue
		}
		if err := f.Set("VERT"); err == nil {
			t.Error("editor accepted a hub ID another network uses")
		}
		if err := f.Set("HUB2"); err != nil {
			t.Errorf("editor refused the network's own hub ID: %v", err)
		}
	}
}

// Opened from the QWK Networks list, the wizard returns there on ESC,
// discard and save; opened from the category menu, it returns there.
func TestQWKWizard_ReturnsToWhereItWasOpened(t *testing.T) {
	esc := tea.KeyMsg{Type: tea.KeyEscape}
	for _, from := range []editorMode{modeRecordList, modeCategoryMenu} {
		// ESC with nothing entered.
		m := qwkTestModel(t)
		m.mode = from
		m, _ = m.enterQWKWizard("")
		next, _ := m.updateQWKWizardForm(esc)
		if got := next.(Model).mode; got != from {
			t.Errorf("from %v: ESC on an empty wizard went to %v", from, got)
		}

		// ESC with data, then N to discard.
		m = qwkTestModel(t)
		m.mode = from
		m, _ = m.enterQWKWizard("")
		m.qwkWizard.hubID = "VERT"
		next, _ = m.updateQWKWizardForm(esc)
		m = next.(Model)
		if m.mode != modeWizardExitConfirm {
			t.Fatalf("from %v: ESC with data did not ask to save (mode %v)", from, m.mode)
		}
		next, _ = m.updateWizardExitConfirm(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
		if got := next.(Model).mode; got != from {
			t.Errorf("from %v: discard went to %v", from, got)
		}

		// A successful save.
		m = qwkTestModel(t)
		m.configs.QWKNet.Networks = map[string]config.QWKNetworkConfig{
			"dovenet": {Enabled: true, Name: "DOVE-Net", HubID: "VERT", Host: "vert.synchro.net", Password: "pw"},
		}
		m.mode = from
		m, _ = m.enterQWKWizard("dovenet")
		m, _ = m.confirmQWKWizard()
		if strings.HasPrefix(m.message, "SAVE ERROR") {
			t.Fatal(m.message)
		}
		if m.mode != from {
			t.Errorf("from %v: save went to %v", from, m.mode)
		}
	}
}
