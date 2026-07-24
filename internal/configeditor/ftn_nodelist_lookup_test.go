package configeditor

import (
	"errors"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/ftn"
)

var errTest = errors.New("boom")

// newLookupWizardModel builds a Model mid-wizard with a network selected.
func newLookupWizardModel(nodelistURL string) *Model {
	m := &Model{configs: &allConfigs{}}
	m.ftnWizard = &ftnWizardState{
		zone:        21,
		networkName: "fsxNet",
		hubPort:     24554,
		nodelistURL: nodelistURL,
	}
	m.ftnWizardFields = m.fieldsFTNWizard()
	m.mode = modeFTNWizardForm
	return m
}

func lookupTestNodelist(t *testing.T) *ftn.Nodelist {
	t.Helper()
	nl, err := ftn.ParseNodelist(strings.NewReader(
		"Zone,21,fsxNet,NZ,Coordinator,-Unpublished-,300,CM,INA:agency.bbs.nz,IBN:24556\n" +
			"Host,4,Net4_HQ,Berlin_DE,Host_Four,-Unpublished-,300,CM,INA:eu.example.org,IBN:24556\n" +
			",158,My_BBS,Berlin_DE,My_Sysop,-Unpublished-,300,CM,IBN\n"))
	if err != nil {
		t.Fatalf("ParseNodelist: %v", err)
	}
	return nl
}

func TestSwitchingNetworksClearsNodelistState(t *testing.T) {
	m := newLookupWizardModel("https://example.org/old.zip")
	m.ftnWizard.nodelist = lookupTestNodelist(t)
	m.ftnWizard.lookupResult = &ftn.NodeLookup{}
	m.ftnWizard.lookupErr = "stale"

	m.populateFTNWizardFromRegistry(&ftn.RegistryNetwork{
		Zone: 25, Name: "MetroNet", NodelistURL: "https://example.org/new.zip",
	})

	w := m.ftnWizard
	if w.nodelistURL != "https://example.org/new.zip" {
		t.Errorf("nodelistURL = %q", w.nodelistURL)
	}
	if w.nodelist != nil || w.lookupResult != nil || w.lookupErr != "" {
		t.Error("nodelist lookup state must be cleared when switching networks")
	}
}

func TestNodeLookupRowStates(t *testing.T) {
	cases := []struct {
		name string
		prep func(m *Model)
		want string
	}{
		{"no url", func(m *Model) { m.ftnWizard.nodelistURL = "" },
			"(no nodelist available for this network)"},
		{"no address", func(m *Model) { m.ftnWizard.ownAddress = "21:" },
			"(enter your address first)"},
		{"ready", func(m *Model) { m.ftnWizard.ownAddress = "21:4/158" },
			"(press Enter to look up your hub)"},
		{"loading", func(m *Model) {
			m.ftnWizard.ownAddress = "21:4/158"
			m.ftnWizard.lookupLoading = true
		}, "downloading nodelist..."},
		{"failed", func(m *Model) {
			m.ftnWizard.ownAddress = "21:4/158"
			m.ftnWizard.lookupErr = "boom"
		}, "lookup failed - boom"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := newLookupWizardModel("https://example.org/nl.zip")
			c.prep(m)
			got := fieldByLabel(t, m.fieldsFTNWizard(), "Node Lookup").Get()
			if got != c.want {
				t.Errorf("row text = %q, want %q", got, c.want)
			}
		})
	}
}

func TestApplyLookupAutofillsHubAndConfirms(t *testing.T) {
	m := newLookupWizardModel("https://example.org/nl.zip")
	m.ftnWizard.ownAddress = "21:4/158"
	m.ftnWizard.nodelist = lookupTestNodelist(t)

	m.applyFTNNodeLookup()

	w := m.ftnWizard
	if w.lookupErr != "" {
		t.Fatalf("lookupErr = %q", w.lookupErr)
	}
	if w.hubAddress != "21:4/0" || w.hubHostname != "eu.example.org" || w.hubPort != 24556 {
		t.Errorf("hub = %s %s:%d, want 21:4/0 eu.example.org:24556",
			w.hubAddress, w.hubHostname, w.hubPort)
	}
	got := fieldByLabel(t, m.ftnWizardFields, "Node Lookup").Get()
	if got != "My BBS, Berlin DE (My Sysop)" {
		t.Errorf("confirmation = %q", got)
	}
}

func TestApplyLookupUnlistedNodeShowsInferred(t *testing.T) {
	m := newLookupWizardModel("https://example.org/nl.zip")
	m.ftnWizard.ownAddress = "21:4/999"
	m.ftnWizard.nodelist = lookupTestNodelist(t)

	m.applyFTNNodeLookup()

	w := m.ftnWizard
	if w.hubAddress != "21:4/0" || w.hubHostname != "eu.example.org" {
		t.Errorf("hub = %s %s:%d", w.hubAddress, w.hubHostname, w.hubPort)
	}
	got := fieldByLabel(t, m.ftnWizardFields, "Node Lookup").Get()
	if got != "not listed yet - hub inferred from net 4" {
		t.Errorf("confirmation = %q", got)
	}
}

func TestApplyLookupNetMissingLeavesHubUntouched(t *testing.T) {
	m := newLookupWizardModel("https://example.org/nl.zip")
	m.ftnWizard.ownAddress = "21:9/1"
	m.ftnWizard.hubAddress = "21:1/100"
	m.ftnWizard.hubHostname = "agency.bbs.nz"
	m.ftnWizard.nodelist = lookupTestNodelist(t)

	m.applyFTNNodeLookup()

	w := m.ftnWizard
	if w.lookupErr == "" {
		t.Fatal("want lookupErr for missing net")
	}
	if w.hubAddress != "21:1/100" || w.hubHostname != "agency.bbs.nz" {
		t.Error("hub fields must not change on lookup failure")
	}
}

func TestStartLookupValidation(t *testing.T) {
	// Zone mismatch: address zone must match the selected network zone.
	m := newLookupWizardModel("https://example.org/nl.zip")
	m.ftnWizard.ownAddress = "1:2/3"
	m2, cmd := m.startFTNNodeLookup()
	if cmd != nil || m2.mode != modeFTNWizardForm || m2.message == "" {
		t.Errorf("zone mismatch: mode=%v msg=%q cmd=%v", m2.mode, m2.message, cmd)
	}

	// No URL: Enter is a no-op with a message.
	m = newLookupWizardModel("")
	m.ftnWizard.ownAddress = "21:4/158"
	m2, cmd = m.startFTNNodeLookup()
	if cmd != nil || m2.message == "" {
		t.Error("no-URL lookup should message and not fetch")
	}

	// Happy path with no cached nodelist: switches to download mode with a cmd.
	m = newLookupWizardModel("https://example.org/nl.zip")
	m.ftnWizard.ownAddress = "21:4/158"
	m2, cmd = m.startFTNNodeLookup()
	if m2.mode != modeFTNNodelistLookup || cmd == nil || !m2.ftnWizard.lookupLoading {
		t.Errorf("mode=%v loading=%v cmd=%v", m2.mode, m2.ftnWizard.lookupLoading, cmd)
	}

	// Cached nodelist: applies synchronously, no download.
	m = newLookupWizardModel("https://example.org/nl.zip")
	m.ftnWizard.ownAddress = "21:4/158"
	m.ftnWizard.nodelist = lookupTestNodelist(t)
	m2, cmd = m.startFTNNodeLookup()
	if cmd != nil || m2.mode != modeFTNWizardForm {
		t.Error("cached nodelist must apply without a fetch")
	}
	if m2.ftnWizard.hubHostname != "eu.example.org" {
		t.Errorf("hubHostname = %q", m2.ftnWizard.hubHostname)
	}
}

func TestHandleNodelistMsg(t *testing.T) {
	// Success: caches, applies, returns to form.
	m := newLookupWizardModel("https://example.org/nl.zip")
	m.ftnWizard.ownAddress = "21:4/158"
	m.ftnWizard.lookupLoading = true
	m.mode = modeFTNNodelistLookup
	res, _ := m.handleFTNNodelistMsg(ftnNodelistMsg{nodelist: lookupTestNodelist(t)})
	m2 := res.(Model)
	if m2.mode != modeFTNWizardForm || m2.ftnWizard.hubHostname != "eu.example.org" {
		t.Errorf("mode=%v hubHostname=%q", m2.mode, m2.ftnWizard.hubHostname)
	}

	// Failure: message + lookupErr, hub untouched.
	m = newLookupWizardModel("https://example.org/nl.zip")
	m.ftnWizard.lookupLoading = true
	m.mode = modeFTNNodelistLookup
	res, _ = m.handleFTNNodelistMsg(ftnNodelistMsg{err: errTest})
	m2 = res.(Model)
	if m2.ftnWizard.lookupErr == "" || m2.message == "" || m2.mode != modeFTNWizardForm {
		t.Errorf("err path: lookupErr=%q msg=%q", m2.ftnWizard.lookupErr, m2.message)
	}

	// Late result after ESC: dropped.
	m = newLookupWizardModel("https://example.org/nl.zip")
	m.mode = modeFTNWizardForm
	res, _ = m.handleFTNNodelistMsg(ftnNodelistMsg{nodelist: lookupTestNodelist(t)})
	m2 = res.(Model)
	if m2.ftnWizard.nodelist != nil {
		t.Error("late nodelist result must be dropped")
	}
}
