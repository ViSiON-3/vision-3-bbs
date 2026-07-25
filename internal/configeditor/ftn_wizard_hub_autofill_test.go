package configeditor

import (
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/ftn"
)

// TestAddressEditRestoresAutofilledHubToRegistryDefaults verifies that
// changing "Your Address" after a lookup autofilled the hub fields resets
// those fields to the registry's defaults for the (now-mismatched) uplink,
// rather than leaving the stale autofilled hub in place.
func TestAddressEditRestoresAutofilledHubToRegistryDefaults(t *testing.T) {
	m := newLookupWizardModel("https://example.org/nl.zip")
	m.ftnWizard.ownAddress = "21:4/158"
	m.ftnWizard.nodelist = lookupTestNodelist(t)
	m.ftnWizard.registryEntry = &ftn.RegistryNetwork{
		Zone: 21, Name: "fsxNet",
		HubAddress:  "21:1/100",
		HubHostname: "agency.bbs.nz",
		HubPort:     24556,
	}

	m.applyFTNNodeLookup()
	if !m.ftnWizard.hubAutofilled {
		t.Fatal("precondition: hubAutofilled must be true after a lookup")
	}
	if m.ftnWizard.hubAddress == "21:1/100" {
		t.Fatal("precondition: lookup uplink should differ from the registry default for this test to be meaningful")
	}

	addr := fieldByLabel(t, m.fieldsFTNWizard(), "Your Address")
	if err := addr.Set("21:2/101"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	w := m.ftnWizard
	if w.hubAddress != "21:1/100" || w.hubHostname != "agency.bbs.nz" || w.hubPort != 24556 {
		t.Errorf("hub = %s %s:%d, want registry defaults 21:1/100 agency.bbs.nz:24556",
			w.hubAddress, w.hubHostname, w.hubPort)
	}
	if w.hubAutofilled {
		t.Error("hubAutofilled must be false after the reset")
	}
}

// TestManualHubEditSurvivesAddressChange verifies that once a sysop
// overrides an autofilled hub field by hand, a later address edit must not
// clobber it — manual values take ownership.
func TestManualHubEditSurvivesAddressChange(t *testing.T) {
	m := newLookupWizardModel("https://example.org/nl.zip")
	m.ftnWizard.ownAddress = "21:4/158"
	m.ftnWizard.nodelist = lookupTestNodelist(t)
	m.ftnWizard.registryEntry = &ftn.RegistryNetwork{
		Zone: 21, Name: "fsxNet",
		HubAddress: "21:1/100", HubHostname: "agency.bbs.nz", HubPort: 24556,
	}

	m.applyFTNNodeLookup()

	hostField := fieldByLabel(t, m.fieldsFTNWizard(), "Hub Hostname")
	if err := hostField.Set("manual.example.org"); err != nil {
		t.Fatalf("Set hostname: %v", err)
	}
	if m.ftnWizard.hubAutofilled {
		t.Fatal("manual hostname edit must clear hubAutofilled")
	}

	addr := fieldByLabel(t, m.fieldsFTNWizard(), "Your Address")
	if err := addr.Set("21:2/101"); err != nil {
		t.Fatalf("Set address: %v", err)
	}

	if m.ftnWizard.hubHostname != "manual.example.org" {
		t.Errorf("hubHostname = %q, want manually-set value preserved", m.ftnWizard.hubHostname)
	}
}

// TestReenteringSameAddressDoesNotResetHub verifies that setting "Your
// Address" to the value it already holds is a no-op with respect to the
// hub fields — only an actual change should trigger the reset.
func TestReenteringSameAddressDoesNotResetHub(t *testing.T) {
	m := newLookupWizardModel("https://example.org/nl.zip")
	m.ftnWizard.ownAddress = "21:4/158"
	m.ftnWizard.nodelist = lookupTestNodelist(t)
	m.ftnWizard.registryEntry = &ftn.RegistryNetwork{
		Zone: 21, Name: "fsxNet",
		HubAddress: "21:1/100", HubHostname: "agency.bbs.nz", HubPort: 24556,
	}

	m.applyFTNNodeLookup()
	wantHub, wantHost, wantPort := m.ftnWizard.hubAddress, m.ftnWizard.hubHostname, m.ftnWizard.hubPort

	addr := fieldByLabel(t, m.fieldsFTNWizard(), "Your Address")
	if err := addr.Set("21:4/158"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	w := m.ftnWizard
	if w.hubAddress != wantHub || w.hubHostname != wantHost || w.hubPort != wantPort {
		t.Errorf("hub changed on re-entering same address: got %s %s:%d, want %s %s:%d",
			w.hubAddress, w.hubHostname, w.hubPort, wantHub, wantHost, wantPort)
	}
	if !w.hubAutofilled {
		t.Error("hubAutofilled must remain true when address is unchanged")
	}
}
