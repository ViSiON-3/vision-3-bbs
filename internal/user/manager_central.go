package user

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
