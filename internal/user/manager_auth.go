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

	// Authentication successful - update LastLogin and TimesCalled
	um.mu.Lock()
	user = um.users[lowerHandle] // Re-fetch under write lock
	if user == nil {
		um.mu.Unlock()
		return nil, false
	}
	user.LastLogin = time.Now()
	user.TimesCalled++
	um.mu.Unlock()

	// Save outside the write lock to avoid blocking other user operations
	if err := um.SaveUsers(); err != nil {
		slog.Error("failed to save user data after login", "handle", handle, "error", err)
	}

	// Return a copy
	um.mu.RLock()
	userCopy := *um.users[lowerHandle]
	um.mu.RUnlock()
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
