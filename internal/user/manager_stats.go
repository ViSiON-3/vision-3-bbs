package user

import (
	"log/slog"
)

// GetAllUsers returns a slice containing copies of all user records.
// Returns copies to prevent callers from mutating internal state.
func (um *UserMgr) GetAllUsers() []*User {
	um.mu.RLock()
	defer um.mu.RUnlock()

	usersSlice := make([]*User, 0, len(um.users))
	for _, user := range um.users {
		userCopy := *user
		usersSlice = append(usersSlice, &userCopy)
	}
	return usersSlice
}

// GetUserCount returns the total number of registered users.
func (um *UserMgr) GetUserCount() int {
	um.mu.RLock()
	defer um.mu.RUnlock()
	return len(um.users)
}

// GetTotalCalls returns the total number of calls (logins) recorded.
func (um *UserMgr) GetTotalCalls() uint64 {
	um.mu.RLock()
	defer um.mu.RUnlock()
	if um.nextCallNumber <= 1 {
		return 0
	}
	return um.nextCallNumber - 1
}

// MarkUserOnline marks a user as currently online/connected
func (um *UserMgr) MarkUserOnline(userID int) {
	um.mu.Lock()
	defer um.mu.Unlock()
	um.activeUserIDs[int32(userID)] = true
	slog.Debug("user marked online", "id", userID)
}

// MarkUserOffline marks a user as offline/disconnected
func (um *UserMgr) MarkUserOffline(userID int) {
	um.mu.Lock()
	defer um.mu.Unlock()
	delete(um.activeUserIDs, int32(userID))
	slog.Debug("user marked offline", "id", userID)
}

// IsUserOnline returns true if the user is currently connected
func (um *UserMgr) IsUserOnline(userID int) bool {
	um.mu.RLock()
	defer um.mu.RUnlock()
	return um.activeUserIDs[int32(userID)]
}
