package user

import (
	"bytes"
	"log/slog"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	"golang.org/x/crypto/ssh"
)

// Authenticate checks handle and compares password hash.
// Handle lookup is case-insensitive.
// Returns: (user, success)
func (um *UserMgr) Authenticate(handle, password string) (*User, bool) {
	lowerHandle := strings.ToLower(handle)

	um.mu.RLock()
	user, exists := um.users[lowerHandle]
	if !exists {
		um.mu.RUnlock()
		return nil, false
	}
	// Deny login if user is deleted
	if user.DeletedUser {
		um.mu.RUnlock()
		return nil, false
	}
	// Copy the password hash while holding the read lock
	passwordHash := user.PasswordHash
	um.mu.RUnlock()

	// Compare hashed password outside any lock (bcrypt is CPU-intensive)
	err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(password))
	if err != nil {
		return nil, false
	}

	// Password verified; record the call.
	return um.BeginSession(handle)
}

// BeginSession stamps the login bookkeeping for a handle and returns a
// snapshot of the record, without checking any credential.
//
// Authenticate calls this once the password verifies. The signup flow calls it
// directly for an account it has just created, which has no password to
// re-check — the caller set it moments ago. Both paths must record a call
// identically, so the bookkeeping lives here rather than being written twice.
func (um *UserMgr) BeginSession(handle string) (*User, bool) {
	lowerHandle := strings.ToLower(handle)

	um.mu.Lock()
	user := um.users[lowerHandle]
	if user == nil {
		um.mu.Unlock()
		return nil, false
	}
	// Preserve the prior login stamp before overwriting it; "new since last
	// login" checks during the login sequence need the previous visit, not
	// this one. A brand-new account has a zero LastLogin, so its PreviousLogin
	// stays zero and everything reads as new — which is correct for a first
	// call.
	user.PreviousLogin = user.LastLogin
	user.LastLogin = time.Now()
	user.TimesCalled++
	// Snapshot under the same lock that set the fields. Re-reading after the
	// unlock would let a concurrent login for this handle overwrite
	// PreviousLogin first, and this session would then measure "new since your
	// last visit" against the other session's timestamp — hiding news, rumors
	// and files posted since this caller was actually last on.
	userCopy := *user
	um.mu.Unlock()

	// Save outside the write lock to avoid blocking other user operations
	if err := um.SaveUsers(); err != nil {
		slog.Error("failed to save user data after login", "handle", handle, "error", err)
	}

	return &userCopy, true
}

// FindByAuthorizedKey returns the user whose registered PublicKeys include a
// key matching the given marshaled wire bytes (ssh.PublicKey.Marshal()).
// Matching is by exact key bytes; access-level authorization is enforced by
// the caller, not here.
func (um *UserMgr) FindByAuthorizedKey(marshaled []byte) (*User, bool) {
	um.mu.RLock()
	defer um.mu.RUnlock()
	for _, u := range um.users { // um.users is map[string]*User
		// A soft-deleted account must not authenticate. The SSH-key path is the
		// one login route that never re-checks this: callers only verify access
		// level, so without this guard a removed user keeps their key access.
		if u.DeletedUser {
			continue
		}
		for _, line := range u.PublicKeys {
			pub, _, _, _, err := ssh.ParseAuthorizedKey([]byte(line))
			if err != nil {
				continue
			}
			if bytes.Equal(pub.Marshal(), marshaled) {
				// Return a copy, as GetUser/GetUserByID/Authenticate do. Handing
				// back the map's own pointer lets the caller read it after the
				// lock is released, while Authenticate mutates LastLogin and
				// TimesCalled on that same struct in place -- a live data race.
				userCopy := *u
				return &userCopy, true
			}
		}
	}
	return nil, false
}
