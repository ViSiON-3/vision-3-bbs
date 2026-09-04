package ftn

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadRegistry(t *testing.T) {
	networks, err := LoadRegistry()
	if err != nil {
		t.Fatalf("LoadRegistry() error: %v", err)
	}

	if len(networks) == 0 {
		t.Fatal("LoadRegistry() returned 0 networks")
	}

	// Verify we got the expected count from init-fidonet.ini
	t.Logf("Loaded %d networks from embedded registry", len(networks))

	// Spot-check a few known entries
	found := map[int]bool{}
	for _, n := range networks {
		found[n.Zone] = true

		// Every entry must have zone, name, and description
		if n.Zone == 0 {
			t.Error("network with zone 0")
		}
		if n.Name == "" {
			t.Errorf("zone %d: empty name", n.Zone)
		}
		if n.Description == "" {
			t.Errorf("zone %d: empty description", n.Zone)
		}
	}

	// Check known zones exist
	for _, z := range []int{1, 2, 3, 4, 21} {
		if !found[z] {
			t.Errorf("expected zone %d in registry, not found", z)
		}
	}

	// Spot-check fsxNet (zone 21)
	for _, n := range networks {
		if n.Zone == 21 {
			if n.Name != "fsxNet" {
				t.Errorf("zone 21: name = %q, want fsxNet", n.Name)
			}
			if n.HubAddress != "21:1/100" {
				t.Errorf("zone 21: hub_address = %q, want 21:1/100", n.HubAddress)
			}
			if n.HubHostname != "agency.bbs.nz" {
				t.Errorf("zone 21: hub_hostname = %q, want agency.bbs.nz", n.HubHostname)
			}
			if n.HubPort != 24556 {
				t.Errorf("zone 21: hub_port = %d, want 24556", n.HubPort)
			}
			if n.AreatagPrefix != "FSX_" {
				t.Errorf("zone 21: areatag_prefix = %q, want FSX_", n.AreatagPrefix)
			}
			break
		}
	}
}

func TestRegistryNodelistURLRoundTrips(t *testing.T) {
	var nets []RegistryNetwork
	data := []byte(`[{"zone": 21, "name": "fsxNet", "nodelist_url": "https://example.org/fsxnet.zip"}]`)
	if err := json.Unmarshal(data, &nets); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if nets[0].NodelistURL != "https://example.org/fsxnet.zip" {
		t.Fatalf("NodelistURL = %q", nets[0].NodelistURL)
	}
}

func TestEmbeddedRegistryFsxNetHasNodelistURL(t *testing.T) {
	nets, err := LoadRegistry()
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	found := false
	for _, n := range nets {
		if n.Name != "fsxNet" {
			continue
		}
		found = true
		if n.NodelistURL == "" {
			t.Error("fsxNet registry entry should carry a nodelist_url")
		}
	}
	if !found {
		t.Error("fsxNet registry entry not found")
	}
}

func TestLoadOverrideRegistryTrimsURLs(t *testing.T) {
	// A sysop hand-editing ftn_networks.json can easily leave a stray space.
	// Untrimmed it passes EcholistIsDownloadable (which trims) and then fails
	// in url.Parse, and a whitespace-only value reads as "configured" instead
	// of "absent", sending the wizard down the wrong message branch.
	dir := t.TempDir()
	body := `[{"zone": 21, "name": "fsxNet",
	  "echolist_url": "  https://example.test/fsxnet.na  ",
	  "nodelist_url": "\thttps://example.test/fsxnet.zip\n"},
	 {"zone": 25, "name": "Blank", "echolist_url": "   "}]`
	if err := os.WriteFile(filepath.Join(dir, "ftn_networks.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	networks, err := LoadOverrideRegistry(dir)
	if err != nil {
		t.Fatalf("LoadOverrideRegistry() error: %v", err)
	}
	if len(networks) != 2 {
		t.Fatalf("got %d networks, want 2", len(networks))
	}

	if got := networks[0].EcholistURL; got != "https://example.test/fsxnet.na" {
		t.Errorf("EcholistURL = %q, want it trimmed", got)
	}
	if got := networks[0].NodelistURL; got != "https://example.test/fsxnet.zip" {
		t.Errorf("NodelistURL = %q, want it trimmed", got)
	}
	if _, err := http.NewRequest(http.MethodGet, networks[0].EcholistURL, nil); err != nil {
		t.Errorf("trimmed URL should build a request: %v", err)
	}
	if got := networks[1].EcholistURL; got != "" {
		t.Errorf("whitespace-only EcholistURL = %q, want empty so it reads as absent", got)
	}
}

func TestEmbeddedRegistryURLsAreTrimmed(t *testing.T) {
	networks, err := LoadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range networks {
		if n.EcholistURL != strings.TrimSpace(n.EcholistURL) {
			t.Errorf("%s zone %d: echolist_url has surrounding whitespace: %q", n.Name, n.Zone, n.EcholistURL)
		}
		if n.NodelistURL != strings.TrimSpace(n.NodelistURL) {
			t.Errorf("%s zone %d: nodelist_url has surrounding whitespace: %q", n.Name, n.Zone, n.NodelistURL)
		}
	}
}
