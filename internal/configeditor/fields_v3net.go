package configeditor

import (
	"fmt"
	"strconv"
)

// fieldsV3NetGlobal returns fields for editing the global V3Net + hub settings.
func (m *Model) fieldsV3NetGlobal() []fieldDef {
	v := &m.configs.V3Net
	hub := &m.configs.V3Net.Hub

	return []fieldDef{
		{
			Label: "Enabled", Help: "Enable V3Net networking", Type: ftYesNo, Col: 3, Row: 1, Width: 1,
			Get: func() string { return boolToYN(v.Enabled) },
			Set: func(val string) error { v.Enabled = ynToBool(val); return nil },
		},
		{
			Label: "Keystore Path", Help: "Path to Ed25519 keypair file", Type: ftString, Col: 3, Row: 2, Width: 40,
			Get: func() string { return v.KeystorePath },
			Set: func(val string) error { v.KeystorePath = val; return nil },
		},
		{
			Label: "Dedup DB Path", Help: "Path to deduplication SQLite database", Type: ftString, Col: 3, Row: 3, Width: 40,
			Get: func() string { return v.DedupDBPath },
			Set: func(val string) error { v.DedupDBPath = val; return nil },
		},
		{
			Label: "Registry URL", Help: "Central V3Net registry URL (optional)", Type: ftString, Col: 3, Row: 4, Width: 49,
			Get: func() string { return v.RegistryURL },
			Set: func(val string) error { v.RegistryURL = val; return nil },
		},
		{
			Label: "", Help: "", Type: ftDisplay, Col: 3, Row: 5, Width: 30,
			Get: func() string { return "── Hub Configuration ──" },
		},
		{
			Label: "Hub Enabled", Help: "Run a V3Net hub on this node", Type: ftYesNo, Col: 3, Row: 6, Width: 1,
			Get: func() string { return boolToYN(hub.Enabled) },
			Set: func(val string) error { hub.Enabled = ynToBool(val); return nil },
		},
		{
			Label: "Listen Addr", Help: "Hub listen address (e.g. :8765)", Type: ftString, Col: 3, Row: 7, Width: 20,
			Get: func() string { return hub.ListenAddr },
			Set: func(val string) error { hub.ListenAddr = val; return nil },
		},
		{
			Label: "TLS Cert", Help: "Path to TLS certificate (blank for plain HTTP)", Type: ftString, Col: 3, Row: 8, Width: 40,
			Get: func() string { return hub.TLSCert },
			Set: func(val string) error { hub.TLSCert = val; return nil },
		},
		{
			Label: "TLS Key", Help: "Path to TLS private key", Type: ftString, Col: 3, Row: 9, Width: 40,
			Get: func() string { return hub.TLSKey },
			Set: func(val string) error { hub.TLSKey = val; return nil },
		},
		{
			Label: "Data Dir", Help: "Hub data storage directory", Type: ftString, Col: 3, Row: 10, Width: 40,
			Get: func() string { return hub.DataDir },
			Set: func(val string) error { hub.DataDir = val; return nil },
		},
		{
			Label: "Auto Approve", Help: "Automatically approve new leaf subscriptions", Type: ftYesNo, Col: 3, Row: 11, Width: 1,
			Get: func() string { return boolToYN(hub.AutoApprove) },
			Set: func(val string) error { hub.AutoApprove = ynToBool(val); return nil },
		},
		{
			Label: "Hub Networks", Help: "Number of networks hosted by this hub", Type: ftDisplay, Col: 3, Row: 12, Width: 5,
			Get: func() string { return strconv.Itoa(len(hub.Networks)) },
		},
	}
}

// fieldsV3NetHubNetwork returns fields for editing a single hub network.
func (m *Model) fieldsV3NetHubNetwork() []fieldDef {
	idx := m.recordEditIdx
	if idx < 0 || idx >= len(m.configs.V3Net.Hub.Networks) {
		return nil
	}
	n := &m.configs.V3Net.Hub.Networks[idx]

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
	}
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
			Label: "Board", Help: "Local message area tag to receive messages", Type: ftString, Col: 3, Row: 3, Width: 30,
			Get: func() string { return l.Board },
			Set: func(val string) error { l.Board = val; return nil },
		},
		{
			Label: "Poll Interval", Help: "How often to poll for new messages (e.g. 5m, 30s)", Type: ftString, Col: 3, Row: 4, Width: 10,
			Get: func() string { return l.PollInterval },
			Set: func(val string) error { l.PollInterval = val; return nil },
		},
	}
}
