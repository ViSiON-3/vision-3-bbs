package configeditor

import (
	"fmt"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/uitext"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
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
					hubKeyCleared = l.HubPubKey != "" || l.PendingHubPubKey != "" || l.HubSigned
					l.HubPubKey, l.PendingHubPubKey = "", ""
					l.HubSigned = false
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
			Set: func(val string) error {
				l.HubSigned = uitext.YNToBool(val)
				// N may be what the file already holds while the running leaf
				// enforces a record it could not write. Raising the count
				// tells that leaf, and only that leaf, to drop the record.
				if !l.HubSigned {
					l.HubSignedClears++
				}
				return nil
			},
		},
		// V3Net Central. Each switch is its own consent and defaults to N;
		// the block is only written once a sysop touches it.
		{
			Label: "Central", Help: "Let callers open V3Net Central (the board directory) for this network; sends nothing about them", Type: ftYesNo, Col: 3, Row: 16, Width: 1,
			Get: func() string { return uitext.BoolToYN(l.CentralEnabled()) },
			Set: func(val string) error { v3netCentral(l).Enabled = uitext.YNToBool(val); return nil },
		},
		{
			Label: "Publish Listing", Help: "List this board (name, description, addresses) in the network's directory; N withdraws it", Type: ftYesNo, Col: 3, Row: 17, Width: 1,
			Get: func() string { return uitext.BoolToYN(l.CentralPublishes()) },
			Set: func(val string) error { v3netCentral(l).PublishListing = uitext.YNToBool(val); return nil },
		},
		{
			Label: "Listing Desc", Help: "One line about your board for the directory (240 chars, no pipe codes)", Type: ftString, Col: 3, Row: 18, Width: 49, MaxLen: 240,
			Get: func() string {
				if l.Central == nil {
					return ""
				}
				return l.Central.Description
			},
			Set: func(val string) error {
				if strings.ContainsFunc(val, func(r rune) bool { return r == '|' || r < 0x20 || r == 0x7f }) {
					return fmt.Errorf("pipe codes and control characters are not sent")
				}
				v3netCentral(l).Description = val
				return nil
			},
		},
		{
			Label: "Listing Addrs", Help: "Comma-separated scheme://host:port (telnet, ssh, rlogin, http, https); blank = from server setup", Type: ftString, Col: 3, Row: 19, Width: 49, MaxLen: 512,
			Get: func() string {
				if l.Central == nil {
					return ""
				}
				return strings.Join(l.Central.Addresses, ",")
			},
			Set: func(val string) error {
				var addrs []string
				for _, part := range strings.Split(val, ",") {
					a := strings.TrimSpace(part)
					if a == "" {
						continue
					}
					if err := protocol.ValidateCentralAddress(a); err != nil {
						return err
					}
					addrs = append(addrs, a)
				}
				if len(addrs) > protocol.MaxCentralAddresses {
					return fmt.Errorf("at most %d addresses", protocol.MaxCentralAddresses)
				}
				v3netCentral(l).Addresses = addrs
				return nil
			},
		},
		{
			Label: "Who's Online", Help: "Offer network Who's Online (needs Central); only callers who opt in are shown elsewhere. N withdraws", Type: ftYesNo, Col: 3, Row: 20, Width: 1,
			Get: func() string { return uitext.BoolToYN(l.Central != nil && l.Central.Presence) },
			Set: func(val string) error { v3netCentral(l).Presence = uitext.YNToBool(val); return nil },
		},
		{
			Label: "Last Callers", Help: "Offer network Last Callers (needs Central); only opted-in callers' calls, kept 24 hours. N withdraws", Type: ftYesNo, Col: 3, Row: 21, Width: 1,
			Get: func() string { return uitext.BoolToYN(l.Central != nil && l.Central.LastCallers) },
			Set: func(val string) error { v3netCentral(l).LastCallers = uitext.YNToBool(val); return nil },
		},
		{
			Label: "Callers Default", Help: "Y counts callers who have not chosen as sharing on Who's Online and Last Callers; their own choice wins", Type: ftYesNo, Col: 3, Row: 22, Width: 1,
			Get: func() string { return uitext.BoolToYN(l.Central != nil && l.Central.CallersDefault) },
			Set: func(val string) error { v3netCentral(l).CallersDefault = uitext.YNToBool(val); return nil },
		},
		{
			Label: "Lobby Chat", Help: "Offer network chat on this network; on unless set to N, and does not need Central. Callers join only after agreeing to what is shared. N ends it for callers in a room", Type: ftYesNo, Col: 3, Row: 23, Width: 1,
			Get: func() string { return uitext.BoolToYN(l.CentralChat()) },
			Set: func(val string) error { v := uitext.YNToBool(val); v3netCentral(l).Chat = &v; return nil },
		},
	}
}

// v3netCentral returns the leaf's Central block, creating an all-off one
// the first time a Central field is set.
func v3netCentral(l *config.V3NetLeafConfig) *config.V3NetCentralConfig {
	if l.Central == nil {
		l.Central = &config.V3NetCentralConfig{}
	}
	return l.Central
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
