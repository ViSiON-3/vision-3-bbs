package user

import (
	"fmt"
	"log/slog"
	"time"
)

// PurgeResult holds information about a permanently purged user for reporting.
type PurgeResult struct {
	ID        int
	Handle    string
	DeletedAt time.Time
}

// PurgeDeletedUsers permanently removes soft-deleted users whose DeletedAt timestamp
// is older than retentionDays days. Pass retentionDays=-1 to skip (no-op).
// Returns a slice of PurgeResult describing the removed accounts.
// The caller is responsible for logging admin activity if desired.
func (um *UserMgr) PurgeDeletedUsers(retentionDays int) ([]PurgeResult, error) {
	if retentionDays < 0 {
		return nil, nil
	}

	cutoff := time.Now().AddDate(0, 0, -retentionDays)

	um.mu.Lock()
	defer um.mu.Unlock()

	// Phase 1: identify eligible users without modifying the map yet.
	type candidate struct {
		key    string
		user   *User
		result PurgeResult
	}
	var candidates []candidate
	for key, u := range um.users {
		if !u.DeletedUser {
			continue
		}
		if u.DeletedAt == nil {
			// Deleted but no timestamp: treat as immediately eligible.
			candidates = append(candidates, candidate{
				key:  key,
				user: u,
				result: PurgeResult{
					ID:     u.ID,
					Handle: u.Handle,
				},
			})
		} else if u.DeletedAt.Before(cutoff) {
			candidates = append(candidates, candidate{
				key:  key,
				user: u,
				result: PurgeResult{
					ID:        u.ID,
					Handle:    u.Handle,
					DeletedAt: *u.DeletedAt,
				},
			})
		}
	}

	if len(candidates) == 0 {
		return nil, nil
	}

	// The mark goes past every ID before any account goes, so none of
	// theirs can be given out again (#1020). If it can't be stored, nothing
	// is purged.
	maxID := 0
	for _, u := range um.users {
		maxID = max(maxID, u.ID)
	}
	if err := raiseNextUserID(um.path, max(um.nextUserID, maxID+1)); err != nil {
		return nil, fmt.Errorf("storing the next user ID before purging: %w", err)
	}
	// Their handles are retired first too: private mail is matched by
	// handle, so a new account with one would read the purged account's
	// mail (#1048). If they can't be recorded, nothing is purged.
	handles := make([]string, 0, len(candidates))
	for _, c := range candidates {
		handles = append(handles, c.user.Handle)
	}
	if err := RetireHandles(um.path, handles...); err != nil {
		return nil, fmt.Errorf("retiring purged handles: %w", err)
	}

	// Phase 2: remove from in-memory store, then persist.
	// Roll back in-memory changes if save fails so the store stays consistent.
	for _, c := range candidates {
		delete(um.users, c.key)
	}
	if err := um.saveUsersLocked(); err != nil {
		for _, c := range candidates {
			um.users[c.key] = c.user
		}
		return nil, fmt.Errorf("purge: failed to save users: %w", err)
	}

	purged := make([]PurgeResult, len(candidates))
	for i, c := range candidates {
		purged[i] = c.result
	}
	slog.Info("purged soft-deleted user accounts", "count", len(purged))
	return purged, nil
}
