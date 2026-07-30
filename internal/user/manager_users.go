package user

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// UpdateUserByID updates a user looked up by their stable ID, safely re-keying
// the internal map when the handle has changed. Use this in any flow that may
// rename a user's handle (e.g., admin editor); for all other updates prefer
// UpdateUser.
func (um *UserMgr) UpdateUserByID(u *User) error {
	if u == nil {
		return fmt.Errorf("cannot update nil user")
	}
	u.Handle = strings.TrimSpace(u.Handle)
	if u.Handle == "" {
		return fmt.Errorf("handle cannot be blank")
	}
	um.mu.Lock()
	defer um.mu.Unlock()

	// Locate existing map entry by stable ID.
	var oldKey string
	for k, existing := range um.users {
		if existing.ID == u.ID {
			oldKey = k
			break
		}
	}
	if oldKey == "" {
		return ErrUserNotFound
	}

	newKey := strings.ToLower(u.Handle)
	var originalEntry *User
	rekeyed := false
	if newKey != oldKey {
		// Handle changed — ensure no collision with a different user.
		if existing, exists := um.users[newKey]; exists && existing.ID != u.ID {
			return ErrHandleExists
		}
		originalEntry = um.users[oldKey] // save for rollback
		delete(um.users, oldKey)
		rekeyed = true
	}

	userCopy := *u
	um.users[newKey] = &userCopy
	if err := um.saveUsersLocked(); err != nil {
		// Rollback in-memory map to match what is still on disk.
		delete(um.users, newKey)
		if rekeyed {
			um.users[oldKey] = originalEntry
		}
		return err
	}
	return nil
}

// UpdateUser copies the modified user back into the internal map and saves to disk.
// Use this instead of SaveUsers when you have modified a user copy obtained from
// GetUser or Authenticate and need those changes persisted.
func (um *UserMgr) UpdateUser(u *User) error {
	if u == nil {
		return fmt.Errorf("cannot update nil user")
	}
	um.mu.Lock()
	defer um.mu.Unlock()
	lowerHandle := strings.ToLower(u.Handle)
	if _, exists := um.users[lowerHandle]; !exists {
		return ErrUserNotFound
	}
	// Create a defensive copy to prevent external mutations from bypassing locks
	userCopy := *u
	previous := um.users[lowerHandle]
	um.users[lowerHandle] = &userCopy
	if err := um.saveUsersLocked(); err != nil {
		// Restore the previous entry so the cache never serves a value that
		// failed to reach disk, matching AddUser and UpdateUserByID.
		um.users[lowerHandle] = previous
		return err
	}
	return nil
}

// GetUser retrieves a user by handle (case-insensitive).
// Returns a copy to prevent callers from mutating internal state without the lock.
func (um *UserMgr) GetUser(handle string) (*User, bool) {
	um.mu.RLock()
	defer um.mu.RUnlock()

	user, exists := um.users[strings.ToLower(handle)]
	if !exists {
		return nil, false
	}
	userCopy := *user
	return &userCopy, true
}

// GetUserByID returns a user by their ID (for optimistic locking checks)
func (um *UserMgr) GetUserByID(id int) (*User, bool) {
	um.mu.RLock()
	defer um.mu.RUnlock()

	for _, user := range um.users {
		if user.ID == id {
			// Return a copy to prevent modification of the internal user data
			userCopy := *user
			return &userCopy, true
		}
	}
	return nil, false
}

// NextUserID returns the ID that will be assigned to the next new user.
func (um *UserMgr) NextUserID() int {
	um.mu.RLock()
	defer um.mu.RUnlock()
	return um.nextUserID
}

// AddUser creates a new user, hashes the password, assigns an ID, and saves.
func (um *UserMgr) AddUser(password, handle, realName, groupLocation string) (*User, error) {
	handle = strings.TrimSpace(handle)
	if handle == "" {
		return nil, fmt.Errorf("handle cannot be blank")
	}
	lowerHandle := strings.ToLower(handle)

	um.mu.Lock()
	defer um.mu.Unlock()

	// Check if handle already exists
	if _, exists := um.users[lowerHandle]; exists {
		return nil, ErrHandleExists
	}

	// Hash the password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("failed to hash password: %w", err)
	}

	// Create new user
	newUser := &User{
		ID:            um.nextUserID,
		PasswordHash:  string(hashedPassword),
		Handle:        handle,
		RealName:      realName,
		GroupLocation: groupLocation,
		AccessLevel:   um.newUserLevel,
		TimeLimit:     60,
		Validated:     false,
		LastLogin:     time.Time{},
	}

	// Add to map and increment nextUserID
	um.users[lowerHandle] = newUser
	um.nextUserID++

	// Save the updated user list *while still holding the lock*
	if err := um.saveUsersLocked(); err != nil {
		slog.Error("failed to save users after adding user", "handle", handle, "error", err)
		delete(um.users, lowerHandle)
		um.nextUserID--
		return nil, err
	}

	slog.Info("added user", "handle", newUser.Handle, "id", newUser.ID)
	return newUser, nil
}

// SetNewUserLevel sets the access level assigned to new user signups.
// This should be called after loading the server config.
// Level is clamped to the valid range of 0-255.
func (um *UserMgr) SetNewUserLevel(level int) {
	um.mu.Lock()
	defer um.mu.Unlock()

	// Validate and clamp to 0-255 range
	if level < 0 {
		slog.Warn("invalid newUserLevel; clamping to 0", "level", level)
		level = 0
	} else if level > 255 {
		slog.Warn("invalid newUserLevel; clamping to 255", "level", level)
		level = 255
	}

	um.newUserLevel = level
}
