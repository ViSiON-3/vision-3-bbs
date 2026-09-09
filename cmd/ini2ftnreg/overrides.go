package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
)

// Patching gaps in the upstream network data.
//
// registry.json is generated from Synchronet's init-fidonet.ini, so anything
// missing or unusable upstream is missing here too, and editing registry.json
// by hand does not survive the next regeneration. Overrides are applied after
// parsing and before writing, which does.
//
// Keep this file small. It is for fields upstream records in a form Vision/3
// cannot use, not for disagreeing with upstream about a network's details.

//go:embed overrides.json
var embeddedOverrides []byte

// overridesJSON is a variable so tests can swap the data in.
var overridesJSON = embeddedOverrides

// override patches one network, matched by zone. Only non-empty fields are
// applied, so an entry can correct a single value and leave the rest alone.
type override struct {
	Zone   int    `json:"zone"`
	Name   string `json:"name"`   // the network this zone is, for readability; not applied
	Reason string `json:"reason"` // why upstream's value cannot be used; not applied

	// Remove drops the network from the registry entirely — for a network that
	// is defunct upstream (dead domain, no working list). Field overrides on the
	// same entry are ignored when Remove is set.
	Remove bool `json:"remove,omitempty"`

	EcholistURL string `json:"echolist_url,omitempty"`
	NodelistURL string `json:"nodelist_url,omitempty"`
	PackURL     string `json:"pack_url,omitempty"`
	InfoURL     string `json:"info_url,omitempty"`
	HubAddress  string `json:"hub_address,omitempty"`
	HubHostname string `json:"hub_hostname,omitempty"`
}

// applyOverrides patches the parsed networks and reports what it changed, so a
// build log shows which values did not come from upstream. It returns the
// (possibly filtered) network list — a `remove` override drops that network. An
// override whose zone is absent from the ini is an error rather than a silent
// no-op: it means upstream renumbered or dropped the network and the entry
// needs revisiting.
func applyOverrides(networks []Network) ([]Network, []string, error) {
	var overrides []override
	if err := json.Unmarshal(overridesJSON, &overrides); err != nil {
		return nil, nil, fmt.Errorf("parsing overrides.json: %w", err)
	}

	byZone := make(map[int]*Network, len(networks))
	for i := range networks {
		byZone[networks[i].Zone] = &networks[i]
	}

	var applied []string
	removeZones := make(map[int]bool)
	for _, o := range overrides {
		n, ok := byZone[o.Zone]
		if !ok {
			return nil, nil, fmt.Errorf("override for zone %d matches no network in the ini "+
				"— upstream may have renumbered or dropped it, so the override needs revisiting", o.Zone)
		}
		if o.Remove {
			removeZones[o.Zone] = true
			applied = append(applied, fmt.Sprintf("zone %d (%s): removed", n.Zone, n.Name))
			continue
		}
		for _, f := range []struct {
			name string
			val  string
			dst  *string
		}{
			{"echolist_url", o.EcholistURL, &n.EcholistURL},
			{"nodelist_url", o.NodelistURL, &n.NodelistURL},
			{"hub_address", o.HubAddress, &n.HubAddress},
			{"pack_url", o.PackURL, &n.PackURL},
			{"info_url", o.InfoURL, &n.InfoURL},
			{"hub_hostname", o.HubHostname, &n.HubHostname},
		} {
			if f.val == "" {
				continue
			}
			applied = append(applied, fmt.Sprintf("zone %d (%s): %s = %s", n.Zone, n.Name, f.name, f.val))
			*f.dst = f.val
		}
	}

	if len(removeZones) == 0 {
		return networks, applied, nil
	}
	kept := make([]Network, 0, len(networks))
	for _, n := range networks {
		if !removeZones[n.Zone] {
			kept = append(kept, n)
		}
	}
	return kept, applied, nil
}

// resetOverrides restores the embedded overrides after a test replaces them.
func resetOverrides() { overridesJSON = embeddedOverrides }
