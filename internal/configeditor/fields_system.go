package configeditor

// Field definitions for the System Setup and Access & Security inner menus.
// The sysFields* builders themselves live in the fields_system_*/fields_security*
// files named for the sub-screen they build.

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

// buildSysFields returns the field definitions for the currently loaded inner
// menu's sub-screen at the given index.
func (m *Model) buildSysFields(screen int) []fieldDef {
	if screen < 0 || screen >= len(m.sysMenuItems) {
		return nil
	}
	return m.sysMenuItems[screen].Build(m)
}

// systemConfigMenuItems returns the System Setup inner menu (general board and
// server settings).
func systemConfigMenuItems() []sysConfigMenuItem {
	return []sysConfigMenuItem{
		{Label: "BBS Registration", Build: func(m *Model) []fieldDef { return sysFieldsRegistration(&m.configs.Server) }},
		{Label: "Server Setup", Build: func(m *Model) []fieldDef { return m.sysFieldsNetwork(&m.configs.Server) }},
		{Label: "Default Settings", Build: func(m *Model) []fieldDef { return sysFieldsDefaults(&m.configs.Server) }},
		{Label: "DOS Emulation", Build: func(m *Model) []fieldDef { return sysFieldsDOS(&m.configs.Server) }},
		{Label: "Logging", Build: func(m *Model) []fieldDef { return sysFieldsLogging(&m.configs.Server) }},
		{Label: "QWK Mobile API", Build: func(m *Model) []fieldDef { return sysFieldsQWKAPI(&m.configs.Server) }},
	}
}

// securityMenuItems returns the Access & Security inner menu (access control and
// abuse-defense settings).
func securityMenuItems() []sysConfigMenuItem {
	return []sysConfigMenuItem{
		{Label: "Access Levels", Build: func(m *Model) []fieldDef { return sysFieldsLevels(&m.configs.Server) }},
		{Label: "Connection Limits", Build: func(m *Model) []fieldDef { return sysFieldsLimits(&m.configs.Server) }},
		{Label: "Bot Defense", Build: func(m *Model) []fieldDef { return sysFieldsBotDefense(&m.configs.Server) }},
		{Label: "IP Blocklist/Allowlist", Build: func(m *Model) []fieldDef { return sysFieldsIPLists(&m.configs.Server) }},
		{Label: "New User Voting (NUV)", Build: func(m *Model) []fieldDef { return sysFieldsNUV(&m.configs.Server) }},
	}
}
