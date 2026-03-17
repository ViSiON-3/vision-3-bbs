package configeditor

import (
	"strconv"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

// buildTimezoneLookupItems returns a list of common IANA timezones.
func buildTimezoneLookupItems() []LookupItem {
	timezones := []string{
		// Americas
		"America/New_York",
		"America/Chicago",
		"America/Denver",
		"America/Phoenix",
		"America/Los_Angeles",
		"America/Anchorage",
		"America/Toronto",
		"America/Vancouver",
		"America/Mexico_City",
		"America/Sao_Paulo",
		"America/Argentina/Buenos_Aires",
		// Europe
		"Europe/London",
		"Europe/Paris",
		"Europe/Berlin",
		"Europe/Rome",
		"Europe/Madrid",
		"Europe/Amsterdam",
		"Europe/Brussels",
		"Europe/Vienna",
		"Europe/Warsaw",
		"Europe/Moscow",
		"Europe/Istanbul",
		"Europe/Athens",
		// Asia
		"Asia/Dubai",
		"Asia/Karachi",
		"Asia/Kolkata",
		"Asia/Bangkok",
		"Asia/Shanghai",
		"Asia/Hong_Kong",
		"Asia/Tokyo",
		"Asia/Seoul",
		"Asia/Singapore",
		"Asia/Manila",
		// Pacific
		"Pacific/Auckland",
		"Pacific/Fiji",
		"Pacific/Honolulu",
		// Australia
		"Australia/Sydney",
		"Australia/Melbourne",
		"Australia/Brisbane",
		"Australia/Perth",
		"Australia/Adelaide",
		// Africa
		"Africa/Cairo",
		"Africa/Johannesburg",
		"Africa/Lagos",
		"Africa/Nairobi",
		// UTC
		"UTC",
	}

	items := make([]LookupItem, len(timezones))
	for i, tz := range timezones {
		items[i] = LookupItem{
			Value:   tz,
			Display: tz,
		}
	}
	return items
}

// buildSysFields returns field definitions for the given system config sub-screen.
func (m *Model) buildSysFields(screen int) []fieldDef {
	cfg := &m.configs.Server
	switch screen {
	case 0:
		return sysFieldsRegistration(cfg)
	case 1:
		return m.sysFieldsNetwork(cfg)
	case 2:
		return sysFieldsLimits(cfg)
	case 3:
		return sysFieldsLevels(cfg)
	case 4:
		return sysFieldsDefaults(cfg)
	case 5:
		return sysFieldsIPLists(cfg)
	case 6:
		return sysFieldsNUV(cfg)
	case 7:
		return sysFieldsDOS(cfg)
	}
	return nil
}

// sysFieldsRegistration returns fields for BBS Registration sub-screen.
func sysFieldsRegistration(cfg *config.ServerConfig) []fieldDef {
	return []fieldDef{
		{
			Label: "Board Name", Help: "Your BBS name shown to users", Type: ftString, Col: 3, Row: 1, Width: 40,
			Get: func() string { return cfg.BoardName },
			Set: func(val string) error { cfg.BoardName = val; return nil },
		},
		{
			Label: "SysOp Name", Help: "System operator name", Type: ftString, Col: 3, Row: 2, Width: 30,
			Get: func() string { return cfg.SysOpName },
			Set: func(val string) error { cfg.SysOpName = val; return nil },
		},
		{
			Label: "Timezone", Help: "IANA timezone", Type: ftLookup, Col: 3, Row: 3, Width: 30,
			Get: func() string { return cfg.Timezone },
			Set: func(val string) error { cfg.Timezone = val; return nil },
			LookupItems: func() []LookupItem {
				items := buildTimezoneLookupItems()

				// If current timezone is not empty and not in the curated list, append it
				if cfg.Timezone != "" {
					found := false
					for _, item := range items {
						if item.Value == cfg.Timezone || item.Display == cfg.Timezone {
							found = true
							break
						}
					}
					if !found {
						items = append(items, LookupItem{
							Value:   cfg.Timezone,
							Display: cfg.Timezone + " (current)",
						})
					}
				}

				return items
			},
		},
	}
}

// sysFieldsNetwork returns fields for Server Setup sub-screen.
func (m *Model) sysFieldsNetwork(cfg *config.ServerConfig) []fieldDef {
	v3 := &m.configs.V3Net
	hub := &m.configs.V3Net.Hub

	return []fieldDef{
		{
			Label: "SSH Enabled", Help: "Enable SSH server", Type: ftYesNo, Col: 3, Row: 1, Width: 1,
			Get: func() string { return boolToYN(cfg.SSHEnabled) },
			Set: func(val string) error { cfg.SSHEnabled = ynToBool(val); return nil },
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
			Get: func() string { return boolToYN(cfg.LegacySSHAlgorithms) },
			Set: func(val string) error { cfg.LegacySSHAlgorithms = ynToBool(val); return nil },
		},
		{
			Label: "Telnet Enabled", Help: "Enable Telnet server", Type: ftYesNo, Col: 3, Row: 6, Width: 1,
			Get: func() string { return boolToYN(cfg.TelnetEnabled) },
			Set: func(val string) error { cfg.TelnetEnabled = ynToBool(val); return nil },
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
			Get: func() string { return boolToYN(v3.Enabled) },
			Set: func(val string) error { v3.Enabled = ynToBool(val); return nil },
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
			Get: func() string { return boolToYN(hub.Enabled) },
			Set: func(val string) error { hub.Enabled = ynToBool(val); return nil },
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
			Label: "Hub TLS Cert", Help: "Path to TLS certificate (blank for plain HTTP)", Type: ftString, Col: 3, Row: 18, Width: 40,
			Get: func() string { return hub.TLSCert },
			Set: func(val string) error { hub.TLSCert = val; return nil },
		},
		{
			Label: "Hub TLS Key", Help: "Path to TLS private key", Type: ftString, Col: 3, Row: 19, Width: 40,
			Get: func() string { return hub.TLSKey },
			Set: func(val string) error { hub.TLSKey = val; return nil },
		},
		{
			Label: "Hub Data Dir", Help: "Hub data storage directory", Type: ftString, Col: 3, Row: 20, Width: 40,
			Get: func() string { return hub.DataDir },
			Set: func(val string) error { hub.DataDir = val; return nil },
		},
		{
			Label: "Auto Approve", Help: "Automatically approve new leaf subscriptions", Type: ftYesNo, Col: 3, Row: 21, Width: 1,
			Get: func() string { return boolToYN(hub.AutoApprove) },
			Set: func(val string) error { hub.AutoApprove = ynToBool(val); return nil },
		},
	}
}

// sysFieldsLimits returns fields for Connection Limits sub-screen.
func sysFieldsLimits(cfg *config.ServerConfig) []fieldDef {
	return []fieldDef{
		{
			Label: "Max Nodes", Help: "Maximum simultaneous connections", Type: ftInteger, Col: 3, Row: 1, Width: 5, Min: 1, Max: 999,
			Get: func() string { return strconv.Itoa(cfg.MaxNodes) },
			Set: func(val string) error {
				n, err := strconv.Atoi(val)
				if err != nil {
					return err
				}
				cfg.MaxNodes = n
				return nil
			},
		},
		{
			Label: "Max Per IP", Help: "Max connections from a single IP address", Type: ftInteger, Col: 3, Row: 2, Width: 5, Min: 1, Max: 999,
			Get: func() string { return strconv.Itoa(cfg.MaxConnectionsPerIP) },
			Set: func(val string) error {
				n, err := strconv.Atoi(val)
				if err != nil {
					return err
				}
				cfg.MaxConnectionsPerIP = n
				return nil
			},
		},
		{
			Label: "Failed Logins", Help: "Failed attempts before lockout (0=disabled)", Type: ftInteger, Col: 3, Row: 3, Width: 5, Min: 0, Max: 100,
			Get: func() string { return strconv.Itoa(cfg.MaxFailedLogins) },
			Set: func(val string) error {
				n, err := strconv.Atoi(val)
				if err != nil {
					return err
				}
				cfg.MaxFailedLogins = n
				return nil
			},
		},
		{
			Label: "Lockout Mins", Help: "Lockout duration after failed logins", Type: ftInteger, Col: 3, Row: 4, Width: 5, Min: 0, Max: 9999,
			Get: func() string { return strconv.Itoa(cfg.LockoutMinutes) },
			Set: func(val string) error {
				n, err := strconv.Atoi(val)
				if err != nil {
					return err
				}
				cfg.LockoutMinutes = n
				return nil
			},
		},
		{
			Label: "Idle Timeout", Help: "Disconnect idle users after N minutes", Type: ftInteger, Col: 3, Row: 5, Width: 5, Min: 0, Max: 999,
			Get: func() string { return strconv.Itoa(cfg.SessionIdleTimeoutMinutes) },
			Set: func(val string) error {
				n, err := strconv.Atoi(val)
				if err != nil {
					return err
				}
				cfg.SessionIdleTimeoutMinutes = n
				return nil
			},
		},
		{
			Label: "Xfer Timeout", Help: "Max minutes for active file transfers (0=unlimited)", Type: ftInteger, Col: 3, Row: 6, Width: 5, Min: 0, Max: 999,
			Get: func() string { return strconv.Itoa(cfg.TransferTimeoutMinutes) },
			Set: func(val string) error {
				n, err := strconv.Atoi(val)
				if err != nil {
					return err
				}
				cfg.TransferTimeoutMinutes = n
				return nil
			},
		},
	}
}

// sysFieldsLevels returns fields for Access Levels sub-screen.
func sysFieldsLevels(cfg *config.ServerConfig) []fieldDef {
	return []fieldDef{
		{
			Label: "SysOp Level", Help: "Security level for full SysOp access", Type: ftInteger, Col: 3, Row: 1, Width: 3, Min: 0, Max: 255,
			Get: func() string { return strconv.Itoa(cfg.SysOpLevel) },
			Set: func(val string) error {
				n, err := strconv.Atoi(val)
				if err != nil {
					return err
				}
				cfg.SysOpLevel = n
				return nil
			},
		},
		{
			Label: "CoSysOp Level", Help: "Security level for CoSysOp access", Type: ftInteger, Col: 3, Row: 2, Width: 3, Min: 0, Max: 255,
			Get: func() string { return strconv.Itoa(cfg.CoSysOpLevel) },
			Set: func(val string) error {
				n, err := strconv.Atoi(val)
				if err != nil {
					return err
				}
				cfg.CoSysOpLevel = n
				return nil
			},
		},
		{
			Label: "Invisible Lvl", Help: "Level at which user is hidden from who's online", Type: ftInteger, Col: 3, Row: 3, Width: 3, Min: 0, Max: 255,
			Get: func() string { return strconv.Itoa(cfg.InvisibleLevel) },
			Set: func(val string) error {
				n, err := strconv.Atoi(val)
				if err != nil {
					return err
				}
				cfg.InvisibleLevel = n
				return nil
			},
		},
		{
			Label: "New User Level", Help: "Level assigned to new signups", Type: ftInteger, Col: 3, Row: 4, Width: 3, Min: 0, Max: 255,
			Get: func() string { return strconv.Itoa(cfg.NewUserLevel) },
			Set: func(val string) error {
				n, err := strconv.Atoi(val)
				if err != nil {
					return err
				}
				cfg.NewUserLevel = n
				return nil
			},
		},
		{
			Label: "Regular Level", Help: "Level assigned when user is validated", Type: ftInteger, Col: 3, Row: 5, Width: 3, Min: 0, Max: 255,
			Get: func() string { return strconv.Itoa(cfg.RegularUserLevel) },
			Set: func(val string) error {
				n, err := strconv.Atoi(val)
				if err != nil {
					return err
				}
				cfg.RegularUserLevel = n
				return nil
			},
		},
		{
			Label: "Logon Level", Help: "Minimum access level required to log in (0=disabled)", Type: ftInteger, Col: 3, Row: 6, Width: 3, Min: 0, Max: 255,
			Get: func() string { return strconv.Itoa(cfg.LogonLevel) },
			Set: func(val string) error {
				n, err := strconv.Atoi(val)
				if err != nil {
					return err
				}
				cfg.LogonLevel = n
				return nil
			},
		},
		{
			Label: "Anonymous Lvl", Help: "Minimum level required to post anonymously (0=disabled)", Type: ftInteger, Col: 3, Row: 7, Width: 3, Min: 0, Max: 255,
			Get: func() string { return strconv.Itoa(cfg.AnonymousLevel) },
			Set: func(val string) error {
				n, err := strconv.Atoi(val)
				if err != nil {
					return err
				}
				cfg.AnonymousLevel = n
				return nil
			},
		},
	}
}

// sysFieldsDefaults returns fields for Default Settings sub-screen.
func sysFieldsDefaults(cfg *config.ServerConfig) []fieldDef {
	return []fieldDef{
		{
			Label: "Allow New Users", Help: "Allow new user registration", Type: ftYesNo, Col: 3, Row: 1, Width: 1,
			Get: func() string { return boolToYN(cfg.AllowNewUsers) },
			Set: func(val string) error { cfg.AllowNewUsers = ynToBool(val); return nil },
		},
		{
			Label: "File List Mode", Help: "File listing style", Type: ftLookup, Col: 3, Row: 2, Width: 15,
			Get: func() string { return cfg.FileListingMode },
			Set: func(val string) error { cfg.FileListingMode = val; return nil },
			LookupItems: func() []LookupItem {
				return []LookupItem{
					{Value: "lightbar", Display: "lightbar - Interactive arrow-key navigation"},
					{Value: "classic", Display: "classic - Traditional numbered list"},
				}
			},
		},
		{
			Label: "Del User Days", Help: "Days to keep deleted user records (0=purge now, -1=forever)", Type: ftInteger, Col: 3, Row: 3, Width: 5, Min: -1, Max: 9999,
			Get: func() string { return strconv.Itoa(cfg.DeletedUserRetentionDays) },
			Set: func(val string) error {
				n, err := strconv.Atoi(val)
				if err != nil {
					return err
				}
				cfg.DeletedUserRetentionDays = n
				return nil
			},
		},
	}
}

// sysFieldsIPLists returns fields for IP Blocklist/Allowlist sub-screen.
func sysFieldsIPLists(cfg *config.ServerConfig) []fieldDef {
	return []fieldDef{
		{
			Label: "Blocklist Path", Help: "Path to IP blocklist file (one IP per line)", Type: ftString, Col: 3, Row: 1, Width: 45,
			Get: func() string { return cfg.IPBlocklistPath },
			Set: func(val string) error { cfg.IPBlocklistPath = val; return nil },
		},
		{
			Label: "Allowlist Path", Help: "Path to IP allowlist file (one IP per line)", Type: ftString, Col: 3, Row: 2, Width: 45,
			Get: func() string { return cfg.IPAllowlistPath },
			Set: func(val string) error { cfg.IPAllowlistPath = val; return nil },
		},
	}
}

// sysFieldsNUV returns fields for the New User Voting sub-screen.
func sysFieldsNUV(cfg *config.ServerConfig) []fieldDef {
	return []fieldDef{
		{
			Label: "Use NUV", Help: "Enable New User Voting system", Type: ftYesNo, Col: 3, Row: 1, Width: 1,
			Get: func() string { return boolToYN(cfg.UseNUV) },
			Set: func(val string) error { cfg.UseNUV = ynToBool(val); return nil },
		},
		{
			Label: "Auto Add NUV", Help: "Automatically queue new registrations for voting", Type: ftYesNo, Col: 3, Row: 2, Width: 1,
			Get: func() string { return boolToYN(cfg.AutoAddNUV) },
			Set: func(val string) error { cfg.AutoAddNUV = ynToBool(val); return nil },
		},
		{
			Label: "NUV Use Level", Help: "Minimum access level required to vote", Type: ftInteger, Col: 3, Row: 3, Width: 3, Min: 0, Max: 255,
			Get: func() string { return strconv.Itoa(cfg.NUVUseLevel) },
			Set: func(val string) error {
				n, err := strconv.Atoi(val)
				if err != nil {
					return err
				}
				cfg.NUVUseLevel = n
				return nil
			},
		},
		{
			Label: "NUV Yes Votes", Help: "YES votes required to trigger yes threshold", Type: ftInteger, Col: 3, Row: 4, Width: 3, Min: 1, Max: 999,
			Get: func() string { return strconv.Itoa(cfg.NUVYesVotes) },
			Set: func(val string) error {
				n, err := strconv.Atoi(val)
				if err != nil {
					return err
				}
				cfg.NUVYesVotes = n
				return nil
			},
		},
		{
			Label: "NUV No Votes", Help: "NO votes required to trigger no threshold", Type: ftInteger, Col: 3, Row: 5, Width: 3, Min: 1, Max: 999,
			Get: func() string { return strconv.Itoa(cfg.NUVNoVotes) },
			Set: func(val string) error {
				n, err := strconv.Atoi(val)
				if err != nil {
					return err
				}
				cfg.NUVNoVotes = n
				return nil
			},
		},
		{
			Label: "NUV Validate", Help: "Auto-validate user when yes threshold reached", Type: ftYesNo, Col: 3, Row: 6, Width: 1,
			Get: func() string { return boolToYN(cfg.NUVValidate) },
			Set: func(val string) error { cfg.NUVValidate = ynToBool(val); return nil },
		},
		{
			Label: "NUV Kill", Help: "Auto-delete user when no threshold reached", Type: ftYesNo, Col: 3, Row: 7, Width: 1,
			Get: func() string { return boolToYN(cfg.NUVKill) },
			Set: func(val string) error { cfg.NUVKill = ynToBool(val); return nil },
		},
		{
			Label: "NUV Level", Help: "Access level assigned to auto-validated NUV users", Type: ftInteger, Col: 3, Row: 8, Width: 3, Min: 0, Max: 255,
			Get: func() string { return strconv.Itoa(cfg.NUVLevel) },
			Set: func(val string) error {
				n, err := strconv.Atoi(val)
				if err != nil {
					return err
				}
				cfg.NUVLevel = n
				return nil
			},
		},
		{
			Label: "NUV Form", Help: "Infoform number (1-5) shown to voters; 0 = disabled", Type: ftInteger, Col: 3, Row: 9, Width: 1, Min: 0, Max: 5,
			Get: func() string { return strconv.Itoa(cfg.NUVForm) },
			Set: func(val string) error {
				n, err := strconv.Atoi(val)
				if err != nil {
					return err
				}
				cfg.NUVForm = n
				return nil
			},
		},
	}
}

// sysFieldsDOS returns fields for DOS Emulation sub-screen.
func sysFieldsDOS(cfg *config.ServerConfig) []fieldDef {
	return []fieldDef{
		{
			Label: "DOSemu Path", Help: "Path to dosemu2 binary (blank=auto-detect)", Type: ftString, Col: 3, Row: 1, Width: 45,
			Get: func() string { return cfg.DosemuPath },
			Set: func(val string) error { cfg.DosemuPath = val; return nil },
		},
	}
}
