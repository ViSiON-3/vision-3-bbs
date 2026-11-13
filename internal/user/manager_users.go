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
	return um.updateUserByID(u, nil)
}

// UpdateUserByIDAndChatBar is UpdateUserByID that also sets the account's
// network chat bar (CentralChatBarred), in the same save. The bar is one of
// the consent fields UpdateUserByID keeps as stored (see keepCentralConsent
// in manager_central.go), and the sysop's user editor is the one place that
// changes it, along with whatever else it saves: one save, so a deletion
// and a bar staged together land together or not at all.
func (um *UserMgr) UpdateUserByIDAndChatBar(u *User, barred bool) error {
	return um.updateUserByID(u, &barred)
}

// updateUserByID is UpdateUserByID, setting the chat bar too when chatBar
// is not nil.
func (um *UserMgr) updateUserByID(u *User, chatBar *bool) error {
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
	var current *User
	for k, existing := range um.users {
		if existing.ID == u.ID {
			oldKey = k
			current = existing
			break
		}
	}
	if oldKey == "" {
		return ErrUserNotFound
	}
	// Other settings on an old account save as usual; only a rename is checked.
	if err := ValidateHandleEdit(current.Handle, u.Handle); err != nil {
		return err
	}

	newKey := strings.ToLower(u.Handle)
	if newKey != oldKey {
		// Handle changed — ensure no collision with a different user.
		if existing, exists := um.users[newKey]; exists && existing.ID != u.ID {
			return ErrHandleExists
		}
		delete(um.users, oldKey)
	}

	userCopy := *u
	userCopy.TwitList = append([]TwitEntry(nil), u.TwitList...)
	// Same guard as UpdateUser: a copy taken before an external edit must not
	// carry the pre-edit sysop fields back over what ./ue wrote.
	if current != nil && current.gen > userCopy.gen {
		sysopOwnedFields(&userCopy, current)
		userCopy.gen = current.gen
	}
	// Devices are changed only through the device methods; see UpdateUser.
	if current != nil {
		userCopy.APIDevices = current.APIDevices
		keepCentralConsent(&userCopy, current) // see manager_central.go
	}
	if chatBar != nil {
		userCopy.CentralChatBarred = *chatBar
	}
	// This is the sysop's editor (or a script saving its own user), and what
	// it stores is now the record's sysop-owned state, just as an edit in
	// ./ue is. Moving the generation on makes every copy taken before this
	// save, such as the target's own session halfway through composing a
	// message, look stale to UpdateUser, so it can't write the old level or
	// roles back over the change.
	userCopy.gen = max(userCopy.gen, current.gen) + 1
	um.users[newKey] = &userCopy
	if err := um.saveUsersLocked(); err != nil {
		// Roll the map back to what is still on disk: the entry found above
		// returns under its old key, whether or not the handle changed, so
		// the account is not missing from the next save that succeeds.
		delete(um.users, newKey)
		um.users[oldKey] = current
		return err
	}
	// The caller's copy becomes the one just stored, so a second save from
	// the same editor isn't mistaken for a stale one. Its sysop-owned
	// fields are taken back too: when it was stale, those came from the
	// stored record, and a current generation on the old values would let
	// a later UpdateUser write them back.
	sysopOwnedFields(u, &userCopy)
	u.gen = userCopy.gen
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
	userCopy.TwitList = append([]TwitEntry(nil), u.TwitList...)
	previous := um.users[lowerHandle]
	// The caller is writing a copy it took earlier in the session. If the
	// record has been refreshed from an external edit since then (./ue
	// changing a level or validating an account), that copy still carries the
	// pre-edit values, and storing it wholesale would revert the sysop.
	// Keep the caller's session state, restore the sysop's fields.
	if previous != nil && previous.gen > userCopy.gen {
		sysopOwnedFields(&userCopy, previous)
		userCopy.gen = previous.gen
	}
	// Paired API devices are changed only through the device methods, which
	// work on the stored record. A copy taken before a pairing (say, by a
	// terminal session that is still on) must not drop the new device when
	// it is saved, so the stored list always wins.
	if previous != nil {
		userCopy.APIDevices = previous.APIDevices
		// Central consent too: only SetCentralConsent changes it.
		keepCentralConsent(&userCopy, previous)
	}
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
	userCopy.TwitList = append([]TwitEntry(nil), user.TwitList...)
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
			userCopy.TwitList = append([]TwitEntry(nil), user.TwitList...)
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
	if strings.Contains(handle, "|") || strings.Contains(realName, "|") {
		return nil, ErrNamePipe
	}
	if strings.HasPrefix(handle, "@") {
		return nil, ErrHandleGroupPrefix
	}
	lowerHandle := strings.ToLower(handle)

	// Reject a duplicate handle before doing the expensive work, so the common
	// error path is unchanged and costs nothing.
	um.mu.RLock()
	_, exists := um.users[lowerHandle]
	um.mu.RUnlock()
	if exists {
		return nil, ErrHandleExists
	}

	// Hash OUTSIDE the lock. bcrypt is deliberately slow, and it depends only on
	// password -- holding um.mu across it blocks every read and write on the
	// manager for the duration of each registration. Authenticate already
	// releases the lock before bcrypt for exactly this reason.
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("failed to hash password: %w", err)
	}

	um.mu.Lock()
	defer um.mu.Unlock()

	// Re-check under the write lock: another registration may have taken the
	// handle while we were hashing.
	if _, exists := um.users[lowerHandle]; exists {
		return nil, ErrHandleExists
	}

	// The mark goes past this ID before the account is saved: if it can't,
	// there is no account, rather than one whose ID a later purge and
	// restart could hand out again.
	if um.idMarkErr != nil {
		return nil, fmt.Errorf("%w: %v", ErrUserIDMark, um.idMarkErr)
	}
	if err := raiseNextUserID(um.path, um.nextUserID+1); err != nil {
		return nil, fmt.Errorf("storing the next user ID: %w", err)
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
		Validated:     um.autoValidate,
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

// SetAutoValidateNewUsers controls whether accounts created by AddUser are
// marked validated straight away. Called after loading the server config, and
// again when it is reloaded.
//
// This does not decide whether a new caller can log in — that is AccessLevel
// against LogonLevel. It sets the reviewed flag, which suppresses the
// required-infoforms prompt and clears the account from the pending list.
func (um *UserMgr) SetAutoValidateNewUsers(auto bool) {
	um.mu.Lock()
	defer um.mu.Unlock()
	um.autoValidate = auto
}
