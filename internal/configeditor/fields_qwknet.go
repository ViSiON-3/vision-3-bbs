package configeditor

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/uitext"
)

// qwkNetworkKeys returns the configured QWK network keys, sorted, so the
// map-backed record list has a stable order.
func (m Model) qwkNetworkKeys() []string {
	if m.configs == nil {
		return nil
	}
	return m.configs.QWKNet.NetworkKeys()
}

// buildQWKNetworkLookupItems lists the configured QWK networks for the
// message area editor's Network field.
func (m *Model) buildQWKNetworkLookupItems() []LookupItem {
	keys := m.qwkNetworkKeys()
	items := make([]LookupItem, 0, len(keys))
	for _, k := range keys {
		label := k
		if name := m.configs.QWKNet.Networks[k].Name; name != "" {
			label = fmt.Sprintf("%s - %s", k, name)
		}
		items = append(items, LookupItem{Value: k, Display: label})
	}
	return items
}

// systemQWKID is the QWK ID this system identifies itself with: the
// configured one, else one derived from the board name, as the offline
// mail door resolves it.
func (m Model) systemQWKID() string {
	if m.configs == nil {
		return ""
	}
	if id := config.NormalizeQWKID(m.configs.Server.QWKID); id != "" {
		return id
	}
	return config.NormalizeQWKID(m.configs.Server.BoardName)
}

// fieldsQWKNetwork returns the fields for one configured QWK network.
func (m *Model) fieldsQWKNetwork() []fieldDef {
	keys := m.qwkNetworkKeys()
	idx := m.recordEditIdx
	if idx < 0 || idx >= len(keys) {
		return nil
	}
	key := keys[idx]
	net := m.configs.QWKNet.Networks[key]
	netPtr := &net
	save := func() { m.configs.QWKNet.Networks[key] = *netPtr }

	return []fieldDef{
		{
			Label: "Network Key", Help: "Short lowercase key message areas refer to (e.g. dovenet)", Type: ftString, Col: 3, Row: 1, Width: 20,
			Get: func() string { return key },
			Set: func(val string) error {
				val = strings.ToLower(strings.TrimSpace(val))
				if val == "" {
					return fmt.Errorf("network key cannot be empty")
				}
				if val == key {
					return nil
				}
				if _, exists := m.configs.QWKNet.Networks[val]; exists {
					return fmt.Errorf("network %q already exists", val)
				}
				cfg := m.configs.QWKNet.Networks[key]
				m.configs.QWKNet.Networks[val] = cfg
				delete(m.configs.QWKNet.Networks, key)
				for i := range m.configs.MsgAreas {
					a := &m.configs.MsgAreas[i]
					if a.IsQWKNet() && strings.EqualFold(a.Network, key) {
						a.Network = val
					}
				}
				renameQWKPollEvent(&m.configs.Events, key, val)
				key = val
				return nil
			},
		},
		{
			Label: "Name", Help: "Display name of the network (e.g. DOVE-Net)", Type: ftString, Col: 3, Row: 2, Width: 30,
			Get: func() string { return netPtr.Name },
			Set: func(val string) error { netPtr.Name = strings.TrimSpace(val); save(); return nil },
		},
		{
			Label: "Enabled", Help: "Poll this hub on the scheduled event (Y/N)", Type: ftYesNo, Col: 3, Row: 3, Width: 1,
			Get: func() string { return uitext.BoolToYN(netPtr.Enabled) },
			Set: func(val string) error { netPtr.Enabled = uitext.YNToBool(val); save(); return nil },
		},
		{
			Label: "Hub QWK-ID", Help: "The hub's QWK ID; packets are <ID>.REP up and <ID>.QWK down (e.g. VERT)", Type: ftString, Col: 3, Row: 5, Width: 8,
			Get: func() string { return netPtr.HubID },
			Set: func(val string) error {
				id := config.NormalizeQWKID(val)
				if id == "" {
					return fmt.Errorf("hub QWK ID is required (letters and digits, max 8)")
				}
				netPtr.HubID = id
				save()
				return nil
			},
		},
		{
			Label: "Hub Host", Help: "Hub FTP hostname or IP address", Type: ftString, Col: 3, Row: 6, Width: 40,
			Get: func() string { return netPtr.Host },
			Set: func(val string) error {
				val = strings.TrimSpace(val)
				if val == "" {
					return fmt.Errorf("hub host is required")
				}
				netPtr.Host = val
				save()
				return nil
			},
		},
		{
			Label: "Hub Port", Help: "Hub FTP port (default 21)", Type: ftInteger, Col: 3, Row: 7, Width: 6, Min: 1, Max: 65535,
			Get: func() string {
				if netPtr.Port <= 0 {
					return "21"
				}
				return strconv.Itoa(netPtr.Port)
			},
			Set: func(val string) error {
				n, err := strconv.Atoi(strings.TrimSpace(val))
				if err != nil || n < 1 || n > 65535 {
					return fmt.Errorf("must be 1-65535")
				}
				netPtr.Port = n
				save()
				return nil
			},
		},
		{
			Label: "Login Name", Help: "FTP user name on the hub; blank = your QWK ID (" + m.systemQWKID() + ")", Type: ftString, Col: 3, Row: 8, Width: 25,
			Get: func() string { return netPtr.Username },
			Set: func(val string) error { netPtr.Username = strings.TrimSpace(val); save(); return nil },
		},
		{
			Label: "Password", Help: "Password of your node account on the hub", Type: ftString, Col: 3, Row: 9, Width: 30, Masked: true,
			Get: func() string { return netPtr.Password },
			Set: func(val string) error {
				val = strings.TrimSpace(val)
				if val == "" {
					return fmt.Errorf("password is required")
				}
				netPtr.Password = val
				save()
				return nil
			},
		},
		{
			Label: "Tagline", Help: "Added under a tearline on every message you send the hub (blank = none)", Type: ftString, Col: 3, Row: 11, Width: 45,
			Get: func() string { return netPtr.Tagline },
			Set: func(val string) error { netPtr.Tagline = strings.TrimSpace(val); save(); return nil },
		},
		{
			Label: "HEADERS.DAT", Help: "Send HEADERS.DAT with full names, Message-IDs and time zones (Y recommended)", Type: ftYesNo, Col: 3, Row: 12, Width: 1,
			Get: func() string { return uitext.BoolToYN(!netPtr.NoHeaders) },
			Set: func(val string) error { netPtr.NoHeaders = !uitext.YNToBool(val); save(); return nil },
		},
		{
			Label: "Timeout", Help: "Seconds allowed per transfer (0 = 300)", Type: ftInteger, Col: 3, Row: 13, Width: 5, Min: 0, Max: 86400,
			Get: func() string { return strconv.Itoa(netPtr.TimeoutSeconds) },
			Set: func(val string) error {
				n, err := strconv.Atoi(strings.TrimSpace(val))
				if err != nil || n < 0 {
					return fmt.Errorf("must be 0 or more")
				}
				netPtr.TimeoutSeconds = n
				save()
				return nil
			},
		},
	}
}

// fieldsQWKNetGlobal returns the shared path settings from qwknet.json.
func (m *Model) fieldsQWKNetGlobal() []fieldDef {
	qc := &m.configs.QWKNet
	qc.ApplyDefaults()
	return []fieldDef{
		{
			Label: "Inbound Path", Help: "Directory downloaded QWK packets wait in until tossed", Type: ftString, Col: 3, Row: 1, Width: 45,
			Get: func() string { return qc.InboundPath },
			Set: func(val string) error { qc.InboundPath = strings.TrimSpace(val); return nil },
		},
		{
			Label: "Outbound Path", Help: "Directory packed REP files wait in until uploaded", Type: ftString, Col: 3, Row: 2, Width: 45,
			Get: func() string { return qc.OutboundPath },
			Set: func(val string) error { qc.OutboundPath = strings.TrimSpace(val); return nil },
		},
		{
			Label: "Temp Path", Help: "Scratch directory for building and unpacking packets", Type: ftString, Col: 3, Row: 3, Width: 45,
			Get: func() string { return qc.TempPath },
			Set: func(val string) error { qc.TempPath = strings.TrimSpace(val); return nil },
		},
		{
			Label: "Dupe DB Path", Help: "JSON file of Message-IDs already imported (90-day memory)", Type: ftString, Col: 3, Row: 4, Width: 45,
			Get: func() string { return qc.DupeDBPath },
			Set: func(val string) error { qc.DupeDBPath = strings.TrimSpace(val); return nil },
		},
		{
			Label: "Bad Area Tag", Help: "Message area tag that receives mail for conferences no area mirrors (blank = drop and log)", Type: ftString, Col: 3, Row: 5, Width: 20,
			Get: func() string { return qc.BadAreaTag },
			Set: func(val string) error { qc.BadAreaTag = strings.TrimSpace(val); return nil },
		},
	}
}

// fileExists reports whether path names an existing file.
func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

// qwkConferenceNumbersFor lists the conference numbers already mirrored by
// message areas on the network.
func (m Model) qwkConferenceNumbersFor(netKey string) map[int]bool {
	out := make(map[int]bool)
	if m.configs == nil {
		return out
	}
	for _, a := range m.configs.MsgAreas {
		if a.IsQWKNet() && strings.EqualFold(a.Network, netKey) && a.QWKConference > 0 {
			out[a.QWKConference] = true
		}
	}
	return out
}
