package user

import "strings"

// ResolveRecipient maps a name as it appears on a piece of mail to the local
// account it addresses, so private mail can be stored and matched by handle
// (the only name that identifies an account; see message.DisplayMessage.VisibleTo).
//
// name may be a handle, a real name or the conventional "Sysop". It resolves,
// in order and ignoring case and surrounding space:
//
//  1. a user whose handle is name;
//  2. user #1 when name is "Sysop" (the FidoNet convention for the board's
//     operator);
//  3. the one user whose real name is name. When two or more accounts share
//     that real name the result is ambiguous and nothing is returned, since
//     choosing either could deliver one user's mail to the other.
//
// Deleted accounts are never returned. The result is a copy the caller may
// keep. ok is false when name resolves to no account, and always on a nil
// UserMgr, so an importer running without the users file resolves nothing.
//
// Resolution is for addressing mail when it is written (imports, replies,
// migration). Readers must still compare handles only: a real name can be
// changed at any time, so it must never grant access at read time.
func (um *UserMgr) ResolveRecipient(name string) (*User, bool) {
	name = strings.TrimSpace(name)
	if um == nil || name == "" {
		return nil, false
	}
	um.mu.RLock()
	defer um.mu.RUnlock()

	if u, ok := um.users[strings.ToLower(name)]; ok {
		if u.DeletedUser {
			return nil, false
		}
		c := *u
		return &c, true
	}
	if strings.EqualFold(name, "sysop") {
		for _, u := range um.users {
			if u.ID == 1 && !u.DeletedUser {
				c := *u
				return &c, true
			}
		}
		return nil, false
	}
	var match *User
	for _, u := range um.users {
		if u.DeletedUser || !strings.EqualFold(strings.TrimSpace(u.RealName), name) {
			continue
		}
		if match != nil {
			return nil, false // ambiguous
		}
		match = u
	}
	if match == nil {
		return nil, false
	}
	c := *match
	return &c, true
}

// HandleExists reports whether handle belongs to an account, deleted or not.
// Private mail addressed to a handle that no account holds is undeliverable
// (see ResolveRecipient); deleted accounts still own their mail.
func (um *UserMgr) HandleExists(handle string) bool {
	handle = strings.TrimSpace(handle)
	if um == nil || handle == "" {
		return false
	}
	um.mu.RLock()
	defer um.mu.RUnlock()
	_, ok := um.users[strings.ToLower(handle)]
	return ok
}

// RecipientResolver is the part of UserMgr that writes private mail: mail
// importers take one so tests can supply a fixed set of accounts, and so a
// caller without the users file can pass nil and resolve nothing.
type RecipientResolver interface {
	ResolveRecipient(name string) (*User, bool)
}

// AddressByHandle returns the handle of the account to resolves to through r
// (see UserMgr.ResolveRecipient), or to unchanged when r is nil or to names no
// single account. Mail importers store the result as a private message's To so
// the recipient, who is matched by handle alone, can read it.
func AddressByHandle(r RecipientResolver, to string) string {
	if r == nil {
		return to
	}
	if u, ok := r.ResolveRecipient(to); ok && u != nil {
		return u.Handle
	}
	return to
}
