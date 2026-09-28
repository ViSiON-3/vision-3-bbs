package message

import "strings"

// VisibleTo reports whether the user with the given handle may read m. A
// public message is visible to everyone. A private message is visible only to
// its recipient and its sender: the To or From field must equal handle,
// ignoring case and surrounding space. A blank handle never matches, so an
// unknown user sees no private mail.
//
// Only the handle is trusted as an identity. Handles are unique and fixed;
// real names are neither (users can change theirs, and two accounts can share
// one), so matching them would let a user read mail meant for someone else by
// adopting that person's real name. Private mail must therefore be addressed
// by handle.
//
// Every path that shows message content to a user (the reader, the message
// list, newscan, QWK export, the script API) applies this rule on top of its
// own area access checks; area ACS alone does not protect private mail.
func (m *DisplayMessage) VisibleTo(handle string) bool {
	if !m.IsPrivate {
		return true
	}
	handle = strings.TrimSpace(handle)
	if handle == "" {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(m.To), handle) ||
		strings.EqualFold(strings.TrimSpace(m.From), handle)
}
