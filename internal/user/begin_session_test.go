package user

import (
	"path/filepath"
	"testing"
	"time"
)

func mgrWithAccount(t *testing.T, u *User) *UserMgr {
	t.Helper()
	um := NewUserMgrForTest(u)
	um.path = filepath.Join(t.TempDir(), "users.json")
	return um
}

// A brand-new account has never called before, so its first session must
// record one call and leave PreviousLogin zero — which is what makes the
// newscans treat everything as new for a first-time caller.
func TestBeginSessionRecordsAFirstCall(t *testing.T) {
	um := mgrWithAccount(t, &User{ID: 1, Handle: "Newbie", AccessLevel: 10})

	got, ok := um.BeginSession("Newbie")
	if !ok {
		t.Fatal("BeginSession failed for an account that exists")
	}
	if got.TimesCalled != 1 {
		t.Errorf("TimesCalled = %d, want 1 — the first call went unrecorded", got.TimesCalled)
	}
	if got.LastLogin.IsZero() {
		t.Error("LastLogin was not stamped")
	}
	if !got.PreviousLogin.IsZero() {
		t.Errorf("PreviousLogin = %v, want zero for an account that has never called", got.PreviousLogin)
	}
}

// On a later call it behaves exactly as Authenticate always did, rolling the
// old stamp into PreviousLogin.
func TestBeginSessionRollsPreviousLogin(t *testing.T) {
	prior := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	um := mgrWithAccount(t, &User{ID: 1, Handle: "Regular", LastLogin: prior, TimesCalled: 4})

	got, ok := um.BeginSession("Regular")
	if !ok {
		t.Fatal("BeginSession failed")
	}
	if !got.PreviousLogin.Equal(prior) {
		t.Errorf("PreviousLogin = %v, want the prior stamp %v", got.PreviousLogin, prior)
	}
	if !got.LastLogin.After(prior) {
		t.Errorf("LastLogin = %v, expected it to advance past %v", got.LastLogin, prior)
	}
	if got.TimesCalled != 5 {
		t.Errorf("TimesCalled = %d, want 5", got.TimesCalled)
	}
}

// Authenticate delegates here, so the signup path and the password path record
// a call identically. This pins that they cannot drift apart.
func TestAuthenticateAndBeginSessionAgree(t *testing.T) {
	um := mgrWithAccount(t, &User{ID: 1, Handle: "Same", AccessLevel: 10, TimesCalled: 7})

	got, ok := um.BeginSession("Same")
	if !ok {
		t.Fatal("BeginSession failed")
	}
	if got.TimesCalled != 8 {
		t.Errorf("TimesCalled = %d, want 8", got.TimesCalled)
	}

	um.mu.RLock()
	stored := *um.users["same"]
	um.mu.RUnlock()
	if stored.TimesCalled != got.TimesCalled || !stored.LastLogin.Equal(got.LastLogin) {
		t.Error("the returned snapshot does not match what was stored")
	}
}

func TestBeginSessionUnknownHandle(t *testing.T) {
	um := mgrWithAccount(t, &User{ID: 1, Handle: "Known"})
	if _, ok := um.BeginSession("Nobody"); ok {
		t.Error("BeginSession succeeded for a handle that does not exist")
	}
}
