package configeditor

import (
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/ftn"
)

// wizardReadyToSave returns a wizard filled in far enough that only the echo
// area selection is missing — the state an operator is left in when the
// echolist download fails.
func wizardReadyToSave(t *testing.T) Model {
	t.Helper()
	m := Model{
		configPath: t.TempDir(),
		configs:    &allConfigs{},
		ftnWizard: &ftnWizardState{
			zone:            21,
			networkName:     "fsxNet",
			ownAddress:      "21:4/158",
			hubAddress:      "21:1/100",
			hubHostname:     "agency.bbs.nz",
			hubPort:         24556,
			areafixPassword: "afpw",
			sessionPassword: "sesspw",
			autoJoinAreas:   true,
			echolistURL:     "https://example.test/fsxnet.na",
		},
	}
	m.ftnWizardFields = m.fieldsFTNWizard()
	return m
}

// TestFTNWizardSavesWithoutEchoAreas covers the dead end reported when the
// echolist 401s: the wizard used to refuse to save without at least one echo
// area, so the only way out discarded everything the operator had typed.
func TestFTNWizardSavesWithoutEchoAreas(t *testing.T) {
	m := wizardReadyToSave(t)

	result, _ := m.submitFTNWizardForm()

	if _, ok := result.configs.FTN.Networks["fsxnet"]; !ok {
		t.Fatalf("network was not saved; message: %q", result.message)
	}
	if strings.Contains(result.message, "Select at least one echo area") {
		t.Fatalf("save was refused: %q", result.message)
	}

	// Netmail still gets its area, and it is the only one.
	if len(result.configs.MsgAreas) != 1 || result.configs.MsgAreas[0].AreaType != "netmail" {
		t.Fatalf("want a single netmail area, got %+v", result.configs.MsgAreas)
	}
	if !strings.Contains(result.message, "Message Areas") {
		t.Errorf("message should say where echo areas get added, got %q", result.message)
	}
}

// TestFTNWizardAreaBrowserRejectsNonURLEcholist covers the registry entries
// that record a filename ("metronet.na") rather than a web address: handing
// one to the HTTP client fails with an opaque scheme error, so the wizard
// explains where the file actually comes from and stays on the form.
func TestFTNWizardAreaBrowserRejectsNonURLEcholist(t *testing.T) {
	m := wizardReadyToSave(t)
	m.ftnWizard.echolistURL = "metronet.na"
	m.mode = modeFTNWizardForm

	result, cmd := m.enterFTNAreaBrowser()

	if cmd != nil {
		t.Error("no download should be started for a bare filename")
	}
	if result.mode != modeFTNWizardForm {
		t.Errorf("mode = %v, want to stay on the wizard form", result.mode)
	}
	if !strings.Contains(result.message, "metronet.na") || !strings.Contains(result.message, "AreaFix") {
		t.Errorf("message should name the file and how to get it, got %q", result.message)
	}
}

func TestEcholistIsDownloadable(t *testing.T) {
	for _, url := range []string{
		"https://raw.githubusercontent.com/fsxnet/infopack/master/fsxnet.na",
		"http://example.test/backbone.na",
		"HTTPS://EXAMPLE.TEST/A.NA",
	} {
		if !ftn.EcholistIsDownloadable(url) {
			t.Errorf("EcholistIsDownloadable(%q) = false, want true", url)
		}
	}
	for _, url := range []string{"", "metronet.na", "DORENET.NA", "ftp://example.test/a.na"} {
		if ftn.EcholistIsDownloadable(url) {
			t.Errorf("EcholistIsDownloadable(%q) = true, want false", url)
		}
	}
}
