package message

import (
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/jam"
)

// VisibleTo reports whether the user with the given handle and account ID
// may read m. A public message is visible to everyone. A private message is
// visible only to its recipient and its sender (see recipientIs, senderIs).
// A blank handle never matches, so an unknown user sees no private mail.
//
// The account ID decides when both the message and the caller have one
// (#1048): a message records its sender's and recipient's IDs when the BBS
// writes it, and user IDs are never reused, so an account keeps its mail
// through a rename and a new account given an old handle gets none of it.
// Otherwise (mail from before IDs, from other boards, or a caller that only
// knows a handle, id 0) the handle decides, ignoring case and surrounding
// space. Real names are never trusted as an identity: two accounts can share
// one, and users change theirs.
//
// Every path that shows message content to a user (the reader, the message
// list, newscan, QWK export, the script API) applies this rule on top of its
// own area access checks; area ACS alone does not protect private mail.
func (m *DisplayMessage) VisibleTo(handle string, id int) bool {
	if !m.IsPrivate {
		return true
	}
	return m.recipientIs(handle, id) || m.senderIs(handle, id)
}

// recipientIs reports whether the account (handle, id) is m's recipient:
// by RecipientID when m and the caller both have an ID, else by To.
func (m *DisplayMessage) recipientIs(handle string, id int) bool {
	return jam.AddressedTo(m.To, m.RecipientID, handle, id)
}

// senderIs reports whether the account (handle, id) sent m: by SenderID
// when m and the caller both have an ID, else by the sender's handle.
func (m *DisplayMessage) senderIs(handle string, id int) bool {
	handle = strings.TrimSpace(handle)
	if handle == "" {
		return false
	}
	if m.SenderID > 0 && id > 0 {
		return id == m.SenderID
	}
	return strings.EqualFold(m.sender(), handle)
}

// WrittenBy reports whether the account (handle, id) wrote m, public or
// private, by the same rule as the sender of private mail.
func (m *DisplayMessage) WrittenBy(handle string, id int) bool { return m.senderIs(handle, id) }

// sender is the handle that sent m: the SenderHandle a local post signed
// with a real name records, else From. A post signed with a real name names
// its sender by SenderHandle; its From is then only a display name, and must
// not match an account whose handle happens to equal it.
func (m *DisplayMessage) sender() string {
	if s := strings.TrimSpace(m.SenderHandle); s != "" {
		return s
	}
	return strings.TrimSpace(m.From)
}

// SentBy reports whether m is private mail the account (handle, id) sent,
// by the rule VisibleTo uses for the sender.
func (m *DisplayMessage) SentBy(handle string, id int) bool {
	return m.IsPrivate && m.senderIs(handle, id)
}

// MailFor reports whether m is private mail addressed to the account
// (handle, id): its recipient, who may delete it. The sender, whom
// VisibleTo also lets read it, is not.
func (m *DisplayMessage) MailFor(handle string, id int) bool {
	return m.IsPrivate && m.recipientIs(handle, id)
}
