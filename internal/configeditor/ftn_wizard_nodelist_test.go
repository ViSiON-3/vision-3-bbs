package configeditor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/ftn"
)

const wizardTestNodelist = ";A fsxNet Nodelist for Friday, October 2, 2026 -- Day number 275 : 12345\r\n" +
	"Zone,21,fsxNet,NZ,Coordinator,-Unpublished-,300,CM\r\n" +
	"Host,1,Net_1,NZ,Host,-Unpublished-,300,CM\r\n" +
	",100,Agency_HUB,Dunedin,Paul_Hayton,-Unpublished-,300,CM,INA:agency.bbs.nz,IBN:24556\r\n"

// TestFTNWizardSavesDownloadedNodelist covers the nodelist Node Lookup
// downloads: it used to be kept only for the lookup, so a new network had no
// compiled nodelist until one arrived by file echo or was imported by hand.
func TestFTNWizardSavesDownloadedNodelist(t *testing.T) {
	m := wizardReadyToSave(t)
	root := t.TempDir()
	m.configPath = filepath.Join(root, "configs")
	if err := os.Mkdir(m.configPath, 0o755); err != nil {
		t.Fatal(err)
	}
	nl, err := ftn.ParseNodelist(strings.NewReader(wizardTestNodelist))
	if err != nil {
		t.Fatal(err)
	}
	m.ftnWizard.nodelist = nl
	m.ftnWizard.nodelistURL = "https://example.test/fsxnet.zip"

	result, _ := m.submitFTNWizardForm()

	if !strings.Contains(result.message, "Nodelist saved") {
		t.Errorf("message should report the saved nodelist, got %q", result.message)
	}
	c, err := ftn.LoadCompiledNodelist(ftn.NodelistDir(filepath.Join(root, "data")), "fsxnet")
	if err != nil {
		t.Fatalf("compiled nodelist not written: %v", err)
	}
	if !c.HasZone(21) {
		t.Error("compiled nodelist has no zone 21")
	}
}

// TestFTNWizardSkipsOtherZoneNodelist pins that a downloaded list without the
// network's own zone is not saved as its nodelist.
func TestFTNWizardSkipsOtherZoneNodelist(t *testing.T) {
	m := wizardReadyToSave(t)
	root := t.TempDir()
	m.configPath = filepath.Join(root, "configs")
	if err := os.Mkdir(m.configPath, 0o755); err != nil {
		t.Fatal(err)
	}
	nl, err := ftn.ParseNodelist(strings.NewReader(wizardTestNodelist))
	if err != nil {
		t.Fatal(err)
	}
	m.ftnWizard.nodelist = nl
	m.ftnWizard.zone = 1

	result, _ := m.submitFTNWizardForm()

	if strings.Contains(result.message, "Nodelist") {
		t.Errorf("no nodelist should be mentioned, got %q", result.message)
	}
	if _, err := ftn.LoadCompiledNodelist(ftn.NodelistDir(filepath.Join(root, "data")), "fsxnet"); err == nil {
		t.Error("another zone's nodelist was saved")
	}
}
