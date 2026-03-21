package configeditor

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// fieldsLeafWizard returns field definitions for the leaf setup form.
func (m *Model) fieldsLeafWizard() []fieldDef {
	w := m.wizard
	return []fieldDef{
		{
			Label: "Registry", Help: "Press Enter to browse known networks", Type: ftDisplay, Col: 3, Row: 1, Width: 45,
			Get: func() string { return "(press Enter to browse available networks)" },
		},
		{
			Label: "Hub URL", Help: "URL of the V3Net hub (e.g. https://hub.example.org)", Type: ftString, Col: 3, Row: 2, Width: 45,
			Get: func() string { return w.hubURL },
			Set: func(val string) error {
				val = strings.TrimSpace(val)
				if val == "" || (!strings.HasPrefix(val, "http://") && !strings.HasPrefix(val, "https://")) {
					return fmt.Errorf("must start with http:// or https://")
				}
				w.hubURL = val
				return nil
			},
		},
		{
			Label: "Network", Help: "Network name to subscribe to (e.g. felonynet)", Type: ftString, Col: 3, Row: 3, Width: 30,
			Get: func() string { return w.networkName },
			Set: func(val string) error {
				val = strings.TrimSpace(val)
				if val == "" {
					return fmt.Errorf("cannot be empty")
				}
				w.networkName = val
				return nil
			},
		},
		{
			Label: "Areas", Help: "Press Enter to browse and subscribe to network areas", Type: ftDisplay, Col: 3, Row: 4, Width: 45,
			Get: func() string {
				n := 0
				for _, a := range w.selectedAreas {
					if a.Subscribed {
						n++
					}
				}
				if n == 0 {
					return "(none — press Enter to browse)"
				}
				return fmt.Sprintf("%d area(s) selected", n)
			},
		},
		{
			Label: "Poll Interval", Help: "How often to poll for new messages (e.g. 5m, 30s, 1h)", Type: ftString, Col: 3, Row: 5, Width: 10,
			Get: func() string { return w.pollInterval },
			Set: func(val string) error {
				val = strings.TrimSpace(val)
				d, err := time.ParseDuration(val)
				if err != nil || d <= 0 {
					return fmt.Errorf("must be a valid duration (e.g. 5m, 30s)")
				}
				w.pollInterval = val
				return nil
			},
		},
		{
			Label: "Origin", Help: "Leave blank to use BBS name", Type: ftString, Col: 3, Row: 6, Width: 45,
			Get: func() string { return w.origin },
			Set: func(val string) error {
				w.origin = strings.TrimSpace(val)
				return nil
			},
		},
	}
}

// fieldsHubWizard returns field definitions for the hub setup form.
// The "Initial Areas" field uses a special AfterSet callback to open the areas sub-form.
func (m *Model) fieldsHubWizard() []fieldDef {
	w := m.wizard
	return []fieldDef{
		{
			Label: "Network Name", Help: "Short lowercase alphanumeric identifier (e.g. felonynet)", Type: ftString, Col: 3, Row: 1, Width: 30,
			Get: func() string { return w.netName },
			Set: func(val string) error {
				val = strings.TrimSpace(val)
				if val == "" {
					return fmt.Errorf("cannot be empty")
				}
				for _, c := range val {
					if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9')) {
						return fmt.Errorf("must be lowercase alphanumeric only")
					}
				}
				w.netName = val
				return nil
			},
		},
		{
			Label: "Description", Help: "Human-readable description shown to subscribers", Type: ftString, Col: 3, Row: 2, Width: 45,
			Get: func() string { return w.netDesc },
			Set: func(val string) error {
				w.netDesc = strings.TrimSpace(val)
				return nil
			},
		},
		{
			Label: "Listen Port", Help: "TCP port for the hub server (default: 8765)", Type: ftInteger, Col: 3, Row: 3, Width: 6, Min: 1, Max: 65535,
			Get: func() string { return w.port },
			Set: func(val string) error {
				val = strings.TrimSpace(val)
				p, err := strconv.Atoi(val)
				if err != nil || p < 1 || p > 65535 {
					return fmt.Errorf("must be 1-65535")
				}
				w.port = val
				return nil
			},
		},
		{
			Label: "Auto-Approve", Help: "Auto-approve new leaf connections", Type: ftYesNo, Col: 3, Row: 4, Width: 1,
			Get: func() string { return boolToYN(w.autoApprove) },
			Set: func(val string) error {
				w.autoApprove = ynToBool(val)
				return nil
			},
		},
		{
			Label: "Initial Areas", Help: "Press Enter to manage initial message areas", Type: ftDisplay, Col: 3, Row: 6, Width: 30,
			Get: func() string {
				n := len(w.areas)
				if n == 0 {
					return "(none — press Enter to add)"
				}
				return fmt.Sprintf("%d area(s) configured", n)
			},
		},
	}
}

// validateLeafWizard validates all leaf wizard fields and returns the first error.
func (m *Model) validateLeafWizard() error {
	for _, f := range m.wizardFields {
		if f.Set == nil {
			continue
		}
		val := f.Get()
		if err := f.Set(val); err != nil {
			return fmt.Errorf("%s: %v", f.Label, err)
		}
	}
	return nil
}

// validateHubWizard validates all hub wizard fields (except areas) and returns the first error.
func (m *Model) validateHubWizard() error {
	for _, f := range m.wizardFields {
		if f.Set == nil || f.Type == ftDisplay {
			continue
		}
		val := f.Get()
		if err := f.Set(val); err != nil {
			return fmt.Errorf("%s: %v", f.Label, err)
		}
	}
	// Validate area tags.
	seen := make(map[string]bool)
	for _, a := range m.wizard.areas {
		if err := protocol.ValidateAreaTag(a.Tag); err != nil {
			return fmt.Errorf("area %q: %v", a.Tag, err)
		}
		if a.Name == "" {
			return fmt.Errorf("area %q: name cannot be empty", a.Tag)
		}
		if seen[a.Tag] {
			return fmt.Errorf("area %q: duplicate tag", a.Tag)
		}
		seen[a.Tag] = true
	}
	return nil
}
