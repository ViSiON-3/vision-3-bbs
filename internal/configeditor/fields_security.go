package configeditor

import (
	"strconv"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/uitext"
)

// Field definitions for the Access & Security sub-screens that govern who may
// connect and at what level.

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
			Label: "WFC Access", Help: "Allow remote WFC sysop console connections", Type: ftYesNo, Col: 3, Row: 3, Width: 1,
			Get: func() string { return uitext.BoolToYN(cfg.WFCEnabled) },
			Set: func(val string) error { cfg.WFCEnabled = uitext.YNToBool(val); return nil },
		},
		{
			Label: "Invisible Lvl", Help: "Level at which user is hidden from who's online", Type: ftInteger, Col: 3, Row: 4, Width: 3, Min: 0, Max: 255,
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
			Label: "New User Level", Help: "Level assigned to new signups", Type: ftInteger, Col: 3, Row: 5, Width: 3, Min: 0, Max: 255,
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
			Label: "Regular Level", Help: "Level assigned when user is validated", Type: ftInteger, Col: 3, Row: 6, Width: 3, Min: 0, Max: 255,
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
			Label: "Logon Level", Help: "Minimum access level required to log in (0=disabled)", Type: ftInteger, Col: 3, Row: 7, Width: 3, Min: 0, Max: 255,
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
			Label: "Anonymous Lvl", Help: "Minimum level required to post anonymously (0=disabled)", Type: ftInteger, Col: 3, Row: 8, Width: 3, Min: 0, Max: 255,
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
