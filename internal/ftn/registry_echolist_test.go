package ftn

import "testing"

// TestRegistryEcholistURLsAreFetchable guards the registry against echolist
// entries the wizard cannot use. Several networks legitimately hand their .NA
// file out over the network itself and record only a filename, and the wizard
// explains that. tqwNet was in that group despite publishing its list on the
// web, so the wizard reported the areas as unavailable for a network whose
// list was a fetch away (#275).
func TestRegistryEcholistURLsAreFetchable(t *testing.T) {
	networks, err := LoadRegistry()
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}

	// Zones whose echolist really is only available over FTN, so a bare
	// filename is the correct record. Anything else with an unfetchable
	// echolist should be looked at rather than silently accepted.
	knownFTNOnly := map[int]bool{}
	for _, n := range networks {
		if n.EcholistURL == "" || knownFTNOnly[n.Zone] {
			continue
		}
		if !EcholistIsDownloadable(n.EcholistURL) {
			t.Logf("zone %d (%s) records echolist %q, which the wizard cannot fetch — "+
				"if the list is on the web, add an override in cmd/ini2ftnreg/overrides.json",
				n.Zone, n.Name, n.EcholistURL)
		}
	}

	// tqwNet specifically: the network from #275.
	for _, n := range networks {
		if n.Zone != 1337 {
			continue
		}
		if !EcholistIsDownloadable(n.EcholistURL) {
			t.Errorf("tqwNet echolist_url = %q, which the wizard cannot fetch", n.EcholistURL)
		}
		return
	}
	t.Error("tqwNet (zone 1337) is missing from the registry")
}
