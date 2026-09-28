package configeditor

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/qwk"
)

// gotoQWKField moves the QWK wizard cursor to the field labelled label.
func gotoQWKField(t *testing.T, m Model, label string) Model {
	t.Helper()
	for i := 0; m.qwkWizardFields[m.editField].Label != label; i++ {
		if i > len(m.qwkWizardFields) {
			t.Fatalf("no QWK wizard field %q", label)
		}
		m = press(t, m, "down")
	}
	return m
}

// setQWKField opens the QWK wizard field label, replaces its text with val
// and presses Enter.
func setQWKField(t *testing.T, m Model, label, val string) Model {
	t.Helper()
	m = press(t, gotoQWKField(t, m, label), "enter")
	return press(t, replaceText(t, m, val), "enter")
}

// TestQWKWizard_KeyboardFlowSavesNetwork drives the QWK wizard from the menu:
// pick DOVE-Net from the known networks, tick conferences from its preset
// list, enter the password, save, and check qwknet.json and the areas.
func TestQWKWizard_KeyboardFlowSavesNetwork(t *testing.T) {
	m, dir := newDiskModel(t)
	m.configs.Server.BoardName = "Test Board"
	m.configs.Server.QWKID = "TESTBBS"
	m = press(t, m, "5", "2")
	if m.mode != modeQWKWizardForm || m.qwkWizard.tagline != "Test Board" {
		t.Fatalf("mode=%v tagline=%q", m.mode, m.qwkWizard.tagline)
	}
	wantScreen(t, m, "QWK Network Wizard", "TESTBBS")

	m = press(t, m, "enter")
	if m.mode != modeQWKNetworkBrowser || len(m.qwkNetBrowserNets) == 0 {
		t.Fatalf("mode = %v", m.mode)
	}
	wantScreen(t, m, "Known QWK Networks", m.qwkNetBrowserNets[0].Name)
	m = press(t, m, "end", "home", "enter")
	if m.qwkWizard.networkKey != "dovenet" || m.qwkWizard.hubID != "VERT" {
		t.Fatalf("prefill: key=%q hub=%q", m.qwkWizard.networkKey, m.qwkWizard.hubID)
	}

	m = press(t, gotoQWKField(t, m, "Conferences"), "enter")
	if m.mode != modeQWKConfBrowser || len(m.qwkWizard.available) < 3 {
		t.Fatalf("mode=%v available=%d", m.mode, len(m.qwkWizard.available))
	}
	wantScreen(t, m, "Hub Conferences", m.qwkWizard.available[0].Name)
	m = press(t, m, "A", "N", "space", "down", "down", "space", "end", "home")
	m = press(t, m, "enter")
	if m.mode != modeQWKWizardForm || m.qwkWizard.selectedCount() != 2 {
		t.Fatalf("mode=%v selected=%d", m.mode, m.qwkWizard.selectedCount())
	}
	// Escape from the picker discards a toggle.
	m = press(t, m, "enter", "space", "esc")
	if m.qwkWizard.selectedCount() != 2 {
		t.Fatalf("escape kept a toggle: selected=%d", m.qwkWizard.selectedCount())
	}

	m = press(t, m, "S")
	if !strings.Contains(strings.ToLower(m.message), "password") {
		t.Errorf("missing password msg = %q", m.message)
	}
	m = setQWKField(t, m, "Password", "secret")
	m = setQWKField(t, m, "Poll Schedule", "*/15 * * * *")
	m = press(t, gotoQWKField(t, m, "Newscan Default"), "space")
	m = press(t, m, "pgdown")
	if _, ok := m.configs.QWKNet.Networks["dovenet"]; !ok {
		t.Fatalf("network not saved: %q", m.message)
	}

	ac := reloadConfigs(t, dir)
	nc := ac.QWKNet.Networks["dovenet"]
	if nc.Password != "secret" || nc.HubID != "VERT" || nc.Tagline != "Test Board" {
		t.Errorf("saved network = %+v", nc)
	}
	var qwkAreas int
	for _, a := range ac.MsgAreas {
		if a.AreaType == "qwknet" {
			qwkAreas++
			if a.AutoJoin {
				t.Errorf("area %s auto-joins despite Newscan Default N", a.Tag)
			}
		}
	}
	if qwkAreas != 2 {
		t.Errorf("qwk areas = %d, want 2", qwkAreas)
	}
	var sched string
	for _, e := range ac.Events.Events {
		if e.ID == "qwknet_poll_dovenet" {
			sched = e.Schedule
		}
	}
	if sched != "*/15 * * * *" {
		t.Errorf("poll schedule = %q", sched)
	}
}

// TestQWKWizard_CustomHubFetchesConferences pins the custom-hub path: the
// conference list needs connection details first, the fetch result fills the
// picker, errors are shown with a retry, and stale answers are ignored.
func TestQWKWizard_CustomHubFetchesConferences(t *testing.T) {
	m, _ := newDiskModel(t)
	m = press(t, m, "5", "2", "enter", "C")
	if m.mode != modeQWKWizardForm || m.qwkWizard.known != nil {
		t.Fatalf("custom: mode = %v", m.mode)
	}
	m = press(t, gotoQWKField(t, m, "Conferences"), "enter")
	if !strings.HasPrefix(m.message, "Fill in Hub QWK-ID") {
		t.Fatalf("msg = %q", m.message)
	}
	m = setQWKField(t, m, "Network Key", "mynet")
	m = setQWKField(t, m, "Hub QWK-ID", "HUB")
	m = setQWKField(t, m, "Hub Host", "hub.example")
	m = setQWKField(t, m, "Password", "pw")

	m = press(t, gotoQWKField(t, m, "Conferences"), "enter")
	if m.mode != modeQWKConfFetching {
		t.Fatalf("mode = %v (%q)", m.mode, m.message)
	}
	gen := m.qwkWizard.fetchGen
	wantScreen(t, m, "Hub Conferences")

	// A stale answer is ignored.
	m = asModel(t, first(m.Update(qwkConfsMsg{gen: gen - 1, confs: []qwk.ConferenceInfo{{Number: 1, Name: "Old"}}})))
	if m.mode != modeQWKConfFetching {
		t.Fatalf("stale answer applied: mode = %v", m.mode)
	}
	m = asModel(t, first(m.Update(qwkConfsMsg{gen: gen, err: errors.New("login refused")})))
	if m.mode != modeQWKConfBrowser || m.qwkConfBrowserErr != "login refused" {
		t.Fatalf("mode=%v err=%q", m.mode, m.qwkConfBrowserErr)
	}
	wantScreen(t, m, "login refused")
	m = press(t, m, "space", "r")
	if m.mode != modeQWKConfFetching {
		t.Fatalf("retry: mode = %v", m.mode)
	}
	m = asModel(t, first(m.Update(qwkConfsMsg{gen: m.qwkWizard.fetchGen, confs: []qwk.ConferenceInfo{
		{Number: 10, Name: "General"}, {Number: 11, Name: "Tech"},
	}})))
	if m.mode != modeQWKConfBrowser || len(m.qwkWizard.available) != 2 || !m.qwkWizard.confsFromHub {
		t.Fatalf("mode=%v available=%d", m.mode, len(m.qwkWizard.available))
	}
	m = press(t, m, "space", "enter")
	if m.qwkWizard.selectedCount() != 1 {
		t.Fatalf("selected = %d", m.qwkWizard.selectedCount())
	}

	// F refreshes, carrying the ticks; a failed refresh keeps the list.
	m = press(t, m, "enter", "F")
	if m.mode != modeQWKConfFetching {
		t.Fatalf("F: mode = %v", m.mode)
	}
	m = asModel(t, first(m.Update(qwkConfsMsg{gen: m.qwkWizard.fetchGen, err: errors.New("timeout")})))
	if m.mode != modeQWKConfBrowser || len(m.qwkWizard.available) != 2 || !m.qwkConfBrowserSel[0] {
		t.Fatalf("failed refresh: mode=%v available=%d sel=%v", m.mode, len(m.qwkWizard.available), m.qwkConfBrowserSel)
	}
	// Escape while fetching abandons it.
	m = press(t, m, "F", "x", "esc")
	if m.mode != modeQWKWizardForm || m.qwkWizard.fetching {
		t.Errorf("esc while fetching: mode=%v fetching=%v", m.mode, m.qwkWizard.fetching)
	}
}

// TestQWKWizard_FieldValidation pins QWK wizard field rules: port range,
// letters dropped from integers, Escape discarding, and Up applying.
func TestQWKWizard_FieldValidation(t *testing.T) {
	m, _ := newDiskModel(t)
	m = press(t, m, "5", "2")
	m = press(t, gotoQWKField(t, m, "Hub Port"), "enter")
	m.textInput.SetValue("")
	m = typeText(t, m, "2q1")
	if m.textInput.Value() != "21" {
		t.Errorf("letters accepted: %q", m.textInput.Value())
	}
	m = press(t, replaceText(t, m, "70000"), "enter")
	if m.mode != modeQWKWizardField || !strings.HasPrefix(m.message, "Invalid:") {
		t.Fatalf("mode=%v msg=%q", m.mode, m.message)
	}
	m = press(t, replaceText(t, m, "2121"), "up")
	if m.qwkWizard.port != 2121 || m.qwkWizardFields[m.editField].Label != "Hub Host" {
		t.Errorf("port=%d field=%q", m.qwkWizard.port, m.qwkWizardFields[m.editField].Label)
	}
	m = press(t, m, "enter")
	m = press(t, replaceText(t, m, "nope"), "esc")
	if m.qwkWizard.host == "nope" {
		t.Error("escape applied the host edit")
	}
	m = press(t, gotoQWKField(t, m, "Newscan Default"), "enter")
	if m.qwkWizard.autoJoin {
		t.Error("enter did not toggle Newscan Default")
	}
	m = press(t, gotoQWKField(t, m, "Your QWK-ID"), "space", "enter")
	if m.qwkWizardFields[m.editField].Label != "Login Name" {
		t.Errorf("enter on display row: field = %q", m.qwkWizardFields[m.editField].Label)
	}
}

// TestQWKWizard_PickerAndExit pins the entry picker when a network exists,
// the locked network row while editing, and the unsaved-wizard dialog.
func TestQWKWizard_PickerAndExit(t *testing.T) {
	m, dir := newDiskModel(t)
	m.configs.QWKNet.Networks = map[string]config.QWKNetworkConfig{
		"dovenet": {Name: "DOVE-Net", HubID: "VERT", Host: "vert.synchro.net", Password: "pw", Enabled: true},
	}
	m = press(t, m, "5", "2")
	if m.mode != modeQWKWizardPicker {
		t.Fatalf("mode = %v", m.mode)
	}
	wantScreen(t, m, "QWK Network Wizard", "dovenet")
	m = press(t, m, "end", "esc")
	if m.mode != modeCategoryMenu {
		t.Fatalf("esc: mode = %v", m.mode)
	}
	m = press(t, m, "2", "down", "enter")
	if m.mode != modeQWKWizardForm || !m.qwkWizard.editing() || m.qwkWizard.known == nil {
		t.Fatalf("edit: mode=%v editing=%v known=%v", m.mode, m.qwkWizard.editing(), m.qwkWizard.known != nil)
	}
	m = press(t, m, "enter")
	if !strings.HasPrefix(m.message, "Network is fixed while editing") {
		t.Errorf("msg = %q", m.message)
	}
	m = press(t, m, "esc")
	if m.mode != modeWizardExitConfirm || m.wizardExitSource != modeQWKWizardForm {
		t.Fatalf("mode=%v src=%v", m.mode, m.wizardExitSource)
	}
	wantScreen(t, m, "Unsaved Wizard", "QWK Network Wizard")
	m = press(t, m, "n")
	if m.mode != modeCategoryMenu {
		t.Fatalf("no: mode = %v", m.mode)
	}

	// N in the picker starts a new network; picking the configured one from
	// the known list is refused.
	m = press(t, m, "2", "n", "enter", "enter")
	if m.mode != modeQWKNetworkBrowser || !strings.Contains(m.message, "already configured") {
		t.Errorf("duplicate: mode=%v msg=%q", m.mode, m.message)
	}
	m = press(t, m, "esc")
	if m.mode != modeQWKWizardForm {
		t.Errorf("esc from browser: mode = %v", m.mode)
	}
	if _, err := os.Stat(filepath.Join(dir, "qwknet.json")); !os.IsNotExist(err) {
		t.Error("nothing was saved, but qwknet.json exists")
	}
}
