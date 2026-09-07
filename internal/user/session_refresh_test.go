package user

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/filelock"
)

// refreshMgr builds a manager over a real users.json and clears the poll
// throttle, so a test does not have to wait out diskSyncInterval.
func refreshMgr(t *testing.T, u *User) *UserMgr {
	t.Helper()
	um := NewUserMgrForTest(u)
	um.path = filepath.Join(t.TempDir(), "users.json")
	if err := um.SaveUsers(); err != nil {
		t.Fatalf("seed SaveUsers: %v", err)
	}
	allowImmediateSync()
	return um
}

// allowImmediateSync resets the shared poll throttle between tests.
func allowImmediateSync() {
	sessionRefresh.mu.Lock()
	sessionRefresh.lastSync = time.Time{}
	sessionRefresh.mu.Unlock()
}

// editOnDisk rewrites users.json the way ./ue would, from a separate process.
func editOnDisk(t *testing.T, path string, mutate func(*User)) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var list []*User
	if err := json.Unmarshal(data, &list); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, u := range list {
		mutate(u)
	}
	out, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, out, 0600); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// The headline case: demote someone who is online and it takes effect on their
// session, rather than at their next login.
func TestRefreshPicksUpADemotion(t *testing.T) {
	um := refreshMgr(t, &User{ID: 1, Handle: "Felonius", AccessLevel: 255})
	session, ok := um.BeginSession("Felonius")
	if !ok {
		t.Fatal("BeginSession failed")
	}

	editOnDisk(t, um.path, func(u *User) { u.AccessLevel = 20 })
	allowImmediateSync()

	refreshed, stillValid := um.RefreshSessionUser(session)
	if !stillValid {
		t.Fatal("a demoted account should still be able to continue its call")
	}
	if refreshed.AccessLevel != 20 {
		t.Errorf("AccessLevel = %d, want 20 — the demotion did not reach the session",
			refreshed.AccessLevel)
	}
}

// The issue this came from is usually described in terms of access control,
// but the whole record is frozen. These two are what actually got hit.
func TestRefreshPicksUpNonAccessFields(t *testing.T) {
	um := refreshMgr(t, &User{ID: 1, Handle: "Felonius", MsgHdr: 1, PasswordHash: "old"})
	session, ok := um.BeginSession("Felonius")
	if !ok {
		t.Fatal("BeginSession failed")
	}

	editOnDisk(t, um.path, func(u *User) {
		u.MsgHdr = 4
		u.PasswordHash = "new"
	})
	allowImmediateSync()

	refreshed, ok := um.RefreshSessionUser(session)
	if !ok {
		t.Fatal("RefreshSessionUser reported the account gone")
	}
	if refreshed.MsgHdr != 4 {
		t.Errorf("MsgHdr = %d, want 4", refreshed.MsgHdr)
	}
	if refreshed.PasswordHash != "new" {
		t.Errorf("PasswordHash = %q, want %q", refreshed.PasswordHash, "new")
	}
}

// A refresh must not undo the current call's own bookkeeping. Those fields are
// session-owned and the copy in hand is the newer one.
func TestRefreshLeavesSessionOwnedStateAlone(t *testing.T) {
	um := refreshMgr(t, &User{ID: 1, Handle: "Felonius", AccessLevel: 255, TimesCalled: 5})
	session, ok := um.BeginSession("Felonius")
	if !ok {
		t.Fatal("BeginSession failed")
	}
	// Stand in for activity during the call.
	session.TimesCalled = 99
	session.NumDownloads = 42
	session.FilePoints = 7
	session.LastReadMessageIDs = map[int]string{7: "abc"}

	editOnDisk(t, um.path, func(u *User) {
		u.AccessLevel = 20
		u.TimesCalled = 1  // a sysop typing over the counter mid-call
		u.NumDownloads = 0 // and over a running total the session is advancing
	})
	allowImmediateSync()

	refreshed, ok := um.RefreshSessionUser(session)
	if !ok {
		t.Fatal("RefreshSessionUser reported the account gone")
	}
	if refreshed.AccessLevel != 20 {
		t.Errorf("AccessLevel = %d, want the on-disk 20", refreshed.AccessLevel)
	}
	if refreshed.TimesCalled != 99 {
		t.Errorf("TimesCalled = %d, want 99 — this call's activity was discarded", refreshed.TimesCalled)
	}
	if refreshed.NumDownloads != 42 {
		t.Errorf("NumDownloads = %d, want 42 — a running total was taken from disk", refreshed.NumDownloads)
	}
	if refreshed.FilePoints != 7 {
		t.Errorf("FilePoints = %d, want 7", refreshed.FilePoints)
	}
	if got := refreshed.LastReadMessageIDs[7]; got != "abc" {
		t.Errorf("newscan pointer = %q, want abc", got)
	}
}

// Deleting an account while its owner is online has to end the call; that is
// the whole point of a sysop reaching for delete on someone abusive.
func TestRefreshReportsADeletedAccountGone(t *testing.T) {
	um := refreshMgr(t, &User{ID: 1, Handle: "Felonius", AccessLevel: 255})
	session, ok := um.BeginSession("Felonius")
	if !ok {
		t.Fatal("BeginSession failed")
	}

	editOnDisk(t, um.path, func(u *User) { u.DeletedUser = true })
	allowImmediateSync()

	if _, stillValid := um.RefreshSessionUser(session); stillValid {
		t.Error("a deleted account was allowed to carry on its call")
	}
}

// Removing the record outright, rather than flagging it, must end the call too.
func TestRefreshReportsARemovedAccountGone(t *testing.T) {
	um := refreshMgr(t, &User{ID: 1, Handle: "Felonius", AccessLevel: 255})
	session, ok := um.BeginSession("Felonius")
	if !ok {
		t.Fatal("BeginSession failed")
	}

	if err := os.WriteFile(um.path, []byte("[]"), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}
	allowImmediateSync()

	if _, stillValid := um.RefreshSessionUser(session); stillValid {
		t.Error("a removed account was allowed to carry on its call")
	}
}

// The refresh returns a new record rather than writing through the caller's,
// so a session that ignores the result is not changed underneath itself.
func TestRefreshDoesNotMutateTheCallersCopy(t *testing.T) {
	um := refreshMgr(t, &User{ID: 1, Handle: "Felonius", AccessLevel: 255})
	session, ok := um.BeginSession("Felonius")
	if !ok {
		t.Fatal("BeginSession failed")
	}

	editOnDisk(t, um.path, func(u *User) { u.AccessLevel = 20 })
	allowImmediateSync()

	if _, ok := um.RefreshSessionUser(session); !ok {
		t.Fatal("RefreshSessionUser reported the account gone")
	}
	if session.AccessLevel != 255 {
		t.Errorf("the caller's own copy was modified: AccessLevel = %d, want 255", session.AccessLevel)
	}
}

// A refreshed copy must not be treated as pre-dating the edit it just picked
// up, or saving it would merge the on-disk values over itself again.
func TestRefreshedCopySavesWithoutReverting(t *testing.T) {
	um := refreshMgr(t, &User{ID: 1, Handle: "Felonius", AccessLevel: 255})
	session, ok := um.BeginSession("Felonius")
	if !ok {
		t.Fatal("BeginSession failed")
	}

	editOnDisk(t, um.path, func(u *User) { u.AccessLevel = 20 })
	allowImmediateSync()

	refreshed, ok := um.RefreshSessionUser(session)
	if !ok {
		t.Fatal("RefreshSessionUser reported the account gone")
	}
	// The session goes on to record something of its own.
	refreshed.TimesCalled = 77
	if err := um.UpdateUser(refreshed); err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}

	stored, ok := um.GetUser("Felonius")
	if !ok {
		t.Fatal("GetUser failed")
	}
	if stored.AccessLevel != 20 {
		t.Errorf("AccessLevel = %d, want 20 — saving the refreshed copy lost the edit", stored.AccessLevel)
	}
	if stored.TimesCalled != 77 {
		t.Errorf("TimesCalled = %d, want 77 — the session's own change was dropped", stored.TimesCalled)
	}
}

// Nil in, nothing out: the login phase has no session record yet.
func TestRefreshOnNilUser(t *testing.T) {
	um := refreshMgr(t, &User{ID: 1, Handle: "Felonius"})
	if _, ok := um.RefreshSessionUser(nil); ok {
		t.Error("RefreshSessionUser(nil) should report no valid user")
	}
}

// ./ue soft-deletes by setting the flag on disk rather than removing the
// record. That has to survive a BBS save, or a sysop removing an abusive user
// mid-call has it quietly undone by the next thing the BBS writes.
func TestExternalSoftDeleteSurvivesABbsSave(t *testing.T) {
	um := refreshMgr(t, &User{ID: 1, Handle: "Felonius", AccessLevel: 255})

	editOnDisk(t, um.path, func(u *User) {
		u.DeletedUser = true
		now := time.Now()
		u.DeletedAt = &now
	})

	// The BBS saves for any unrelated reason.
	if err := um.SaveUsers(); err != nil {
		t.Fatalf("SaveUsers: %v", err)
	}

	data, err := os.ReadFile(um.path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var final []*User
	if err := json.Unmarshal(data, &final); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(final) != 1 {
		t.Fatalf("expected 1 record, got %d", len(final))
	}
	if !final[0].DeletedUser {
		t.Error("the BBS reverted the editor's soft delete")
	}
	if final[0].DeletedAt == nil {
		t.Error("DeletedAt was dropped, so retention/purge would not know when it happened")
	}
}

// Renaming an account in ./ue re-keys the map, since the handle is the key.
// A handle-only lookup misses and the session would be dropped as though the
// account had been deleted -- so the session follows the rename instead.
func TestRefreshFollowsAnExternalRename(t *testing.T) {
	um := refreshMgr(t, &User{ID: 1, Handle: "Felonius", AccessLevel: 255})
	session, ok := um.BeginSession("Felonius")
	if !ok {
		t.Fatal("BeginSession failed")
	}

	editOnDisk(t, um.path, func(u *User) {
		u.Handle = "Renamed"
		u.AccessLevel = 20
	})
	allowImmediateSync()

	refreshed, stillValid := um.RefreshSessionUser(session)
	if !stillValid {
		t.Fatal("a rename dropped the call as though the account had been deleted")
	}
	if refreshed.Handle != "Renamed" {
		t.Errorf("Handle = %q, want Renamed — the session kept a name nothing answers to",
			refreshed.Handle)
	}
	if refreshed.AccessLevel != 20 {
		t.Errorf("AccessLevel = %d, want 20", refreshed.AccessLevel)
	}
	// And the renamed record must still be reachable for later saves.
	refreshed.TimesCalled = 33
	if err := um.UpdateUser(refreshed); err != nil {
		t.Fatalf("UpdateUser after a rename: %v", err)
	}
	stored, ok := um.GetUser("Renamed")
	if !ok {
		t.Fatal("the renamed account could not be looked up after a save")
	}
	if stored.TimesCalled != 33 {
		t.Errorf("TimesCalled = %d, want 33", stored.TimesCalled)
	}
}

// The ID fallback must not match a record just because both IDs are zero,
// which is what an unsaved or synthetic record looks like.
func TestRefreshDoesNotMatchOnAZeroID(t *testing.T) {
	um := refreshMgr(t, &User{ID: 0, Handle: "Ghost"})

	orphan := &User{ID: 0, Handle: "NoSuchAccount"}
	if _, ok := um.RefreshSessionUser(orphan); ok {
		t.Error("a zero ID matched an unrelated record")
	}
}

// Concurrent refreshes must be safe: this runs on every menu change on every
// node at once.
func TestRefreshIsSafeUnderConcurrency(t *testing.T) {
	um := refreshMgr(t, &User{ID: 1, Handle: "Felonius", AccessLevel: 255})
	session, ok := um.BeginSession("Felonius")
	if !ok {
		t.Fatal("BeginSession failed")
	}

	done := make(chan struct{})
	for i := range 8 {
		go func(n int) {
			defer func() { done <- struct{}{} }()
			for range 25 {
				allowImmediateSync()
				if _, ok := um.RefreshSessionUser(session); !ok {
					t.Errorf("goroutine %d saw the account vanish", n)
					return
				}
			}
		}(i)
	}
	for range 8 {
		<-done
	}
}

// The refresh runs on the menu path, so it must not wait on the cross-process
// lock. Waiting the save path's timeout would stall a caller's screen behind an
// editor that happens to be mid-save -- a stall observed live at five seconds
// before this was made a single attempt.
func TestRefreshDoesNotWaitOnAHeldLock(t *testing.T) {
	um := refreshMgr(t, &User{ID: 1, Handle: "Felonius", AccessLevel: 255})
	session, ok := um.BeginSession("Felonius")
	if !ok {
		t.Fatal("BeginSession failed")
	}

	// Stand in for ./ue holding the lock across its own save.
	lock, err := filelock.Acquire(um.path, time.Second)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer lock.Release()

	allowImmediateSync()
	start := time.Now()
	if _, ok := um.RefreshSessionUser(session); !ok {
		t.Fatal("RefreshSessionUser reported the account gone")
	}
	waited := time.Since(start)

	// Generous bound: the point is that it does not sit out a multi-second
	// timeout, not that it finishes in any particular number of milliseconds.
	if waited > time.Second {
		t.Errorf("refresh blocked %s on a held lock; it must not wait on the menu path", waited)
	}
}
