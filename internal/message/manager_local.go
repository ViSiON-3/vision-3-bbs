package message

import "strings"

// IsNetworked reports whether area's messages are shared with other boards:
// its type is anything but local (echomail, netmail, QWK, V3Net), or a V3Net
// leaf binds it by tag whatever its type (isV3NetArea). A post there goes out
// to the network, and changing or removing a message changes only this
// board's copy.
func (mm *MessageManager) IsNetworked(area *MessageArea) bool {
	if area == nil {
		return false
	}
	if t := strings.ToLower(strings.TrimSpace(area.AreaType)); t != "" && t != "local" {
		return true
	}
	return mm.isV3NetArea(area)
}

// StoredAsUTF8 reports whether m's text is stored as UTF-8, or names no
// character set, so it can be edited or reposted as it is. The reader
// decodes other character sets for display only.
func (m *DisplayMessage) StoredAsUTF8() bool {
	cs := strings.ToUpper(strings.TrimSpace(m.Charset))
	return cs == "" || cs == "UTF-8" || cs == "UTF8"
}
