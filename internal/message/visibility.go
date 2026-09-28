package message

import "strings"

// VisibleTo reports whether the user identified by handle and realName may
// read m. A public message is visible to everyone. A private message is
// visible only to its recipient and its sender, matched case-insensitively
// against either name, because real-name-only areas sign and address mail by
// real name rather than handle. Blank names never match, so an unknown user
// sees no private mail.
//
// Every path that shows message content to a user (the reader, the message
// list, newscan, QWK export, the script API) applies this rule on top of its
// own area access checks; area ACS alone does not protect private mail.
func (m *DisplayMessage) VisibleTo(handle, realName string) bool {
	if !m.IsPrivate {
		return true
	}
	return nameMatches(m.To, handle, realName) || nameMatches(m.From, handle, realName)
}

// nameMatches reports whether field equals either non-blank name, ignoring
// case and surrounding space.
func nameMatches(field, handle, realName string) bool {
	field = strings.TrimSpace(field)
	if field == "" {
		return false
	}
	for _, n := range []string{handle, realName} {
		if n = strings.TrimSpace(n); n != "" && strings.EqualFold(field, n) {
			return true
		}
	}
	return false
}
