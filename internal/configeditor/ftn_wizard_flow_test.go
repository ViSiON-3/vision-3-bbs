package configeditor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/ftn"
)

// gotoFTNField moves the FTN wizard cursor to the field labelled label.
func gotoFTNField(t *testing.T, m Model, label string) Model {
	t.Helper()
	for i := 0; m.ftnWizardFields[m.editField].Label != label; i++ {
		if i > len(m.ftnWizardFields) {
			t.Fatalf("no FTN wizard field %q", label)
		}
		m = press(t, m, "down")
	}
	return m
}

// setFTNField opens the FTN wizard field label, replaces its text with val
// and presses Enter.
func setFTNField(t *testing.T, m Model, label, val string) Model {
	t.Helper()
	m = press(t, gotoFTNField(t, m, label), "enter")
	return press(t, replaceText(t, m, val), "enter")
}

// pickRegistryNetwork opens the network browser and selects name.
func pickRegistryNetwork(t *testing.T, m Model, name string) Model {
	t.Helper()
	m = press(t, gotoFTNField(t, m, "Network"), "enter")
	if m.mode != modeFTNNetworkBrowser {
		t.Fatalf("mode = %v, want network browser (%q)", m.mode, m.message)
	}
	m = press(t, m, "home")
	for m.ftnNetBrowserEntries[m.ftnNetBrowserCursor].Name != name {
		if m.ftnNetBrowserCursor == len(m.ftnNetBrowserEntries)-1 {
			t.Fatalf("registry has no %q", name)
		}
		m = press(t, m, "down")
	}
	return press(t, m, "enter")
}

// TestFTNWizard_KeyboardFlowSavesNetwork drives the whole FTN wizard from the
// Echomail menu: pick fsxNet from the registry, choose echo areas from a
// delivered echolist, fill in the node details, save, and check ftn.json,
// the message areas and binkd.conf on disk.
func TestFTNWizard_KeyboardFlowSavesNetwork(t *testing.T) {
	m, dir := newDiskModel(t)
	m.configs.Server.BoardName = "Test BBS"
	m.configs.Server.SSHHost = "bbs.example"
	m = press(t, m, "4", "3")
	if m.mode != modeFTNWizardForm {
		t.Fatalf("mode = %v, want FTN wizard form", m.mode)
	}
	if m.ftnWizard.originLine != "Test BBS - bbs.example" {
		t.Errorf("default origin = %q", m.ftnWizard.originLine)
	}
	wantScreen(t, m, "FTN Setup Wizard")

	m = pickRegistryNetwork(t, m, "fsxNet")
	w := m.ftnWizard
	if w.networkName != "fsxNet" || w.hubAddress != "21:1/100" || w.hubPort != 24556 || w.ownAddress != "21:" {
		t.Fatalf("registry fill: name=%q hub=%q port=%d own=%q", w.networkName, w.hubAddress, w.hubPort, w.ownAddress)
	}
	wantScreen(t, m, "fsxNet", "agency.bbs.nz")

	// Echo areas: Enter starts the download; deliver the result directly.
	m = press(t, gotoFTNField(t, m, "Echo Areas"), "enter")
	if m.mode != modeFTNAreaDownloading {
		t.Fatalf("mode = %v, want downloading (%q)", m.mode, m.message)
	}
	wantScreen(t, m, "Downloading echolist...")
	areas := []ftn.EchoArea{{Tag: "FSX_GEN", Description: "General"}, {Tag: "FSX_BBS", Description: "BBS talk"}, {Tag: "FSX_TST", Description: "Test"}}
	m = asModel(t, first(m.Update(ftnEcholistMsg{generation: m.ftnAreaBrowserGeneration, url: m.ftnWizard.echolistURL, areas: areas})))
	if m.mode != modeFTNAreaBrowser || len(m.ftnAreaBrowserAreas) != 3 {
		t.Fatalf("mode=%v areas=%d", m.mode, len(m.ftnAreaBrowserAreas))
	}
	wantScreen(t, m, "FSX_GEN", "BBS talk")
	m = press(t, m, "A", "N", "space", "down", "down", "space", "up", "end", "home")
	m = press(t, m, "enter")
	if m.mode != modeFTNWizardForm {
		t.Fatalf("mode = %v after choosing areas", m.mode)
	}
	if sel := m.ftnWizard.selectedAreas; !sel[0] || sel[1] || !sel[2] {
		t.Fatalf("selected = %v, want [true false true]", sel)
	}
	// Re-entering uses the cached list rather than downloading again.
	m = press(t, gotoFTNField(t, m, "Echo Areas"), "enter")
	if m.mode != modeFTNAreaBrowser {
		t.Fatalf("cached: mode = %v", m.mode)
	}
	m = press(t, m, "esc")
	if m.mode != modeFTNWizardForm {
		t.Fatalf("esc from cached browser: mode = %v", m.mode)
	}

	m = press(t, m, "S")
	if !strings.Contains(m.message, "ddress") {
		t.Errorf("incomplete submit msg = %q", m.message)
	}
	m = setFTNField(t, m, "Your Address", "21:4/999")
	m = setFTNField(t, m, "Areafix Pwd", "afpw")
	m = setFTNField(t, m, "Session Pwd", "sesspw")
	m = press(t, gotoFTNField(t, m, "Newscan Default"), "space")
	// Hub IP Family cycles Auto -> IPv4 -> IPv6 -> Auto; leave it on IPv4.
	m = press(t, gotoFTNField(t, m, "Hub IP Family"), "enter")
	if got := m.ftnWizard.hubIPFamily; got != config.IPFamilyIPv4 {
		t.Fatalf("after one Enter, hub family = %q, want %q", got, config.IPFamilyIPv4)
	}
	m = press(t, m, "space", "space")
	if got := m.ftnWizard.hubIPFamily; got != config.IPFamilyAuto {
		t.Fatalf("after cycling round, hub family = %q, want auto", got)
	}
	m = press(t, m, "enter")

	m = press(t, m, "pgdown")
	if _, ok := m.configs.FTN.Networks["fsxnet"]; !ok {
		t.Fatalf("network not added: %q", m.message)
	}
	if m.dirty {
		t.Errorf("wizard save left dirty: %q", m.message)
	}

	ac := reloadConfigs(t, dir)
	net, ok := ac.FTN.Networks["fsxnet"]
	if !ok {
		t.Fatalf("ftn.json networks = %v", ac.FTN.Networks)
	}
	if net.OwnAddress != "21:4/999" || len(net.Links) != 1 || net.Links[0].Address != "21:1/100" ||
		net.Links[0].SessionPassword != "sesspw" || net.Links[0].Port != 24556 ||
		net.Links[0].IPFamily != config.IPFamilyIPv4 {
		t.Errorf("saved network = %+v", net)
	}
	var echoes []string
	for _, a := range ac.MsgAreas {
		if a.AreaType == "echomail" {
			echoes = append(echoes, a.EchoTag)
			if a.AutoJoin {
				t.Errorf("area %s auto-joins despite Newscan Default N", a.EchoTag)
			}
		}
	}
	if strings.Join(echoes, ",") != "FSX_GEN,FSX_TST" {
		t.Errorf("echo areas = %v", echoes)
	}

	// Bad/Dupe Areas defaults to Y on a board without them: both are created
	// sysop-only and local (so scan never exports them), and ftn.json names them.
	if ac.FTN.BadAreaTag != "ftn_bad" || ac.FTN.DupeAreaTag != "ftn_dupe" {
		t.Errorf("bad/dupe tags = %q/%q", ac.FTN.BadAreaTag, ac.FTN.DupeAreaTag)
	}
	for _, tag := range []string{"ftn_bad", "ftn_dupe"} {
		found := false
		for _, a := range ac.MsgAreas {
			if a.Tag != tag {
				continue
			}
			found = true
			if a.AreaType != "local" || a.ACSRead != "SYSOP" || a.ACSWrite != "SYSOP" || a.Network != "" {
				t.Errorf("area %s = type %q read %q write %q network %q", tag, a.AreaType, a.ACSRead, a.ACSWrite, a.Network)
			}
		}
		if !found {
			t.Errorf("no %s message area", tag)
		}
	}
	conf, err := os.ReadFile(filepath.Join(dir, "..", "data", "ftn", "binkd.conf"))
	if err != nil {
		t.Fatalf("binkd.conf: %v", err)
	}
	if !strings.Contains(string(conf), "node 21:1/100@fsxnet -4 agency.bbs.nz:24556 sesspw") {
		t.Errorf("binkd.conf has no IPv4-pinned node line for the hub:\n%s", conf)
	}
}

// TestFTNWizard_FieldValidation pins the FTN wizard's text-field rules:
// integers drop letters and reject out-of-range ports, Escape discards, and
// Up applies and moves back.
func TestFTNWizard_FieldValidation(t *testing.T) {
	m, _ := newDiskModel(t)
	m = press(t, m, "4", "3")
	m = press(t, gotoFTNField(t, m, "Hub BinkP Port"), "enter")
	m.textInput.SetValue("")
	m = typeText(t, m, "9z9")
	if m.textInput.Value() != "99" {
		t.Errorf("letters accepted: %q", m.textInput.Value())
	}
	m = press(t, replaceText(t, m, "0"), "enter")
	if m.mode != modeFTNWizardField || m.message != "Invalid: must be 1-65535" {
		t.Fatalf("mode=%v msg=%q", m.mode, m.message)
	}
	m = press(t, replaceText(t, m, "-"), "up")
	if m.message != "Invalid: not a number" {
		t.Errorf("nan msg = %q", m.message)
	}
	m = press(t, replaceText(t, m, "2323"), "up")
	if m.ftnWizard.hubPort != 2323 || m.mode != modeFTNWizardForm || m.ftnWizardFields[m.editField].Label == "Hub BinkP Port" {
		t.Errorf("port=%d mode=%v", m.ftnWizard.hubPort, m.mode)
	}
	m = press(t, gotoFTNField(t, m, "Origin Line"), "enter")
	m = press(t, replaceText(t, m, "discard me"), "esc")
	if m.ftnWizard.originLine == "discard me" {
		t.Error("escape applied the origin edit")
	}
	m = press(t, gotoFTNField(t, m, "Newscan Default"), "enter")
	if m.ftnWizard.autoJoinAreas {
		t.Error("enter did not toggle Newscan Default")
	}
	// Space on a text field, and Enter on a read-only row, only move/ignore.
	m = press(t, gotoFTNField(t, m, "Description"), "space", "enter")
	if m.ftnWizardFields[m.editField].Label != "Coordinator" {
		t.Errorf("enter on display row: field = %q", m.ftnWizardFields[m.editField].Label)
	}
	m = press(t, m, "up", "up")
	if m.ftnWizardFields[m.editField].Label != "Network" {
		t.Errorf("up: field = %q", m.ftnWizardFields[m.editField].Label)
	}
	m = press(t, m, "up")
	if m.editField != len(m.ftnWizardFields)-1 {
		t.Errorf("up from first did not wrap: %d", m.editField)
	}
}

// TestFTNWizard_EchoAreasWithoutEcholist pins the guidance shown when the
// network has no downloadable echolist, and the error screen when a download
// fails.
func TestFTNWizard_EchoAreasWithoutEcholist(t *testing.T) {
	m, _ := newDiskModel(t)
	m = press(t, m, "4", "3")
	m = press(t, gotoFTNField(t, m, "Echo Areas"), "enter")
	if !strings.HasPrefix(m.message, "No echolist for this network") || m.mode != modeFTNWizardForm {
		t.Errorf("mode=%v msg=%q", m.mode, m.message)
	}
	m.ftnWizard.echolistURL = "fsxnet.na"
	m = press(t, m, "enter")
	if !strings.Contains(m.message, "comes from your hub") {
		t.Errorf("non-URL msg = %q", m.message)
	}

	m.ftnWizard.echolistURL = "https://example.test/x.na"
	m = press(t, m, "enter")
	if m.mode != modeFTNAreaDownloading {
		t.Fatalf("mode = %v", m.mode)
	}
	// Escape during the download returns to the form, and a late result is
	// then ignored.
	m = press(t, m, "x", "esc")
	if m.mode != modeFTNWizardForm {
		t.Fatalf("esc: mode = %v", m.mode)
	}
	m = asModel(t, first(m.Update(ftnEcholistMsg{generation: m.ftnAreaBrowserGeneration, url: m.ftnWizard.echolistURL, areas: []ftn.EchoArea{{Tag: "LATE"}}})))
	if m.mode != modeFTNWizardForm || m.ftnWizard.areasFetched {
		t.Fatalf("late result applied: mode=%v", m.mode)
	}

	m = press(t, m, "enter")
	m = asModel(t, first(m.Update(ftnEcholistMsg{generation: m.ftnAreaBrowserGeneration, url: m.ftnWizard.echolistURL, err: os.ErrDeadlineExceeded})))
	if m.mode != modeFTNAreaBrowser || !strings.HasPrefix(m.ftnAreaBrowserError, "Download failed") {
		t.Fatalf("mode=%v err=%q", m.mode, m.ftnAreaBrowserError)
	}
	wantScreen(t, m, "Download failed")
	// R retries (returning the download command), Escape leaves.
	res, cmd := m.Update(keyMsg("r"))
	if m2 := asModel(t, res); m2.mode != modeFTNAreaDownloading || cmd == nil {
		t.Errorf("retry: mode=%v cmd=%v", m2.mode, cmd != nil)
	}
	m = press(t, m, "down", "esc")
	if m.mode != modeFTNWizardForm {
		t.Errorf("esc from error: mode = %v", m.mode)
	}
}

// TestFTNWizard_NetworkBrowser pins the registry browser: C leaves for a
// custom network, Escape keeps the form as it was, and an already-configured
// network cannot be picked again.
func TestFTNWizard_NetworkBrowser(t *testing.T) {
	m, _ := newDiskModel(t)
	m.configs.FTN.Networks = map[string]config.FTNNetworkConfig{"fsxnet": {OwnAddress: "21:4/1"}}
	m = press(t, m, "4", "3", "n")
	if m.mode != modeFTNWizardForm {
		t.Fatalf("picker N: mode = %v", m.mode)
	}
	m = press(t, m, "enter")
	if m.mode != modeFTNNetworkBrowser || len(m.ftnNetBrowserEntries) == 0 {
		t.Fatalf("mode = %v", m.mode)
	}
	wantScreen(t, m, m.ftnNetBrowserEntries[0].Name)
	m = press(t, m, "end", "up", "home", "C")
	if m.mode != modeFTNWizardForm || m.ftnWizard.networkName != "" {
		t.Fatalf("C: mode=%v name=%q", m.mode, m.ftnWizard.networkName)
	}
	m = press(t, m, "enter", "esc")
	if m.mode != modeFTNWizardForm || m.ftnWizard.networkName != "" {
		t.Fatalf("esc: mode=%v name=%q", m.mode, m.ftnWizard.networkName)
	}
	m = press(t, m, "enter")
	for m.ftnNetBrowserEntries[m.ftnNetBrowserCursor].Name != "fsxNet" {
		m = press(t, m, "down")
	}
	m = press(t, m, "enter")
	if m.mode != modeFTNNetworkBrowser || !strings.Contains(m.message, "already configured") {
		t.Errorf("duplicate pick: mode=%v msg=%q", m.mode, m.message)
	}
}

// TestFTNWizard_PickerEditsExisting pins the entry picker shown when networks
// exist: it lists them, Enter on one loads it for editing with the network
// name locked, and Escape returns to the Echomail menu.
func TestFTNWizard_PickerEditsExisting(t *testing.T) {
	m, dir := newDiskModel(t)
	m.configs.FTN.Networks = map[string]config.FTNNetworkConfig{
		"fsxnet": {OwnAddress: "21:4/158", InternalTosserEnabled: true, Links: []config.FTNLinkConfig{
			{Address: "21:1/100", Hostname: "agency.bbs.nz", Port: 24556, SessionPassword: "s", AreafixPassword: "a"},
		}},
		"zznet": {OwnAddress: "9:9/9"},
	}
	m = press(t, m, "4", "3")
	if m.mode != modeFTNWizardPicker || len(m.ftnWizardPickerKeys) != 2 {
		t.Fatalf("mode=%v keys=%v", m.mode, m.ftnWizardPickerKeys)
	}
	wantScreen(t, m, "Add a new network", "fsxnet", "agency.bbs.nz:24556", "(no link configured)")
	m = press(t, m, "end")
	wantScreen(t, m, "zznet — address 9:9/9")
	m = press(t, m, "esc")
	if m.mode != modeCategoryMenu {
		t.Fatalf("esc: mode = %v", m.mode)
	}

	m = press(t, m, "3", "down", "enter")
	w := m.ftnWizard
	if m.mode != modeFTNWizardForm || !w.editing() || w.editingKey != "fsxnet" || w.zone != 21 || w.hubHostname != "agency.bbs.nz" {
		t.Fatalf("edit load: mode=%v w=%+v", m.mode, w)
	}
	if w.echolistURL == "" {
		t.Error("registry echolist not attached to a known network")
	}
	m = press(t, gotoFTNField(t, m, "Network"), "enter")
	if !strings.HasPrefix(m.message, "Network name is fixed while editing") {
		t.Errorf("msg = %q", m.message)
	}
	m = setFTNField(t, m, "Your Address", "21:4/200")
	m = press(t, m, "S")
	ac := reloadConfigs(t, dir)
	if got := ac.FTN.Networks["fsxnet"]; got.OwnAddress != "21:4/200" || !got.InternalTosserEnabled {
		t.Errorf("edited network = %+v", got)
	}
	if len(ac.FTN.Networks) != 2 {
		t.Errorf("networks = %d, want 2", len(ac.FTN.Networks))
	}
}

// TestFTNWizard_EscapeWithDataAsks pins the unsaved-wizard dialog for the FTN
// wizard: No discards back to the Echomail menu.
func TestFTNWizard_EscapeWithDataAsks(t *testing.T) {
	m, dir := newDiskModel(t)
	m = press(t, m, "4", "3")
	m = press(t, m, "esc")
	if m.mode != modeCategoryMenu {
		t.Fatalf("empty wizard esc: mode = %v", m.mode)
	}
	m = press(t, m, "3")
	m = setFTNField(t, m, "Hub Hostname", "hub.example")
	m = press(t, m, "esc")
	if m.mode != modeWizardExitConfirm || m.wizardExitSource != modeFTNWizardForm {
		t.Fatalf("mode=%v src=%v", m.mode, m.wizardExitSource)
	}
	wantScreen(t, m, "Unsaved Wizard", "FTN Setup Wizard")
	m = press(t, m, "y")
	if m.mode != modeFTNWizardForm {
		t.Fatalf("yes with incomplete form: mode = %v", m.mode)
	}
	m = press(t, m, "esc", "n")
	if m.mode != modeCategoryMenu {
		t.Fatalf("no: mode = %v", m.mode)
	}
	if _, err := os.Stat(filepath.Join(dir, "ftn.json")); !os.IsNotExist(err) {
		t.Error("discarded FTN wizard wrote ftn.json")
	}
}
