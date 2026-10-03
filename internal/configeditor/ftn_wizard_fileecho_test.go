package configeditor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/file"
	"github.com/ViSiON-3/vision-3-bbs/internal/ftn"
)

var tqwFileEchoes = []ftn.EchoArea{
	{Tag: "TQW_NODE", Description: "Weekly Nodelists"},
	{Tag: "TQW_INFO", Description: "Weekly Infopacks"},
	{Tag: "TQW_LINUXFILES", Description: "Linux files"},
}

// TestFTNWizard_FileEchoesCreateLinkedFileAreas drives the optional File
// Echoes step (#570): tqwNet's registry entry names its file echo list, the
// wizard downloads and shows it, and saving creates a file area linked to
// each ticked echo.
func TestFTNWizard_FileEchoesCreateLinkedFileAreas(t *testing.T) {
	m, dir := newDiskModel(t)
	m = press(t, m, "4", "3")
	m = pickRegistryNetwork(t, m, "tqwNet")
	if !ftn.EcholistIsDownloadable(m.ftnWizard.fileEchoListURL) {
		t.Fatalf("tqwNet file echo list URL = %q", m.ftnWizard.fileEchoListURL)
	}

	m = press(t, gotoFTNField(t, m, "File Echoes"), "enter")
	if m.mode != modeFTNAreaDownloading {
		t.Fatalf("mode = %v, want downloading (%q)", m.mode, m.message)
	}
	wantScreen(t, m, "Downloading file echo list...")

	// A late echolist result must not land in the file echo browser.
	m = asModel(t, first(m.Update(ftnEcholistMsg{url: m.ftnWizard.echolistURL, areas: []ftn.EchoArea{{Tag: "TQW_CHAT"}}})))
	if m.mode != modeFTNAreaDownloading || m.ftnWizard.areasFetched {
		t.Fatalf("echolist result taken while downloading file echoes: mode=%v", m.mode)
	}
	// Nor may a late file echo list from a network the sysop moved off.
	m = asModel(t, first(m.Update(ftnEcholistMsg{url: "https://example.test/other_file.na", fileEchoes: true,
		areas: []ftn.EchoArea{{Tag: "OTHER_FILES"}}})))
	if m.mode != modeFTNAreaDownloading || m.ftnWizard.fileEchoesFetched {
		t.Fatalf("another network's file echo list was taken: mode=%v", m.mode)
	}

	m = asModel(t, first(m.Update(ftnEcholistMsg{url: m.ftnWizard.fileEchoListURL, fileEchoes: true, areas: tqwFileEchoes})))
	if m.mode != modeFTNAreaBrowser || len(m.ftnAreaBrowserAreas) != 3 {
		t.Fatalf("mode=%v echoes=%d", m.mode, len(m.ftnAreaBrowserAreas))
	}
	wantScreen(t, m, "File Echoes", "TQW_LINUXFILES", "0 of 3 file echoes selected")
	m = press(t, m, "space", "down", "down", "space", "enter")
	if m.mode != modeFTNWizardForm {
		t.Fatalf("mode = %v after choosing file echoes", m.mode)
	}
	if sel := m.ftnWizard.selectedFileEchoes; !sel[0] || sel[1] || !sel[2] {
		t.Fatalf("selected = %v, want [true false true]", sel)
	}
	if m.ftnWizard.selectedAreaCount() != 0 {
		t.Errorf("file echo selection leaked into echo areas: %v", m.ftnWizard.selectedAreas)
	}
	wantScreen(t, m, "2 file echo(es) selected")

	m = setFTNField(t, m, "Your Address", "1337:3/999")
	m = setFTNField(t, m, "Areafix Pwd", "afpw")
	m = setFTNField(t, m, "Session Pwd", "sesspw")
	m = press(t, m, "pgdown")
	if _, ok := m.configs.FTN.Networks["tqwnet"]; !ok {
		t.Fatalf("network not added: %q", m.message)
	}
	if !strings.Contains(m.message, "2 file area(s) created") || !strings.Contains(m.message, "saved with no echo areas") {
		t.Errorf("save message = %q", m.message)
	}

	ac := reloadConfigs(t, dir)
	var got []string
	for _, a := range ac.FileAreas {
		if a.Network != "tqwnet" {
			t.Errorf("file area %s network = %q", a.Tag, a.Network)
		}
		if a.ACSUpload != "s250" {
			t.Errorf("file area %s upload ACS = %q, want sysop-only", a.Tag, a.ACSUpload)
		}
		got = append(got, a.FileEcho+"@"+a.Path)
	}
	if strings.Join(got, ",") != "TQW_NODE@tqwnet/tqw_node,TQW_LINUXFILES@tqwnet/tqw_linuxfiles" {
		t.Errorf("file areas = %v", got)
	}
}

// TestFTNWizard_EditTicksCarriedFileEchoes covers editing a network that
// already feeds file areas: the browser starts from what is carried, saving
// does not duplicate those areas, and an untick leaves its area in place.
func TestFTNWizard_EditTicksCarriedFileEchoes(t *testing.T) {
	m := wizardReadyToSave(t)
	m.configs.FileAreas = []file.FileArea{
		{ID: 4, Tag: "NODES", Path: "tqwnet/tqw_node", Network: "tqwnet", FileEcho: "TQW_NODE"},
		{ID: 5, Tag: "INFO", Path: "tqwnet/tqw_info", Network: "tqwnet", FileEcho: "TQW_INFO"},
	}
	m.configs.FTN.Networks = nil
	m.ftnWizard.networkName = "tqwNet"
	m, _ = m.submitFTNWizardForm()
	if _, ok := m.configs.FTN.Networks["tqwnet"]; !ok {
		t.Fatalf("setup save failed: %q", m.message)
	}

	// The sysop's ftn_networks.json entry wins over the built-in one.
	override := `[{"name": "tqwNet", "fileecho_list_url": "https://example.test/tqw_file.na"}]`
	if err := os.WriteFile(filepath.Join(m.configPath, "ftn_networks.json"), []byte(override), 0o644); err != nil {
		t.Fatal(err)
	}

	m, _ = m.startFTNWizardEdit("tqwnet")
	for _, f := range m.ftnWizardFields {
		if f.Label == "File Echoes" && !strings.HasPrefix(f.Get(), "2 already carried") {
			t.Errorf("File Echoes field = %q, want the carried count", f.Get())
		}
	}
	if got := m.ftnWizard.fileEchoListURL; got != "https://example.test/tqw_file.na" {
		t.Fatalf("file echo list URL = %q, want the ftn_networks.json one", got)
	}
	m, cmd := m.enterFTNFileEchoBrowser()
	if cmd == nil || m.mode != modeFTNAreaDownloading {
		t.Fatalf("no download started: mode=%v msg=%q", m.mode, m.message)
	}
	m = asModel(t, first(m.Update(ftnEcholistMsg{url: m.ftnWizard.fileEchoListURL, fileEchoes: true, areas: tqwFileEchoes})))
	if sel := m.ftnAreaBrowserSelected; !sel[0] || !sel[1] || sel[2] {
		t.Fatalf("browser selection = %v, want the carried echoes ticked", sel)
	}
	// Untick TQW_INFO, tick TQW_LINUXFILES.
	m = press(t, m, "down", "space", "down", "space", "enter")

	m, _ = m.submitFTNWizardForm()
	if !strings.Contains(m.message, "1 file area(s) created") || !strings.Contains(m.message, "1 existing file area(s) left in place") {
		t.Errorf("save message = %q", m.message)
	}
	var echoes []string
	for _, a := range m.configs.FileAreas {
		echoes = append(echoes, a.FileEcho)
	}
	if strings.Join(echoes, ",") != "TQW_NODE,TQW_INFO,TQW_LINUXFILES" {
		t.Errorf("file areas = %v", echoes)
	}
}

// TestFTNWizard_FileEchoesUnavailable covers networks with no file echo list,
// or one only obtainable from the hub: the step explains itself and stays on
// the form rather than starting a download.
func TestFTNWizard_FileEchoesUnavailable(t *testing.T) {
	for _, tc := range []struct{ url, want string }{
		{"", "helper fileecho"},
		{"tqw_file.na", "comes from your hub"},
	} {
		m := wizardReadyToSave(t)
		m.ftnWizard.fileEchoListURL = tc.url
		m.mode = modeFTNWizardForm
		result, cmd := m.enterFTNFileEchoBrowser()
		if cmd != nil || result.mode != modeFTNWizardForm {
			t.Errorf("url %q: cmd=%v mode=%v", tc.url, cmd != nil, result.mode)
		}
		if !strings.Contains(result.message, tc.want) {
			t.Errorf("url %q: message = %q, want %q", tc.url, result.message, tc.want)
		}
	}
}
