package configeditor

import (
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/ftn"
)

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
