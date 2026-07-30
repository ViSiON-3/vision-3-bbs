package configeditor

import (
	"strconv"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/uitext"
)

// Field definitions for the System Setup sub-screens that configure the board
// itself: registration details, per-user defaults, and DOS emulation.

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
			Label: "BBS Location", Help: "Geographic location (e.g. Auckland, NZ)", Type: ftString, Col: 3, Row: 3, Width: 40,
			Get: func() string { return cfg.BBSLocation },
			Set: func(val string) error { cfg.BBSLocation = val; return nil },
		},
		{
			Label: "Timezone", Help: "IANA timezone", Type: ftLookup, Col: 3, Row: 4, Width: 30,
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
		{
			Label: "QWK ID", Help: "Stable QWK packet ID (max 8, A-Z/0-9); blank = derive from Board Name",
			Type: ftString, Col: 3, Row: 5, Width: 8,
			Get: func() string { return cfg.QWKID },
			Set: func(val string) error { cfg.QWKID = config.NormalizeQWKID(val); return nil },
		},
	}
}

// sysFieldsDefaults returns fields for Default Settings sub-screen.
func sysFieldsDefaults(cfg *config.ServerConfig) []fieldDef {
	return []fieldDef{
		{
			Label: "Allow New Users", Help: "Allow new user registration", Type: ftYesNo, Col: 3, Row: 1, Width: 1,
			Get: func() string { return uitext.BoolToYN(cfg.AllowNewUsers) },
			Set: func(val string) error { cfg.AllowNewUsers = uitext.YNToBool(val); return nil },
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
