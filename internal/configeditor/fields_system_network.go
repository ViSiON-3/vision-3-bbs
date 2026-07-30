package configeditor

import (
	"strconv"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/uitext"
)

// Field definitions for the Server Setup sub-screen: listeners, hostnames and
// the settings that control how callers reach the board.

// sysFieldsNetwork returns fields for Server Setup sub-screen.
func (m *Model) sysFieldsNetwork(cfg *config.ServerConfig) []fieldDef {
	v3 := &m.configs.V3Net
	hub := &m.configs.V3Net.Hub
	binkd := &m.configs.FTN.Binkd

	return []fieldDef{
		{
			Label: "SSH Enabled", Help: "Enable SSH server", Type: ftYesNo, Col: 3, Row: 1, Width: 1,
			Get: func() string { return uitext.BoolToYN(cfg.SSHEnabled) },
			Set: func(val string) error { cfg.SSHEnabled = uitext.YNToBool(val); return nil },
		},
		{
			Label: "SSH Host", Help: "Listen address (blank=all interfaces)", Type: ftString, Col: 3, Row: 2, Width: 20,
			Get: func() string { return cfg.SSHHost },
			Set: func(val string) error { cfg.SSHHost = val; return nil },
		},
		{
			Label: "SSH Port", Help: "SSH listen port (default: 8022)", Type: ftInteger, Col: 3, Row: 3, Width: 5, Min: 1, Max: 65535,
			Get: func() string { return strconv.Itoa(cfg.SSHPort) },
			Set: func(val string) error {
				n, err := strconv.Atoi(val)
				if err != nil {
					return err
				}
				cfg.SSHPort = n
				return nil
			},
		},
		{
			Label: "Legacy SSH", Help: "Allow legacy algorithms for older clients", Type: ftYesNo, Col: 3, Row: 4, Width: 1,
			Get: func() string { return uitext.BoolToYN(cfg.LegacySSHAlgorithms) },
			Set: func(val string) error { cfg.LegacySSHAlgorithms = uitext.YNToBool(val); return nil },
		},
		{
			Label: "Telnet Enabled", Help: "Enable Telnet server", Type: ftYesNo, Col: 3, Row: 6, Width: 1,
			Get: func() string { return uitext.BoolToYN(cfg.TelnetEnabled) },
			Set: func(val string) error { cfg.TelnetEnabled = uitext.YNToBool(val); return nil },
		},
		{
			Label: "Telnet Host", Help: "Listen address (blank=all interfaces)", Type: ftString, Col: 3, Row: 7, Width: 20,
			Get: func() string { return cfg.TelnetHost },
			Set: func(val string) error { cfg.TelnetHost = val; return nil },
		},
		{
			Label: "Telnet Port", Help: "Telnet listen port (default: 8023)", Type: ftInteger, Col: 3, Row: 8, Width: 5, Min: 1, Max: 65535,
			Get: func() string { return strconv.Itoa(cfg.TelnetPort) },
			Set: func(val string) error {
				n, err := strconv.Atoi(val)
				if err != nil {
					return err
				}
				cfg.TelnetPort = n
				return nil
			},
		},
		{
			Label: "V3Net", Help: "Enable V3Net networking", Type: ftYesNo, Col: 3, Row: 10, Width: 1,
			Get: func() string { return uitext.BoolToYN(v3.Enabled) },
			Set: func(val string) error { v3.Enabled = uitext.YNToBool(val); return nil },
		},
		{
			Label: "Keystore Path", Help: "Path to Ed25519 keypair file", Type: ftString, Col: 3, Row: 11, Width: 40,
			Get: func() string { return v3.KeystorePath },
			Set: func(val string) error { v3.KeystorePath = val; return nil },
		},
		{
			Label: "Dedup DB Path", Help: "Path to deduplication SQLite database", Type: ftString, Col: 3, Row: 12, Width: 40,
			Get: func() string { return v3.DedupDBPath },
			Set: func(val string) error { v3.DedupDBPath = val; return nil },
		},
		{
			Label: "Registry URL", Help: "Central V3Net registry URL (optional)", Type: ftString, Col: 3, Row: 13, Width: 49,
			Get: func() string { return v3.RegistryURL },
			Set: func(val string) error { v3.RegistryURL = val; return nil },
		},
		{
			Label: "V3Net Hub", Help: "Run a V3Net hub server on this node", Type: ftYesNo, Col: 3, Row: 15, Width: 1,
			Get: func() string { return uitext.BoolToYN(hub.Enabled) },
			Set: func(val string) error { hub.Enabled = uitext.YNToBool(val); return nil },
		},
		{
			Label: "Hub Host", Help: "Hub listen address (blank=all interfaces)", Type: ftString, Col: 3, Row: 16, Width: 20,
			Get: func() string { return hub.Host },
			Set: func(val string) error { hub.Host = val; return nil },
		},
		{
			Label: "Hub Port", Help: "Hub listen port (default: 8765)", Type: ftInteger, Col: 3, Row: 17, Width: 5, Min: 1, Max: 65535,
			Get: func() string {
				p := hub.Port
				if p == 0 {
					p = 8765
				}
				return strconv.Itoa(p)
			},
			Set: func(val string) error {
				n, err := strconv.Atoi(val)
				if err != nil {
					return err
				}
				hub.Port = n
				return nil
			},
		},
		{
			Label: "Hub Data Dir", Help: "Hub data storage directory", Type: ftString, Col: 3, Row: 18, Width: 40,
			Get: func() string { return hub.DataDir },
			Set: func(val string) error { hub.DataDir = val; return nil },
		},
		{
			Label: "Auto Approve", Help: "Automatically approve new leaf subscriptions", Type: ftYesNo, Col: 3, Row: 19, Width: 1,
			Get: func() string { return uitext.BoolToYN(hub.AutoApprove) },
			Set: func(val string) error { hub.AutoApprove = uitext.YNToBool(val); return nil },
		},
		{
			Label: "Binkd Mailer", Help: "Run bundled binkd FTN mailer at startup", Type: ftYesNo, Col: 3, Row: 21, Width: 1,
			Get: func() string { return uitext.BoolToYN(binkd.Enabled) },
			Set: func(val string) error { binkd.Enabled = uitext.YNToBool(val); return nil },
		},
		{
			Label: "Binkd Port", Help: "binkp listen port (default: 24554)", Type: ftInteger, Col: 3, Row: 22, Width: 5, Min: 1, Max: 65535,
			Get: func() string { return strconv.Itoa(binkd.Port) },
			Set: func(val string) error {
				n, err := strconv.Atoi(val)
				if err != nil {
					return err
				}
				binkd.Port = n
				return nil
			},
		},
		{
			Label: "Binkd Binary", Help: "Path to binkd binary (default: bin/binkd)", Type: ftString, Col: 3, Row: 23, Width: 40,
			Get: func() string { return binkd.BinaryPath },
			Set: func(val string) error { binkd.BinaryPath = val; return nil },
		},
		{
			Label: "Binkd Log Lvl", Help: "binkd loglevel 1-9 (default: 4)", Type: ftInteger, Col: 3, Row: 24, Width: 2, Min: 1, Max: 9,
			Get: func() string { return strconv.Itoa(binkd.LogLevel) },
			Set: func(val string) error {
				n, err := strconv.Atoi(val)
				if err != nil {
					return err
				}
				binkd.LogLevel = n
				return nil
			},
		},
		{
			Label: "Export Secs", Help: "Outbound scan/pack interval in seconds (default: 300)", Type: ftInteger, Col: 3, Row: 25, Width: 6, Min: 30, Max: 86400,
			Get: func() string { return strconv.Itoa(binkd.ExportSecs) },
			Set: func(val string) error {
				n, err := strconv.Atoi(val)
				if err != nil {
					return err
				}
				binkd.ExportSecs = n
				return nil
			},
		},
	}
}
