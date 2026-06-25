package configeditor

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/ftn"
)

// fieldsFTNWizard returns field definitions for the FTN setup wizard form.
func (m *Model) fieldsFTNWizard() []fieldDef {
	w := m.ftnWizard
	return []fieldDef{
		{
			Label: "Network", Help: "Press Enter to browse known FTN networks", Type: ftDisplay, Col: 3, Row: 1, Width: 40,
			Get: func() string {
				if w.networkName != "" {
					return w.networkName
				}
				return "(press Enter to browse)"
			},
		},
		{
			Label: "Description", Help: "Network description (read-only)", Type: ftDisplay, Col: 3, Row: 2, Width: 40,
			Get: func() string { return w.networkDesc },
		},
		{
			Label: "Coordinator", Help: "Network coordinator (read-only)", Type: ftDisplay, Col: 3, Row: 3, Width: 40,
			Get: func() string {
				if w.coordinator == "" {
					return ""
				}
				if w.coordinatorEmail != "" {
					return w.coordinator + " <" + w.coordinatorEmail + ">"
				}
				return w.coordinator
			},
		},
		{
			Label: "Info", Help: "Network info URL (read-only)", Type: ftDisplay, Col: 3, Row: 4, Width: 40,
			Get: func() string { return w.infoURL },
		},
		{
			Label: "Your Address", Help: "Your FTN address (zone:net/node or zone:net/node.point)", Type: ftString, Col: 3, Row: 6, Width: 20,
			Get: func() string { return w.ownAddress },
			Set: func(val string) error {
				val = strings.TrimSpace(val)
				if val == "" {
					return fmt.Errorf("cannot be empty")
				}
				if err := ftn.ValidateAddress(val); err != nil {
					return err
				}
				w.ownAddress = val
				return nil
			},
		},
		{
			Label: "Hub Address", Help: "Hub's FTN address (zone:net/node)", Type: ftString, Col: 3, Row: 7, Width: 20,
			Get: func() string { return w.hubAddress },
			Set: func(val string) error {
				val = strings.TrimSpace(val)
				if val == "" {
					return fmt.Errorf("cannot be empty")
				}
				if err := ftn.ValidateAddress(val); err != nil {
					return err
				}
				w.hubAddress = val
				return nil
			},
		},
		{
			Label: "Hub Hostname", Help: "Hub BinkP hostname or IP address", Type: ftString, Col: 3, Row: 8, Width: 35,
			Get: func() string { return w.hubHostname },
			Set: func(val string) error {
				val = strings.TrimSpace(val)
				if val == "" {
					return fmt.Errorf("cannot be empty")
				}
				w.hubHostname = val
				return nil
			},
		},
		{
			Label: "Hub BinkP Port", Help: "BinkP port (1-65535, default 24554)", Type: ftInteger, Col: 3, Row: 9, Width: 6, Min: 1, Max: 65535,
			Get: func() string { return strconv.Itoa(w.hubPort) },
			Set: func(val string) error {
				val = strings.TrimSpace(val)
				p, err := strconv.Atoi(val)
				if err != nil || p < 1 || p > 65535 {
					return fmt.Errorf("must be 1-65535")
				}
				w.hubPort = p
				return nil
			},
		},
		{
			Label: "Areafix Pwd", Help: "AreaFix password (required, case-insensitive)", Type: ftString, Col: 3, Row: 11, Width: 20, Masked: true,
			Get: func() string { return w.areafixPassword },
			Set: func(val string) error {
				val = strings.TrimSpace(val)
				if val == "" {
					return fmt.Errorf("cannot be empty")
				}
				w.areafixPassword = val
				return nil
			},
		},
		{
			Label: "Session Pwd", Help: "BinkP session password (required)", Type: ftString, Col: 3, Row: 12, Width: 20, Masked: true,
			Get: func() string { return w.sessionPassword },
			Set: func(val string) error {
				val = strings.TrimSpace(val)
				if val == "" {
					return fmt.Errorf("cannot be empty")
				}
				w.sessionPassword = val
				return nil
			},
		},
		{
			Label: "Packet Pwd", Help: "Packet password (optional, max 8 chars)", Type: ftString, Col: 3, Row: 13, Width: 8, Masked: true,
			Get: func() string { return w.packetPassword },
			Set: func(val string) error {
				val = strings.TrimSpace(val)
				if len(val) > 8 {
					return fmt.Errorf("max 8 characters")
				}
				w.packetPassword = val
				return nil
			},
		},
		{
			Label: "Origin Line", Help: "Echomail origin line (leave blank for default)", Type: ftString, Col: 3, Row: 15, Width: 45,
			Get: func() string { return w.originLine },
			Set: func(val string) error {
				w.originLine = strings.TrimSpace(val)
				return nil
			},
		},
		{
			Label: "Echo Areas", Help: "Press Enter to download and browse echo areas", Type: ftDisplay, Col: 3, Row: 17, Width: 40,
			Get: func() string {
				n := w.selectedAreaCount()
				if n == 0 {
					if w.areasFetched {
						return "(none selected — press Enter to browse)"
					}
					return "(press Enter to download area list)"
				}
				return fmt.Sprintf("%d area(s) selected", n)
			},
		},
	}
}

// validateFTNWizard validates all FTN wizard fields and returns the first error.
func (m *Model) validateFTNWizard() error {
	for _, f := range m.ftnWizardFields {
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
