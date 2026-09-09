package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestOverridesApplyToParsedNetworks covers the mechanism itself: only
// non-empty fields are written, so an entry can correct one value and leave
// the rest of the upstream record alone.
func TestOverridesApplyToParsedNetworks(t *testing.T) {
	networks := []Network{
		{Zone: 1337, Name: "tqwNet", EcholistURL: "tqwnet.na", HubAddress: "1337:3/100"},
		{Zone: 21, Name: "fsxNet", EcholistURL: "https://example.org/fsxnet.na"},
	}
	overridesJSON = []byte(`[{"zone":1337,"echolist_url":"https://example.org/real.na"}]`)
	t.Cleanup(resetOverrides)

	networks, applied, err := applyOverrides(networks)
	if err != nil {
		t.Fatalf("applyOverrides: %v", err)
	}
	if got := networks[0].EcholistURL; got != "https://example.org/real.na" {
		t.Errorf("echolist_url = %q, want the override", got)
	}
	if got := networks[0].HubAddress; got != "1337:3/100" {
		t.Errorf("hub_address = %q, want it left alone", got)
	}
	if got := networks[1].EcholistURL; got != "https://example.org/fsxnet.na" {
		t.Errorf("an unrelated network was modified: %q", got)
	}
	if len(applied) != 1 {
		t.Errorf("applied = %v, want one entry so the build log shows what came from overrides", applied)
	}
}

// TestOverrideForMissingZoneIsAnError covers a stale entry. Upstream may
// renumber or drop a network, and an override that silently stopped applying
// would take a curated value with it — the failure mode this whole file exists
// to prevent.
func TestOverrideForMissingZoneIsAnError(t *testing.T) {
	overridesJSON = []byte(`[{"zone":9999,"echolist_url":"https://example.org/x.na"}]`)
	t.Cleanup(resetOverrides)

	if _, _, err := applyOverrides([]Network{{Zone: 21}}); err == nil {
		t.Fatal("an override matching no network should be an error, not a silent no-op")
	}
}

// TestEveryOverrideMatchesAShippedNetwork guards the real overrides.json
// against drift: every zone it patches must still exist in the registry we
// ship, so a stale entry is caught here rather than by a curated value
// quietly vanishing from a regenerated registry.
func TestEveryOverrideMatchesAShippedNetwork(t *testing.T) {
	var overrides []override
	if err := json.Unmarshal(overridesJSON, &overrides); err != nil {
		t.Fatalf("overrides.json does not parse: %v", err)
	}
	if len(overrides) == 0 {
		t.Skip("no overrides configured")
	}

	data, err := os.ReadFile(filepath.Join("..", "..", "internal", "ftn", "registry.json"))
	if err != nil {
		t.Fatalf("reading the shipped registry: %v", err)
	}
	var shipped []Network
	if err := json.Unmarshal(data, &shipped); err != nil {
		t.Fatalf("parsing the shipped registry: %v", err)
	}
	zones := make(map[int]Network, len(shipped))
	for _, n := range shipped {
		zones[n.Zone] = n
	}

	for _, o := range overrides {
		if o.Remove {
			// A removed network is intentionally absent from the shipped
			// registry, so the presence check does not apply.
			if _, present := zones[o.Zone]; present {
				t.Errorf("zone %d (%s) is marked remove but is still in registry.json", o.Zone, o.Name)
			}
			if o.Reason == "" {
				t.Errorf("remove override for zone %d (%s) has no reason recorded", o.Zone, o.Name)
			}
			continue
		}
		n, ok := zones[o.Zone]
		if !ok {
			t.Errorf("override for zone %d matches no network in registry.json", o.Zone)
			continue
		}
		if o.Reason == "" {
			t.Errorf("override for zone %d (%s) has no reason recorded", o.Zone, n.Name)
		}
		// The shipped registry is generated with these applied, so each value
		// must already be present in it. A mismatch means registry.json was
		// hand-edited and the next regeneration would undo the edit. Every
		// overrideable field is checked, not just the ones in use today, so
		// adding a field to the override struct cannot quietly escape the
		// guard.
		for _, f := range []struct {
			name     string
			override string
			shipped  string
		}{
			{"echolist_url", o.EcholistURL, n.EcholistURL},
			{"nodelist_url", o.NodelistURL, n.NodelistURL},
			{"pack_url", o.PackURL, n.PackURL},
			{"info_url", o.InfoURL, n.InfoURL},
			{"hub_address", o.HubAddress, n.HubAddress},
			{"hub_hostname", o.HubHostname, n.HubHostname},
		} {
			if f.override != "" && f.shipped != f.override {
				t.Errorf("zone %d (%s) %s: registry has %q, override says %q — registry.json looks hand-edited",
					o.Zone, n.Name, f.name, f.shipped, f.override)
			}
		}
	}
}

// TestOverrideRemovesNetwork covers dropping a defunct network: the entry is
// filtered out of the returned list, and other networks are untouched.
func TestOverrideRemovesNetwork(t *testing.T) {
	networks := []Network{
		{Zone: 77, Name: "SciNet"},
		{Zone: 21, Name: "fsxNet"},
	}
	overridesJSON = []byte(`[{"zone":77,"name":"SciNet","remove":true}]`)
	t.Cleanup(resetOverrides)

	kept, applied, err := applyOverrides(networks)
	if err != nil {
		t.Fatalf("applyOverrides: %v", err)
	}
	if len(kept) != 1 || kept[0].Zone != 21 {
		t.Errorf("removed network still present: %+v", kept)
	}
	if len(applied) != 1 {
		t.Errorf("removal should be logged, got %v", applied)
	}
}
