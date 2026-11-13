package message

import (
	"strings"
	"unicode/utf8"
)

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

// StoredAsUTF8 reports whether m's text is stored as UTF-8, so it can be
// edited or reposted as it is: its CHRS kludge says UTF-8 or names no
// character set, and the text is valid UTF-8 (unlabelled text from an older
// program can be raw CP437, and a label can be wrong). The reader decodes other character sets for
// display only.
func (m *DisplayMessage) StoredAsUTF8() bool {
	switch strings.ToUpper(strings.TrimSpace(m.Charset)) {
	case "", "UTF-8", "UTF8":
		return utf8.ValidString(m.Body)
	}
	return false
}
