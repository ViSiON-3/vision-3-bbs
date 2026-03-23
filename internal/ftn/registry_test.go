package ftn

import (
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
