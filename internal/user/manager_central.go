package user

import "time"

// V3Net Central consent is owned by the stored record, as paired API
// devices are: only SetCentralConsent changes it. A session's copy of the
// user can be stale (another node may have changed the consent since the
// copy was taken), so every other save keeps the stored values rather than
// writing the copy's back. Otherwise an unrelated save on one node could
// silently restore sharing the caller withdrew on another.

// keepCentralConsent copies the consent fields of src into dst.
func keepCentralConsent(dst, src *User) {
	dst.CentralPresence = src.CentralPresence
	dst.CentralAvailability = src.CentralAvailability
	dst.CentralPresenceChosen = src.CentralPresenceChosen
	dst.CentralLastCallsChosen = src.CentralLastCallsChosen
	dst.CentralChat = src.CentralChat
	dst.CentralChatChosen = src.CentralChatChosen
	dst.CentralChatBarred = src.CentralChatBarred
	dst.CentralLastCallsSince = nil
	if src.CentralLastCallsSince != nil {
		t := *src.CentralLastCallsSince
		dst.CentralLastCallsSince = &t
	}
}

// SetCentralConsent changes the V3Net Central consent of the account with
// userID: fn is given a copy of the stored record and may change its
// consent fields, and only those are kept. It saves at once and returns a
// copy of the stored record as saved. On a failed save the stored consent
// is left as it was.
func (um *UserMgr) SetCentralConsent(userID int, fn func(u *User)) (*User, error) {
	um.mu.Lock()
	defer um.mu.Unlock()
	var target *User
	for _, u := range um.users {
		if u.ID == userID {
			target = u
			break
		}
	}
	if target == nil || target.DeletedUser {
		return nil, ErrUserNotFound
	}
	var previous User
	keepCentralConsent(&previous, target)
	scratch := *target
	keepCentralConsent(&scratch, target)
	fn(&scratch)
	if scratch.CentralLastCallsSince != nil {
		t := scratch.CentralLastCallsSince.UTC()
		scratch.CentralLastCallsSince = &t
	}
	keepCentralConsent(target, &scratch)
	if err := um.saveUsersLocked(); err != nil {
		// A save may merge an external edit first, replacing the records,
		// so look the account up again to undo.
		for _, u := range um.users {
			if u.ID == userID {
				keepCentralConsent(u, &previous)
				break
			}
		}
		return nil, err
	}
	for _, u := range um.users {
		if u.ID == userID {
			out := *u
			out.TwitList = append([]TwitEntry(nil), u.TwitList...)
			return &out, nil
		}
	}
	return nil, ErrUserNotFound
}

// CentralPresenceFor reports whether the caller is shown on network Who's
// Online on a network whose Callers Default switch is callersDefault: their
// own choice if they made one, else that default.
func (u *User) CentralPresenceFor(callersDefault bool) bool {
	if u.CentralPresence || u.CentralPresenceChosen {
		return u.CentralPresence
	}
	return callersDefault
}

// CentralLastCallsFrom reports whether the caller's calls are shown on
// network Last Callers on a network whose Callers Default switch is
// callersDefault, and from when. A caller who opted in shares the calls
// from when they did; one counted in by the default has no start, so their
// calls in the feed's 24 hours go.
func (u *User) CentralLastCallsFrom(callersDefault bool) (time.Time, bool) {
	if u.CentralLastCallsSince != nil {
		return *u.CentralLastCallsSince, true
	}
	if u.CentralLastCallsChosen || !callersDefault {
		return time.Time{}, false
	}
	return time.Time{}, true
}
