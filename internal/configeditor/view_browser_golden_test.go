package configeditor

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/ftn"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

var updateGolden = flag.Bool("update", false, "regenerate golden files")

// checkGolden compares got against testdata/<name>.golden, regenerating the
// file when -update is passed. It guards the browser view refactor: output
// must stay byte-identical to the pre-refactor rendering.
func checkGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	if *updateGolden {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (run with -update to regenerate)", path, err)
	}
	if got != string(want) {
		t.Errorf("%s: output differs from golden file %s", name, path)
	}
}

func ftnAreaBrowserModel() Model {
	return Model{
		width:  80,
		height: 25,
		ftnWizard: &ftnWizardState{
			networkName: "fsxNet",
			echolistURL: "https://example.test/fsxnet.na",
		},
		ftnAreaBrowserAreas: []ftn.EchoArea{
			{Tag: "FSX_GEN", Description: "General chat"},
			{Tag: "FSX_BBS", Description: "BBS discussion with a very long description that keeps going on"},
			{Tag: "FSX_MYS", Description: "Mystic BBS"},
		},
		ftnAreaBrowserSelected: []bool{true, false, false},
		ftnAreaBrowserCursor:   1,
	}
}

func TestViewFTNAreaBrowser_Golden(t *testing.T) {
	m := ftnAreaBrowserModel()
	checkGolden(t, "ftn_area_browser_list", m.viewFTNAreaBrowser())

	m2 := ftnAreaBrowserModel()
	m2.ftnAreaBrowserLoading = true
	checkGolden(t, "ftn_area_browser_loading", m2.viewFTNAreaBrowser())

	m3 := ftnAreaBrowserModel()
	m3.ftnAreaBrowserAreas = nil
	m3.ftnAreaBrowserSelected = nil
	m3.ftnAreaBrowserCursor = 0
	m3.ftnAreaBrowserError = "Download failed: connection refused while fetching the echolist from the remote host"
	checkGolden(t, "ftn_area_browser_error", m3.viewFTNAreaBrowser())
}

func v3netAreaBrowserModel() Model {
	return Model{
		width:              80,
		height:             25,
		areaBrowserNetwork: "felnet",
		areaBrowserAreas: []areaBrowserItem{
			{Tag: "fel.general", Name: "General", Status: "ACTIVE", Subscribed: true, LocalBoard: "FelNet General"},
			{Tag: "fel.code", Name: "Coding", Status: "PENDING", Subscribed: true, LocalBoard: "FelNet Coding"},
			{Tag: "fel.retro", Name: "Retro Computing And More", LocalBoard: ""},
		},
		areaBrowserCursor: 2,
	}
}

func TestViewV3NetAreaBrowser_Golden(t *testing.T) {
	m := v3netAreaBrowserModel()
	m.message = "Subscribed to fel.general"
	checkGolden(t, "v3net_area_browser_list", m.viewV3NetAreaBrowser())

	m2 := v3netAreaBrowserModel()
	m2.areaBrowserLoading = true
	checkGolden(t, "v3net_area_browser_loading", m2.viewV3NetAreaBrowser())

	m3 := v3netAreaBrowserModel()
	m3.areaBrowserAreas = nil
	m3.areaBrowserCursor = 0
	m3.areaBrowserError = "Could not fetch areas: dial tcp: connection refused by the remote hub endpoint"
	checkGolden(t, "v3net_area_browser_error", m3.viewV3NetAreaBrowser())
}

func ftnNetworkBrowserModel() Model {
	return Model{
		width:  80,
		height: 25,
		ftnNetBrowserEntries: []ftn.RegistryNetwork{
			{
				Zone: 21, Name: "fsxNet", Description: "Fun, Simple, eXperimental Network",
				Coordinator: "Paul Hayton", CoordinatorEmail: "avon@bbs.nz",
				HubAddress: "21:1/100", HubHostname: "agency.bbs.nz", HubPort: 24556,
				InfoURL: "https://fsxnet.nz",
			},
			{Zone: 1, Name: "FidoNet", Description: "The original hobbyist network"},
		},
		ftnNetBrowserCursor: 0,
	}
}

func TestViewFTNNetworkBrowser_Golden(t *testing.T) {
	m := ftnNetworkBrowserModel()
	checkGolden(t, "ftn_network_browser_list", m.viewFTNNetworkBrowser())

	m2 := ftnNetworkBrowserModel()
	m2.ftnNetBrowserEntries = nil
	checkGolden(t, "ftn_network_browser_empty", m2.viewFTNNetworkBrowser())
}

func registryBrowserModel() Model {
	return Model{
		width:  80,
		height: 25,
		regBrowserEntries: []protocol.RegistryEntry{
			{Name: "felnet", Description: "The feline network", HubURL: "https://hub.felnet.example:8443"},
			{Name: "demonet", Description: "Demo network for testing things", HubURL: "https://demo.example"},
		},
		regBrowserCursor: 1,
	}
}

func TestViewRegistryBrowser_Golden(t *testing.T) {
	m := registryBrowserModel()
	m.message = "Already subscribed to felnet"
	checkGolden(t, "v3net_registry_browser_list", m.viewRegistryBrowser())

	m2 := registryBrowserModel()
	m2.regBrowserLoading = true
	checkGolden(t, "v3net_registry_browser_loading", m2.viewRegistryBrowser())

	m3 := registryBrowserModel()
	m3.regBrowserEntries = nil
	m3.regBrowserCursor = 0
	m3.regBrowserError = "Could not fetch registry: context deadline exceeded"
	checkGolden(t, "v3net_registry_browser_error", m3.viewRegistryBrowser())
}
