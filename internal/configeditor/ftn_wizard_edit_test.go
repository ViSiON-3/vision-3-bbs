package configeditor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/ftn"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
)

// configuredModel builds a Model with one FTN network already set up, the
// situation the wizard used to ignore entirely (issue #176).
func configuredModel() Model {
	return Model{
		configs: &allConfigs{
			FTN: config.FTNConfig{
				Networks: map[string]config.FTNNetworkConfig{
					"fsxnet": {
						InternalTosserEnabled: true,
						OwnAddress:            "21:4/158",
						PollSeconds:           900,
						Tearline:              "Custom Tearline",
						Links: []config.FTNLinkConfig{{
							Address:         "21:1/100",
							Hostname:        "agency.bbs.nz",
							Port:            24554,
							AreafixPassword: "afpw",
							SessionPassword: "sesspw",
							PacketPassword:  "pktpw",
							Name:            "fsxnet Hub",
							Flavour:         "Crash",
						}},
					},
				},
			},
			MsgAreas: []message.MessageArea{
				{ID: 1, Tag: "fsxnet_netmail", Network: "fsxnet", AreaType: "netmail"},
				{ID: 2, Tag: "fsxnet_fsx_gen", Network: "fsxnet", AreaType: "echomail", EchoTag: "FSX_GEN", AutoJoin: false},
				{ID: 3, Tag: "fsxnet_fsx_bbs", Network: "fsxnet", AreaType: "echomail", EchoTag: "FSX_BBS", AutoJoin: false},
			},
		},
	}
}

// TestEnterFTNWizardOffersPickerWhenConfigured is the reported bug: entering
// the wizard with a network already set up must surface it rather than opening
// a blank form that hides what exists.
func TestEnterFTNWizardOffersPickerWhenConfigured(t *testing.T) {
	m, _ := configuredModel().enterFTNWizard()

	if m.mode != modeFTNWizardPicker {
		t.Fatalf("mode = %v, want modeFTNWizardPicker", m.mode)
	}
	if len(m.ftnWizardPickerKeys) != 1 || m.ftnWizardPickerKeys[0] != "fsxnet" {
		t.Errorf("picker keys = %v, want [fsxnet]", m.ftnWizardPickerKeys)
	}
}

// TestEnterFTNWizardSkipsPickerWhenEmpty keeps first-run friction unchanged.
func TestEnterFTNWizardSkipsPickerWhenEmpty(t *testing.T) {
	m, _ := Model{configs: &allConfigs{}}.enterFTNWizard()

	if m.mode != modeFTNWizardForm {
		t.Fatalf("mode = %v, want modeFTNWizardForm on a fresh system", m.mode)
	}
	if m.ftnWizard.editing() {
		t.Error("a fresh system should start in add mode")
	}
}

// TestStartFTNWizardEditLoadsExistingConfig covers the reporter's actual ask:
// the wizard should show what was originally added.
func TestStartFTNWizardEditLoadsExistingConfig(t *testing.T) {
	m, _ := configuredModel().startFTNWizardEdit("fsxnet")

	w := m.ftnWizard
	if !w.editing() || w.editingKey != "fsxnet" {
		t.Fatalf("editingKey = %q, want fsxnet", w.editingKey)
	}

	for _, tc := range []struct{ name, got, want string }{
		{"ownAddress", w.ownAddress, "21:4/158"},
		{"hubAddress", w.hubAddress, "21:1/100"},
		{"hubHostname", w.hubHostname, "agency.bbs.nz"},
		{"areafixPassword", w.areafixPassword, "afpw"},
		{"sessionPassword", w.sessionPassword, "sesspw"},
		{"packetPassword", w.packetPassword, "pktpw"},
	} {
		if tc.got != tc.want {
			t.Errorf("%s = %q, want %q", tc.name, tc.got, tc.want)
		}
	}
	if w.hubPort != 24554 {
		t.Errorf("hubPort = %d, want 24554", w.hubPort)
	}
	if w.zone != 21 {
		t.Errorf("zone = %d, want 21 (parsed from own address)", w.zone)
	}
	if !w.subscribedTags["FSX_GEN"] || !w.subscribedTags["FSX_BBS"] {
		t.Errorf("subscribedTags = %v, want FSX_GEN and FSX_BBS", w.subscribedTags)
	}
	if w.autoJoinAreas {
		t.Error("autoJoinAreas should reflect the configured areas (false), not the add-mode default")
	}
}

// TestConfirmFTNWizardEditPreservesUnaskedSettings is the destructive case:
// the wizard must not reset settings owned by the Echomail Networks editor.
func TestConfirmFTNWizardEditPreservesUnaskedSettings(t *testing.T) {
	m, _ := configuredModel().startFTNWizardEdit("fsxnet")
	m.ftnWizard.hubHostname = "new.host.example"
	m.configPath = t.TempDir()

	m, _ = m.confirmFTNWizard()

	net := m.configs.FTN.Networks["fsxnet"]
	if net.PollSeconds != 900 {
		t.Errorf("PollSeconds = %d, want 900 preserved", net.PollSeconds)
	}
	if net.Tearline != "Custom Tearline" {
		t.Errorf("Tearline = %q, want preserved", net.Tearline)
	}
	if !net.InternalTosserEnabled {
		t.Error("InternalTosserEnabled should be preserved")
	}
	if len(net.Links) != 1 {
		t.Fatalf("expected 1 link, got %d", len(net.Links))
	}
	if net.Links[0].Hostname != "new.host.example" {
		t.Errorf("Hostname = %q, want the edit applied", net.Links[0].Hostname)
	}
}

// TestReplaceHubLinkKeepsDownstreamLinks guards the links the wizard does not
// manage: nodes this system feeds must survive a wizard save.
func TestReplaceHubLinkKeepsDownstreamLinks(t *testing.T) {
	links := []config.FTNLinkConfig{
		{Address: "21:1/100", Hostname: "old.host", Flavour: "Crash", Name: "fsxnet Hub"},
		{Address: "21:4/900", Hostname: "downstream.example", Flavour: "Hold", Name: "A Point"},
	}
	hub := config.FTNLinkConfig{Address: "21:1/100", Hostname: "new.host", Flavour: "Direct", Name: "overwritten"}

	got := replaceHubLink(links, hub)

	if len(got) != 2 {
		t.Fatalf("expected 2 links, got %d", len(got))
	}
	if got[0].Hostname != "new.host" {
		t.Errorf("hub hostname = %q, want new.host", got[0].Hostname)
	}
	if got[0].Flavour != "Crash" || got[0].Name != "fsxnet Hub" {
		t.Errorf("hub flavour/name should be preserved, got %q/%q", got[0].Flavour, got[0].Name)
	}
	if got[1].Address != "21:4/900" || got[1].Hostname != "downstream.example" {
		t.Errorf("downstream link was damaged: %+v", got[1])
	}
}

// TestReplaceHubLinkHandlesChangedHubAddress covers moving to a different hub.
func TestReplaceHubLinkHandlesChangedHubAddress(t *testing.T) {
	links := []config.FTNLinkConfig{{Address: "21:1/100", Hostname: "old.host", Flavour: "Crash"}}
	hub := config.FTNLinkConfig{Address: "21:1/200", Hostname: "new.host"}

	got := replaceHubLink(links, hub)

	if len(got) != 1 {
		t.Fatalf("expected the hub replaced in place, got %d links", len(got))
	}
	if got[0].Address != "21:1/200" {
		t.Errorf("address = %q, want the new hub", got[0].Address)
	}
}

// TestUnsubscribedTagCountOnlyAfterReview makes sure an untouched wizard run
// never reports areas as dropped just because no echolist was downloaded.
func TestUnsubscribedTagCountOnlyAfterReview(t *testing.T) {
	w := &ftnWizardState{subscribedTags: map[string]bool{"FSX_GEN": true, "FSX_BBS": true}}
	if n := w.unsubscribedTagCount(); n != 0 {
		t.Errorf("without a downloaded echolist, dropped = %d, want 0", n)
	}

	w.areasFetched = true
	w.availableAreas = []ftn.EchoArea{{Tag: "FSX_GEN"}, {Tag: "FSX_BBS"}}
	w.selectedAreas = []bool{true, false}
	if n := w.unsubscribedTagCount(); n != 1 {
		t.Errorf("dropped = %d, want 1 (FSX_BBS unticked)", n)
	}
}

// TestConfirmFTNWizardAddRefusesDuplicate keeps add mode from merging into an
// existing network, and points at the new way to change one.
func TestConfirmFTNWizardAddRefusesDuplicate(t *testing.T) {
	m := configuredModel()
	m.ftnWizard = &ftnWizardState{networkName: "fsxnet"}
	m.configPath = t.TempDir()

	m, _ = m.confirmFTNWizard()

	if !strings.Contains(m.message, "already exists") {
		t.Errorf("message = %q, want a duplicate refusal", m.message)
	}
	if !strings.Contains(m.message, "edit") {
		t.Errorf("message = %q, should point the sysop at editing", m.message)
	}
}

// TestConfirmFTNWizardEditUpdatesAreaOriginAddr covers changing your own FTN
// address on an existing network. Areas carry the origin they were created
// with and createFTNMsgAreaIfNeeded leaves existing ones alone, so without an
// explicit update every area would keep stamping outbound mail with the old
// address while ftn.json showed the new one.
func TestConfirmFTNWizardEditUpdatesAreaOriginAddr(t *testing.T) {
	m, _ := configuredModel().startFTNWizardEdit("fsxnet")
	for i := range m.configs.MsgAreas {
		m.configs.MsgAreas[i].OriginAddr = "21:4/158"
	}
	m.ftnWizard.ownAddress = "21:4/159"
	m.configPath = t.TempDir()

	m, _ = m.confirmFTNWizard()

	for _, area := range m.configs.MsgAreas {
		if !strings.EqualFold(area.Network, "fsxnet") {
			continue
		}
		if area.OriginAddr != "21:4/159" {
			t.Errorf("area %s OriginAddr = %q, want the edited address", area.Tag, area.OriginAddr)
		}
	}
}

// TestConfirmFTNWizardKeepsBinkdWarning makes sure a binkd.conf failure is not
// buried by the success message. Telling the operator to restart when the
// mailer never got the new details is worse than saying nothing.
func TestConfirmFTNWizardKeepsBinkdWarning(t *testing.T) {
	m, _ := configuredModel().startFTNWizardEdit("fsxnet")

	// configPath must be writable so the config save succeeds and we reach the
	// success message -- that is the path the warning used to be lost on.
	// binkd.conf resolves to configPath/../data/ftn/binkd.conf, so making
	// "data" a regular file fails only the binkd write.
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "configs"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "data"), []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	m.configPath = filepath.Join(dir, "configs")

	m, _ = m.confirmFTNWizard()

	if strings.HasPrefix(m.message, "SAVE ERROR") {
		t.Fatalf("config save failed, so this did not exercise the success path: %q", m.message)
	}
	if !strings.Contains(m.message, "updated") {
		t.Errorf("message = %q, want the success result retained", m.message)
	}
	if !strings.Contains(m.message, "binkd.conf update failed") {
		t.Errorf("message = %q, want the binkd warning kept alongside the success result", m.message)
	}
}

// TestUnsubscribedTagCountIgnoresAreasAbsentFromEcholist: an area the echolist
// no longer offers cannot be unticked, so it was never the operator's decision
// and must not be reported as one.
func TestUnsubscribedTagCountIgnoresAreasAbsentFromEcholist(t *testing.T) {
	w := &ftnWizardState{
		subscribedTags: map[string]bool{"FSX_GEN": true, "FSX_RETIRED": true},
		areasFetched:   true,
		availableAreas: []ftn.EchoArea{{Tag: "FSX_GEN"}},
		selectedAreas:  []bool{true},
	}
	if n := w.unsubscribedTagCount(); n != 0 {
		t.Errorf("dropped = %d, want 0 — FSX_RETIRED is not in the echolist so it was never unticked", n)
	}

	w.selectedAreas = []bool{false}
	if n := w.unsubscribedTagCount(); n != 1 {
		t.Errorf("dropped = %d, want 1 — FSX_GEN was offered and unticked", n)
	}
}
