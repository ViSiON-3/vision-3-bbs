package configeditor

import (
	"fmt"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/uitext"
)

// fieldsV3NetHubNetwork returns fields for editing a single hub network.
func (m *Model) fieldsV3NetHubNetwork() []fieldDef {
	idx := m.recordEditIdx
	if idx < 0 || idx >= len(m.configs.V3Net.Hub.Networks) {
		return nil
	}
	n := &m.configs.V3Net.Hub.Networks[idx]

	// Count v3net message areas belonging to this network.
	areaCount := 0
	for _, a := range m.configs.MsgAreas {
		if a.AreaType == "v3net" && a.Network == n.Name {
			areaCount++
		}
	}

	return []fieldDef{
		{
			Label: "Network Name", Help: "Short lowercase network identifier (e.g. felonynet)", Type: ftString, Col: 3, Row: 1, Width: 32,
			Get: func() string { return n.Name },
			Set: func(val string) error {
				if val == "" {
					return fmt.Errorf("network name cannot be empty")
				}
				n.Name = val
				return nil
			},
		},
		{
			Label: "Description", Help: "Human-readable description of this network", Type: ftString, Col: 3, Row: 2, Width: 49,
			Get: func() string { return n.Description },
			Set: func(val string) error { n.Description = val; return nil },
		},
		{
			Label: "Areas", Help: "Press Enter to manage network message areas (delete, rename)", Type: ftDisplay, Col: 3, Row: 4, Width: 32,
			Get: func() string { return fmt.Sprintf("%d area(s) — Enter to manage", areaCount) },
		},
	}
}

// buildV3NetNetworkLookupItems returns lookup items from V3Net subscriptions.
func (m *Model) buildV3NetNetworkLookupItems() []LookupItem {
	seen := map[string]bool{}
	var items []LookupItem
	for _, l := range m.configs.V3Net.Leaves {
		if l.Network != "" && !seen[l.Network] {
			seen[l.Network] = true
			items = append(items, LookupItem{Value: l.Network, Display: l.Network})
		}
	}
	return items
}

// fieldsV3NetLeaf returns fields for editing a single leaf subscription.
func (m *Model) fieldsV3NetLeaf() []fieldDef {
	idx := m.recordEditIdx
	if idx < 0 || idx >= len(m.configs.V3Net.Leaves) {
		return nil
	}
	l := &m.configs.V3Net.Leaves[idx]
	// Set by the Hub URL field when it dropped the old hub's key, for its
	// AfterSet to tell the sysop.
	hubKeyCleared := false

	return []fieldDef{
		{
			Label: "Hub URL", Help: "URL of the V3Net hub (e.g. https://felonynet.org); changing it re-pins the hub key on first contact", Type: ftString, Col: 3, Row: 1, Width: 49,
			Get: func() string { return l.HubURL },
			Set: func(val string) error {
				hubKeyCleared = false
				if val != l.HubURL && l.HubURL != "" {
					// The old hub's key does not apply to the new one; the
					// coordinator key belongs to the network and stays.
					// Per-hub state the leaf records is cleared here too.
					hubKeyCleared = l.HubPubKey != "" || l.PendingHubPubKey != ""
					l.HubPubKey, l.PendingHubPubKey = "", ""
				}
				l.HubURL = val
				return nil
			},
			AfterSet: func(cur *Model, _ string) {
				if hubKeyCleared {
					cur.message = "Hub key cleared; it is pinned again on first contact with the new hub"
				}
			},
		},
		{
			Label: "Network", Help: "Network name to subscribe to (e.g. felonynet)", Type: ftString, Col: 3, Row: 2, Width: 32,
			Get: func() string { return l.Network },
			Set: func(val string) error {
				if val == "" {
					return fmt.Errorf("network name cannot be empty")
				}
				l.Network = val
				return nil
			},
		},
		{
			Label: "Boards", Help: "Comma-separated area tags (e.g. fel.general,fel.tech)", Type: ftString, Col: 3, Row: 3, Width: 49,
			Get: func() string { return strings.Join(l.Boards, ",") },
			Set: func(val string) error {
				if val == "" {
					l.Boards = nil
				} else {
					var boards []string
					for _, part := range strings.Split(val, ",") {
						if tag := strings.TrimSpace(part); tag != "" {
							boards = append(boards, tag)
						}
					}
					l.Boards = boards
				}
				return nil
			},
		},
		{
			Label: "Poll Interval", Help: "How often to poll for new messages (e.g. 5m, 30s)", Type: ftString, Col: 3, Row: 4, Width: 10,
			Get: func() string { return l.PollInterval },
			Set: func(val string) error { l.PollInterval = val; return nil },
		},
		{
			Label: "Origin", Help: "Origin line identifying your BBS", Type: ftString, Col: 3, Row: 5, Width: 49,
			Get: func() string { return l.Origin },
			Set: func(val string) error { l.Origin = val; return nil },
		},
		{
			Label: "Newscan Default", Help: "Flag this network's areas as newscan defaults for users", Type: ftYesNo, Col: 3, Row: 6, Width: 1,
			Get: func() string { return uitext.BoolToYN(l.AutoJoinEnabled()) },
			Set: func(val string) error {
				v := uitext.YNToBool(val)
				l.AutoJoinAreas = &v
				return nil
			},
		},
		{
			Label: "Signed Messages", Help: "Drop unsigned messages from the hub; leave N until the network's nodes sign theirs", Type: ftYesNo, Col: 3, Row: 7, Width: 1,
			Get: func() string { return uitext.BoolToYN(l.RequireSignedMessages) },
			Set: func(val string) error { l.RequireSignedMessages = uitext.YNToBool(val); return nil },
		},
		{
			Label: "Browse Areas", Help: "Press Enter to browse and subscribe to hub areas", Type: ftDisplay, Col: 3, Row: 8, Width: 49,
			Get: func() string {
				n := len(l.Boards)
				if n == 0 {
					return "(none — press Enter to browse)"
				}
				return fmt.Sprintf("%d area(s) subscribed — Enter to manage", n)
			},
		},
		v3netPinField("Coord Key", "Pinned coordinator key; NALs signed by any other key are rejected. Blank = pin on first use",
			9, &l.CoordinatorPubKey, &l.PendingCoordinatorPubKey),
		v3netPinField("Hub Key", "Pinned hub key from the network's /info. Blank = pin on first use",
			10, &l.HubPubKey, &l.PendingHubPubKey),
		v3netPendingField("Offered Coord", 11, &l.PendingCoordinatorPubKey),
		v3netPendingField("Offered Hub", 12, &l.PendingHubPubKey),
		{
			Label: "Signed Hub", Help: "Refuse unsigned replies from the hub; leave N until the hub signs", Type: ftYesNo, Col: 3, Row: 13, Width: 1,
			Get: func() string { return uitext.BoolToYN(l.RequireSignedHub) },
			Set: func(val string) error { l.RequireSignedHub = uitext.YNToBool(val); return nil },
		},
		{
			Label: "Hub Signs", Help: "Set by the leaf once the hub signs; its unsigned replies are then refused. N only if it stopped signing", Type: ftYesNo, Col: 3, Row: 14, Width: 1,
			Get: func() string { return uitext.BoolToYN(l.HubSigned) },
			Set: func(val string) error { l.HubSigned = uitext.YNToBool(val); return nil },
		},
	}
}

// v3netPinField edits one of a leaf's pinned keys. A value must be a base64
// ed25519 key or blank (pin on first use). Changing the pin is how the sysop
// accepts a key change, so it also clears the matching pending key.
func v3netPinField(label, help string, row int, pin, pending *string) fieldDef {
	return fieldDef{
		Label: label, Help: help, Type: ftString, Col: 3, Row: row, Width: 49, MaxLen: 64,
		Get: func() string { return *pin },
		Set: func(val string) error {
			val = strings.TrimSpace(val)
			if val != "" && !config.ValidV3NetPubKey(val) {
				return fmt.Errorf("not a base64 ed25519 public key")
			}
			if val != *pin {
				*pin = val
				*pending = ""
			}
			return nil
		},
	}
}

// v3netPendingField shows a key the hub offered that differs from the pin.
// The leaf records it and keeps rejecting it; copying it into the pin field
// above (after checking it with the network operator) accepts it.
func v3netPendingField(label string, row int, pending *string) fieldDef {
	return fieldDef{
		Label: label, Help: "Changed key the hub offered, rejected until you paste it into the pinned key above", Type: ftDisplay, Col: 3, Row: row, Width: 49,
		Get: func() string {
			if *pending == "" {
				return "(none)"
			}
			return *pending
		},
	}
}
