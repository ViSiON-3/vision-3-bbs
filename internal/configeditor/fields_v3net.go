package configeditor

import (
	"fmt"
	"strings"
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

	return []fieldDef{
		{
			Label: "Hub URL", Help: "URL of the V3Net hub (e.g. https://hub.felonynet.org)", Type: ftString, Col: 3, Row: 1, Width: 49,
			Get: func() string { return l.HubURL },
			Set: func(val string) error { l.HubURL = val; return nil },
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
			Label: "Origin", Help: "Origin line identifying your BBS on this network (e.g. My Cool BBS - bbs.example.com)", Type: ftString, Col: 3, Row: 5, Width: 49,
			Get: func() string { return l.Origin },
			Set: func(val string) error { l.Origin = val; return nil },
		},
	}
}
