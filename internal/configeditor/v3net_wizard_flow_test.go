package configeditor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// openHubWizard navigates to Hosted Networks and presses Insert, which opens
// the hub setup wizard.
func openHubWizard(t *testing.T, m Model) Model {
	t.Helper()
	m = press(t, m, "6", "3", "i")
	if m.mode != modeWizardForm || m.wizard.flow != "hub" {
		t.Fatalf("mode/flow = %v/%q, want hub wizard", m.mode, m.wizard.flow)
	}
	return m
}

// addWizardArea adds one area through the hub wizard's area sub-form.
func addWizardArea(t *testing.T, m Model, tag, name, desc string) Model {
	t.Helper()
	m = press(t, m, "A")
	m = press(t, typeText(t, m, tag), "enter")
	m = press(t, typeText(t, m, name), "enter")
	m = press(t, typeText(t, m, desc), "enter")
	if m.wizard.areaAdding {
		t.Fatalf("area %q not added: %q", tag, m.message)
	}
	return m
}

// TestHubWizard_FormValidatesFields pins field-level validation on the hub
// wizard form: bad names and ports are refused, and saving without areas is
// blocked.
func TestHubWizard_FormValidatesFields(t *testing.T) {
	m, _ := newDiskModel(t)
	m = openHubWizard(t, m)

	m = press(t, m, "S")
	if !strings.Contains(m.message, "Network Name: cannot be empty") {
		t.Errorf("empty submit msg = %q", m.message)
	}

	m = press(t, typeText(t, press(t, m, "enter"), "Bad Name"), "enter")
	if m.mode != modeWizardField || !strings.Contains(m.message, "lowercase alphanumeric") {
		t.Fatalf("mode=%v msg=%q", m.mode, m.message)
	}
	m = press(t, replaceText(t, m, "felnet"), "enter")
	if m.wizard.netName != "felnet" || m.editField != 1 {
		t.Fatalf("name=%q field=%d", m.wizard.netName, m.editField)
	}
	m = press(t, typeText(t, press(t, m, "enter"), "Fel Net"), "enter")

	// Port: letters are ignored, out-of-range is refused.
	m = press(t, m, "enter")
	m.textInput.SetValue("")
	m = press(t, typeText(t, m, "7x0000"), "enter")
	if m.message != "Invalid: must be 1-65535" {
		t.Fatalf("port msg = %q", m.message)
	}
	m = press(t, replaceText(t, m, "9999"), "up")
	if m.wizard.port != "9999" || m.editField != 1 {
		t.Fatalf("port=%q field=%d", m.wizard.port, m.editField)
	}
	// Escape abandons an edit.
	m = press(t, replaceText(t, press(t, m, "enter"), "zzz"), "esc")
	if m.wizard.netDesc != "Fel Net" || m.mode != modeWizardForm {
		t.Fatalf("desc=%q mode=%v", m.wizard.netDesc, m.mode)
	}

	m = press(t, m, "down", "down", "enter", "down", "space")
	if !m.wizard.autoApprove || !m.wizard.autoApproveAreas {
		t.Errorf("toggles: approve=%v areas=%v", m.wizard.autoApprove, m.wizard.autoApproveAreas)
	}
	m = press(t, m, "pgdown")
	if m.message != "At least one initial area is required" {
		t.Errorf("no-area msg = %q", m.message)
	}
	wantScreen(t, m, "Hub Setup", "felnet", "9999")
}

// TestHubWizard_AreaSubForm pins the Initial Areas sub-form: tag and name
// validation, editing, deleting, and Escape cancelling an add.
func TestHubWizard_AreaSubForm(t *testing.T) {
	m, _ := newDiskModel(t)
	m = openHubWizard(t, m)
	m = press(t, m, "up") // wraps to Initial Areas
	m = press(t, m, "enter")
	if m.mode != modeV3NetWizardStep {
		t.Fatalf("mode = %v, want areas sub-form", m.mode)
	}
	wantScreen(t, m, "Initial Message Areas")

	m = press(t, typeText(t, press(t, m, "A"), "BAD"), "enter")
	if m.wizard.areaEditField != 0 || m.message == "" {
		t.Fatalf("bad tag accepted: field=%d", m.wizard.areaEditField)
	}
	m = press(t, replaceText(t, m, "fel.general"), "enter", "enter")
	if m.message != "Area name cannot be empty" {
		t.Fatalf("empty name msg = %q", m.message)
	}
	m = press(t, typeText(t, m, "General"), "tab")
	// Back up to the name and tag, then forward again.
	m = press(t, m, "shift+tab", "up", "down", "down")
	if m.wizard.areaEditField != 2 {
		t.Fatalf("field = %d, want 2", m.wizard.areaEditField)
	}
	wantScreen(t, m, "fel.general")
	m = press(t, typeText(t, m, "Talk"), "enter")
	if len(m.wizard.areas) != 1 || m.wizard.areas[0] != (wizardArea{Tag: "fel.general", Name: "General", Description: "Talk"}) {
		t.Fatalf("areas = %+v", m.wizard.areas)
	}

	m = addWizardArea(t, m, "fel.chat", "Chat", "")
	m = press(t, typeText(t, press(t, m, "A"), "fel.drop"), "esc")
	if len(m.wizard.areas) != 2 || m.wizard.areaAdding {
		t.Fatalf("escape kept a half-added area: %+v", m.wizard.areas)
	}

	// Edit the first area's name.
	m = press(t, m, "up", "up", "E")
	if m.wizard.areaEditIdx != 0 || m.textInput.Value() != "fel.general" {
		t.Fatalf("edit idx=%d val=%q", m.wizard.areaEditIdx, m.textInput.Value())
	}
	m = press(t, replaceText(t, press(t, m, "enter"), "Main"), "enter", "enter")
	if m.wizard.areas[0].Name != "Main" || len(m.wizard.areas) != 2 {
		t.Fatalf("areas after edit = %+v", m.wizard.areas)
	}

	m = press(t, m, "down", "D")
	if len(m.wizard.areas) != 1 || m.wizard.areaCursor != 0 {
		t.Fatalf("areas=%+v cursor=%d after delete", m.wizard.areas, m.wizard.areaCursor)
	}
	m = press(t, m, "esc")
	if m.mode != modeWizardForm {
		t.Fatalf("esc: mode = %v", m.mode)
	}
	m = press(t, m, "enter", "enter")
	if m.mode != modeWizardForm {
		t.Errorf("enter on area list: mode = %v", m.mode)
	}
}

// TestHubWizard_SavePersistsHubAndShowsSeed pins the full hub save: v3net.json
// gets the hub, network, seed areas and a localhost self-leaf; message areas
// are created; and a first-time key shows the seed phrase interstitial.
func TestHubWizard_SavePersistsHubAndShowsSeed(t *testing.T) {
	m, dir := newDiskModel(t)
	m = openHubWizard(t, m)
	m.wizard.netName = "felnet"
	m.wizard.netDesc = "Fel Net"
	m.wizard.port = "9999"
	m = press(t, m, "up", "enter")
	m = addWizardArea(t, m, "fel.general", "General", "Talk")
	m = press(t, m, "enter", "S")
	if m.message != "Hub saved. Start BBS to initialize." || m.dirty {
		t.Fatalf("msg=%q dirty=%v", m.message, m.dirty)
	}
	if !m.showSeedInterstitial || m.seedInterstitialNodeID == "" {
		t.Fatal("seed interstitial not shown for a new key")
	}
	wantScreen(t, m, "V3Net Node Identity Created", m.seedInterstitialNodeID)
	if _, err := os.Stat(m.configs.V3Net.KeystorePath); err != nil {
		t.Errorf("key file not created: %v", err)
	}

	ac := reloadConfigs(t, dir)
	hub := ac.V3Net.Hub
	if !ac.V3Net.Enabled || !hub.Enabled || hub.Port != 9999 || len(hub.Networks) != 1 || hub.Networks[0].Name != "felnet" {
		t.Errorf("saved hub = %+v", hub)
	}
	if len(hub.InitialAreas) != 1 || hub.InitialAreas[0].Tag != "fel.general" {
		t.Errorf("initial areas = %+v", hub.InitialAreas)
	}
	if len(ac.V3Net.Leaves) != 1 || ac.V3Net.Leaves[0].HubURL != "http://localhost:9999" ||
		len(ac.V3Net.Leaves[0].Boards) != 1 {
		t.Errorf("self-leaf = %+v", ac.V3Net.Leaves)
	}
	var found bool
	for _, a := range ac.MsgAreas {
		if a.Tag == "fel.general" && a.AreaType == "v3net" && a.Network == "felnet" && a.BasePath == "msgbases/fel.general" {
			found = true
		}
	}
	if !found {
		t.Errorf("v3net message area not saved: %+v", ac.MsgAreas)
	}

	m = press(t, m, "x")
	if m.mode != modeRecordList || m.showSeedInterstitial || m.seedInterstitialPhrase != "" {
		t.Errorf("dismiss: mode=%v shown=%v", m.mode, m.showSeedInterstitial)
	}
}

// TestSeedInterstitial_ExportWritesRecoveryFile pins that E on the seed
// interstitial writes v3net-recovery.txt to the working directory and does
// not overwrite an existing one.
func TestSeedInterstitial_ExportWritesRecoveryFile(t *testing.T) {
	m, _ := newDiskModel(t)
	ks := makeKey(t, m.configs.V3Net.KeystorePath)
	work := t.TempDir()
	t.Chdir(work)

	m.mode = modeWizardForm
	m.wizardFields = m.fieldsHubWizard()
	m.showSeedInterstitial = true
	m = press(t, m, "E")
	if m.mode != modeRecordList || m.showSeedInterstitial {
		t.Fatalf("mode=%v shown=%v", m.mode, m.showSeedInterstitial)
	}
	body, err := os.ReadFile(filepath.Join(work, "v3net-recovery.txt"))
	if err != nil {
		t.Fatalf("recovery file: %v", err)
	}
	if !strings.Contains(string(body), ks.NodeID()) {
		t.Error("recovery file missing node ID")
	}

	m.mode = modeWizardForm
	m.showSeedInterstitial = true
	m = press(t, m, "e")
	if m.mode != modeRecordList {
		t.Fatalf("mode = %v", m.mode)
	}
	// The message is cleared by the screen change; the file must be untouched.
	again, _ := os.ReadFile(filepath.Join(work, "v3net-recovery.txt"))
	if string(again) != string(body) {
		t.Error("existing recovery file was overwritten")
	}
}

// TestHubWizard_EscapeWithDataAsks pins the unsaved-wizard dialog: Escape
// returns to the form, No discards to the list, Yes submits.
func TestHubWizard_EscapeWithDataAsks(t *testing.T) {
	m, dir := newDiskModel(t)
	m = openHubWizard(t, m)
	m.wizard.netName = "felnet"
	m = press(t, m, "esc")
	if m.mode != modeWizardExitConfirm {
		t.Fatalf("mode = %v", m.mode)
	}
	wantScreen(t, m, "Unsaved Wizard")
	m = press(t, m, "esc")
	if m.mode != modeWizardForm {
		t.Fatalf("esc: mode = %v", m.mode)
	}
	// Yes submits; with no areas that is refused and the form stays open.
	m = press(t, m, "esc", "y")
	if m.mode != modeWizardForm {
		t.Fatalf("yes: mode = %v", m.mode)
	}
	m = press(t, m, "esc", "right", "enter")
	if m.mode != modeRecordList {
		t.Fatalf("no: mode = %v", m.mode)
	}
	if _, err := os.Stat(filepath.Join(dir, "v3net.json")); !os.IsNotExist(err) {
		t.Error("discarded wizard wrote v3net.json")
	}

	// A wizard with nothing entered leaves without asking.
	m = press(t, m, "i", "esc")
	if m.mode != modeRecordList {
		t.Errorf("empty wizard esc: mode = %v", m.mode)
	}
}

// TestLeafWizard_FetchNetworksResult pins how the hub's network list is
// applied: one name fills the field, several open a picker whose choice fills
// it, and a failure asks for manual entry.
func TestLeafWizard_FetchNetworksResult(t *testing.T) {
	m, _ := newDiskModel(t)
	m = press(t, m, "6", "2", "i")
	if m.mode != modeWizardForm || m.wizard.flow != "leaf" {
		t.Fatalf("mode/flow = %v/%q", m.mode, m.wizard.flow)
	}
	// Entering a Hub URL returns the fetch command (not run here).
	m = press(t, m, "down", "enter")
	m = typeText(t, m, "https://hub.example")
	res, cmd := m.Update(keyMsg("enter"))
	m = asModel(t, res)
	if cmd == nil || m.wizard.hubURL != "https://hub.example" {
		t.Fatalf("hub URL: cmd=%v url=%q", cmd != nil, m.wizard.hubURL)
	}

	m = asModel(t, first(m.Update(fetchNetworksMsg{names: []string{"felnet"}})))
	if m.wizard.networkName != "felnet" {
		t.Errorf("single network: name = %q", m.wizard.networkName)
	}
	m = asModel(t, first(m.Update(fetchNetworksMsg{})))
	if m.wizard.fetchError == "" {
		t.Error("empty result did not set fetch error")
	}
	m = asModel(t, first(m.Update(fetchNetworksMsg{names: []string{"a", "b"}})))
	if m.mode != modeLookupPicker || m.pickerReturnMode != modeWizardForm {
		t.Fatalf("multi: mode = %v", m.mode)
	}
	m = press(t, m, "down", "enter")
	if m.mode != modeWizardForm || m.wizard.networkName != "b" {
		t.Errorf("picker: mode=%v name=%q", m.mode, m.wizard.networkName)
	}
}

// TestLeafWizard_AreasRequireHubAndNetwork pins the guards on the leaf
// wizard's Areas row and the requirement to subscribe to something.
func TestLeafWizard_AreasRequireHubAndNetwork(t *testing.T) {
	m, _ := newDiskModel(t)
	m = press(t, m, "6", "2", "i")
	m = press(t, m, "down", "down", "down", "enter")
	if m.message != "Enter a Hub URL first" {
		t.Errorf("msg = %q", m.message)
	}
	m.wizard.hubURL = "https://hub.example"
	m = press(t, m, "enter")
	if m.message != "Enter a Network name first" {
		t.Errorf("msg = %q", m.message)
	}
	m.wizard.networkName = "felnet"
	m = press(t, m, "S")
	if m.message != "Select at least one area to subscribe to" {
		t.Errorf("msg = %q", m.message)
	}
	m.wizard.pollInterval = "soon"
	m = press(t, m, "S")
	if !strings.HasPrefix(m.message, "Poll Interval:") {
		t.Errorf("msg = %q", m.message)
	}
}

// TestLeafWizard_SavePersistsSubscription pins that a completed leaf wizard
// writes the subscription, creates a local area for each subscribed board,
// and refuses a duplicate subscription.
func TestLeafWizard_SavePersistsSubscription(t *testing.T) {
	m, dir := newDiskModel(t)
	makeKey(t, m.configs.V3Net.KeystorePath) // existing key: no interstitial
	m = press(t, m, "6", "2", "i")
	m.wizard.hubURL = "https://hub.example"
	m.wizard.networkName = "felnet"
	m.wizard.selectedAreas = []areaBrowserItem{
		{Tag: "fel.general", Name: "General", Subscribed: true, LocalBoard: "FelGeneral"},
		{Tag: "fel.skip", Name: "Skip"},
	}
	m = press(t, m, "S")
	if m.mode != modeRecordList || m.dirty || m.showSeedInterstitial {
		t.Fatalf("mode=%v dirty=%v seed=%v msg=%q", m.mode, m.dirty, m.showSeedInterstitial, m.message)
	}
	ac := reloadConfigs(t, dir)
	if len(ac.V3Net.Leaves) != 1 {
		t.Fatalf("leaves = %+v", ac.V3Net.Leaves)
	}
	l := ac.V3Net.Leaves[0]
	if l.HubURL != "https://hub.example" || l.Network != "felnet" || len(l.Boards) != 1 || l.Boards[0] != "fel.general" || l.PollInterval != "5m" {
		t.Errorf("leaf = %+v", l)
	}
	// Only subscribed boards get a local area. Checked on the model: the
	// wizard's second save is what writes these, and it is not asserted here.
	var tags []string
	for _, a := range m.configs.MsgAreas {
		tags = append(tags, a.EchoTag)
	}
	if strings.Join(tags, ",") != "fel.general" {
		t.Errorf("area echo tags = %v, want only fel.general", tags)
	}

	m = press(t, m, "i")
	m.wizard.hubURL = "https://hub.example"
	m.wizard.networkName = "felnet"
	m.wizard.selectedAreas = []areaBrowserItem{{Tag: "fel.general", Subscribed: true}}
	m = press(t, m, "S")
	if m.message != "Already subscribed to this network on this hub" {
		t.Errorf("duplicate msg = %q", m.message)
	}
}
