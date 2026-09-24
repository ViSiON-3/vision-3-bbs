package configeditor

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/robfig/cron/v3"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/uitext"
)

// fieldsQWKWizard returns the QWK network wizard form.
func (m *Model) fieldsQWKWizard() []fieldDef {
	w := m.qwkWizard
	networkHelp := "Press Enter to pick a known QWK network, or fill the fields in for any hub"
	if w.editing() {
		networkHelp = "Editing an existing network; its key is fixed"
	}
	return []fieldDef{
		{
			Label: "Network", Help: networkHelp, Type: ftDisplay, Col: 3, Row: 1, Width: 45,
			Get: func() string {
				switch {
				case w.editing():
					return fmt.Sprintf("%s  (editing %s)", w.networkName, w.editingKey)
				case w.known != nil:
					return w.known.Name + "  (" + w.known.Description + ")"
				case w.networkName != "":
					return w.networkName + "  (custom)"
				}
				return "(press Enter to browse known networks, or type a key below)"
			},
		},
		{
			Label: "Network Key", Help: "Short lowercase key used in message areas and events (e.g. dovenet)", Type: ftString, Col: 3, Row: 2, Width: 20,
			Get: func() string { return w.networkKey },
			Set: func(val string) error {
				val = strings.ToLower(strings.TrimSpace(val))
				if val == "" {
					return fmt.Errorf("cannot be empty")
				}
				if strings.ContainsAny(val, " /\\:") {
					return fmt.Errorf("letters, digits, - and _ only")
				}
				if w.editing() && val != w.editingKey {
					return fmt.Errorf("key is fixed while editing")
				}
				if !w.editing() {
					if _, exists := m.configs.QWKNet.Networks[val]; exists {
						return fmt.Errorf("network %q already exists — edit it under QWK Networks", val)
					}
				}
				w.networkKey = val
				if w.networkName == "" {
					w.networkName = val
				}
				return nil
			},
		},
		{
			Label: "Hub QWK-ID", Help: "The hub's QWK ID (e.g. VERT); packets are named after it", Type: ftString, Col: 3, Row: 3, Width: 8,
			Get: func() string { return w.hubID },
			Set: func(val string) error {
				id := config.NormalizeQWKID(val)
				if id == "" {
					return fmt.Errorf("letters and digits, max 8")
				}
				w.hubID = id
				return nil
			},
		},
		{
			Label: "Hub Host", Help: "Hub FTP hostname (e.g. vert.synchro.net)", Type: ftString, Col: 3, Row: 4, Width: 40,
			Get: func() string { return w.host },
			Set: func(val string) error {
				val = strings.TrimSpace(val)
				if val == "" {
					return fmt.Errorf("cannot be empty")
				}
				w.host = val
				return nil
			},
		},
		{
			Label: "Hub Port", Help: "Hub FTP port (default 21)", Type: ftInteger, Col: 3, Row: 5, Width: 6, Min: 1, Max: 65535,
			Get: func() string { return strconv.Itoa(w.port) },
			Set: func(val string) error {
				p, err := strconv.Atoi(strings.TrimSpace(val))
				if err != nil || p < 1 || p > 65535 {
					return fmt.Errorf("must be 1-65535")
				}
				w.port = p
				return nil
			},
		},
		{
			Label: "Your QWK-ID", Help: "Your node ID on the hub, from System Setup > Registration > QWK ID (or derived from the board name)", Type: ftDisplay, Col: 3, Row: 7, Width: 40,
			Get: func() string {
				if id := m.systemQWKID(); id != "" {
					return id
				}
				return "(none — set QWK ID under System Setup first)"
			},
		},
		{
			Label: "Login Name", Help: "FTP user name on the hub; blank = your QWK ID", Type: ftString, Col: 3, Row: 8, Width: 25,
			Get: func() string { return w.loginName },
			Set: func(val string) error { w.loginName = strings.TrimSpace(val); return nil },
		},
		{
			Label: "Password", Help: "Password of your node account on the hub (required)", Type: ftString, Col: 3, Row: 9, Width: 30, Masked: true,
			Get: func() string { return w.password },
			Set: func(val string) error {
				val = strings.TrimSpace(val)
				if val == "" {
					return fmt.Errorf("cannot be empty")
				}
				w.password = val
				return nil
			},
		},
		{
			Label: "Tagline", Help: "Added under a tearline on every message you send (blank = none)", Type: ftString, Col: 3, Row: 11, Width: 45,
			Get: func() string { return w.tagline },
			Set: func(val string) error { w.tagline = strings.TrimSpace(val); return nil },
		},
		{
			Label: "Poll Schedule", Help: "Cron schedule for polling the hub (default */30 * * * *)", Type: ftString, Col: 3, Row: 12, Width: 20,
			Get: func() string { return w.schedule },
			Set: func(val string) error {
				val = strings.TrimSpace(val)
				if val == "" {
					val = defaultQWKPollSchedule
				}
				// The same parser the scheduler uses, so "@foo" or a six
				// field spec is refused here instead of silently never running.
				if _, err := cron.ParseStandard(val); err != nil {
					return fmt.Errorf("five cron fields (min hour day month weekday) or @hourly/@daily: %v", err)
				}
				w.schedule = val
				return nil
			},
		},
		{
			Label: "Newscan Default", Help: "Add the new areas to users' newscan by default (Y/n)", Type: ftYesNo, Col: 3, Row: 13, Width: 1,
			Get: func() string { return uitext.BoolToYN(w.autoJoin) },
			Set: func(val string) error { w.autoJoin = uitext.YNToBool(val); return nil },
		},
		{
			Label: "Conferences", Help: "Press Enter to choose which hub conferences to carry", Type: ftDisplay, Col: 3, Row: 15, Width: 45,
			Get: func() string {
				n := w.selectedCount()
				switch {
				case w.fetching:
					return "connecting to hub..."
				case n > 0:
					if add := w.newSelectedCount(); w.editing() && add != n {
						return fmt.Sprintf("%d selected (%d new)", n, add)
					}
					return fmt.Sprintf("%d selected", n)
				case w.confsFetched:
					return "(none selected — press Enter to choose)"
				case len(w.existingConfs) > 0:
					return fmt.Sprintf("%d already configured — press Enter to add more", len(w.existingConfs))
				case w.known != nil && len(w.known.Conferences) > 0:
					return "(press Enter to choose from the network's list)"
				}
				return "(press Enter to fetch the list from the hub)"
			},
		},
	}
}

// validateQWKWizard re-applies every setter so a value that was pre-filled
// rather than typed is checked too.
func (m *Model) validateQWKWizard() error {
	for _, f := range m.qwkWizardFields {
		if f.Set == nil {
			continue
		}
		if err := f.Set(f.Get()); err != nil {
			return fmt.Errorf("%s: %v", f.Label, err)
		}
	}
	// The node's identity on the network is the system QWK ID whatever the
	// FTP login is called; the poller refuses to run without one.
	if m.systemQWKID() == "" {
		return fmt.Errorf("your QWK-ID: this system has no QWK ID; set one under System Setup > Registration first")
	}
	if w := m.qwkWizard; w != nil {
		self := w.networkKey
		if w.editing() {
			self = w.editingKey
		}
		if other := m.configs.QWKNet.HubIDOwner(w.hubID, self); other != "" {
			return fmt.Errorf("hub QWK-ID: network %q already uses hub %s; edit that network instead", other, config.NormalizeQWKID(w.hubID))
		}
	}
	return nil
}
