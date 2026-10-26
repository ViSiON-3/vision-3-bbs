package user

// OfflineSignature is the signature added to replies that arrive with no one
// to ask, such as a QWK REP packet. A caller who wants to be asked before each
// message is signed gets none there; their offline reader can add its own.
func (u *User) OfflineSignature() string {
	if u.AutoSigAsk {
		return ""
	}
	return u.AutoSignature
}
