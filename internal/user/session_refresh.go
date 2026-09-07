package user

import (
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/filelock"
)

// A session's user record is a copy, taken once when the caller authenticates
// and carried by value through the login sequence and the menu executor. That
// copy never changes for the rest of the call, so a sysop editing the account
// in ./ue while its owner is online has no effect on them until they hang up
// and dial back.
//
// It is not only an access-control problem, which is how it tends to be
// described. The whole record is frozen, so setting a message header style or
// a password mid-session is ignored just as thoroughly as a demotion.
//
// Note that adding users.json to the config watcher would not fix this. That
// would refresh the manager's map, and the session does not read from the map.
// The two are separate staleness problems and this is the second one.
//
// The fields taken from disk are exactly sysopOwnedFields — the ones ./ue can
// edit. Everything else stays session-owned, because the session is the newer
// authority for it: times called, time used, newscan pointers and tagged files
// have all moved on since the copy was taken, and pulling them from disk would
// throw away the current call's activity.

// diskSyncInterval bounds how often a refresh re-reads users.json.
//
// Refreshing at every menu means this is on the path of each keystroke that
// changes screen, and the check hashes the file. That is nothing for a small
// board and real work for a large one, so a sysop edit lands within a second
// rather than instantly. Nobody can tell the difference, and it stops the poll
// from scaling with the number of nodes.
const diskSyncInterval = time.Second

// refreshState tracks when users.json was last polled on the read path. It is
// separate from UserMgr's own mutex-guarded fields because it is written on
// what is otherwise a read, from many session goroutines at once.
type refreshState struct {
	mu       sync.Mutex
	lastSync time.Time
}

var sessionRefresh refreshState

// RefreshSessionUser returns the session's user record brought up to date with
// anything a sysop has changed on disk, leaving the session's own running
// state alone.
//
// It reports false when the account has gone — deleted in ./ue, or removed
// from users.json — which the caller should treat as grounds to end the call.
// Refusing to keep serving a deleted account is the point of checking: a sysop
// removing an abusive user while they are online expects them gone.
//
// The returned pointer is a new copy; current is never modified.
func (um *UserMgr) RefreshSessionUser(current *User) (*User, bool) {
	if current == nil {
		return nil, false
	}

	um.syncFromDisk()

	key := strings.ToLower(strings.TrimSpace(current.Handle))
	um.mu.RLock()
	stored := um.users[key]
	var snapshot User
	if stored != nil {
		snapshot = *stored
	}
	um.mu.RUnlock()

	if stored == nil || snapshot.DeletedUser {
		return nil, false
	}

	refreshed := *current
	sysopOwnedFields(&refreshed, &snapshot)
	// Carry the generation forward so a later UpdateUser with this copy is not
	// mistaken for one taken before the edit and made to re-merge over it.
	refreshed.gen = snapshot.gen
	return &refreshed, true
}

// syncFromDisk folds in any external edit to users.json, at most once per
// diskSyncInterval across the whole process.
//
// Without this the map itself stays stale until something triggers a save, so
// a refresh would keep handing back the same pre-edit values and appear to do
// nothing.
func (um *UserMgr) syncFromDisk() {
	sessionRefresh.mu.Lock()
	if time.Since(sessionRefresh.lastSync) < diskSyncInterval {
		sessionRefresh.mu.Unlock()
		return
	}
	sessionRefresh.lastSync = time.Now()
	sessionRefresh.mu.Unlock()

	// Take the cross-process lock so the merge cannot read a file that ./ue is
	// midway through replacing. On failure, read anyway: writes land by rename,
	// so the worst case is merging a version that is one save old, and the next
	// poll picks it up.
	lock, err := filelock.Acquire(um.path, filelock.DefaultTimeout)
	if err != nil {
		slog.Debug("refreshing users without the cross-process lock", "path", um.path, "error", err)
	}
	defer lock.Release() // nil-safe

	um.mu.Lock()
	defer um.mu.Unlock()
	if !um.externallyModified() {
		return
	}
	um.mergeExternalEdits()
	// Record what we just merged, so the next poll does not keep re-reading
	// the same file and rebuilding the map on every interval.
	um.fileState = fingerprintOf(um.path)
}
