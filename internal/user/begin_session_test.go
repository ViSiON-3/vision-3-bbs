package user

import (
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
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
// a call identically. The point of extracting rather than duplicating was that
// they cannot drift, so this actually drives both and compares.
func TestAuthenticateAndBeginSessionAgree(t *testing.T) {
	const pw = "pw123456"
	hash, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("bcrypt: %v", err)
	}
	prior := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

	viaAuth := mgrWithAccount(t, &User{
		ID: 1, Handle: "Same", PasswordHash: string(hash), LastLogin: prior, TimesCalled: 7,
	})
	viaBegin := mgrWithAccount(t, &User{
		ID: 1, Handle: "Same", PasswordHash: string(hash), LastLogin: prior, TimesCalled: 7,
	})

	a, ok := viaAuth.Authenticate("Same", pw)
	if !ok {
		t.Fatal("Authenticate failed")
	}
	b, ok := viaBegin.BeginSession("Same")
	if !ok {
		t.Fatal("BeginSession failed")
	}

	if a.TimesCalled != b.TimesCalled {
		t.Errorf("TimesCalled differs: Authenticate=%d BeginSession=%d", a.TimesCalled, b.TimesCalled)
	}
	if !a.PreviousLogin.Equal(b.PreviousLogin) {
		t.Errorf("PreviousLogin differs: Authenticate=%v BeginSession=%v", a.PreviousLogin, b.PreviousLogin)
	}
	if a.LastLogin.IsZero() || b.LastLogin.IsZero() {
		t.Error("one of the paths did not stamp LastLogin")
	}
}

// The deleted-user guard belongs to the shared entry point, not only to
// Authenticate: a caller reaching BeginSession another way must not be able to
// open a session on a deleted account.
func TestBeginSessionDeniesDeletedAccounts(t *testing.T) {
	um := mgrWithAccount(t, &User{ID: 1, Handle: "Ghost", DeletedUser: true, TimesCalled: 3})

	if _, ok := um.BeginSession("Ghost"); ok {
		t.Fatal("BeginSession opened a session on a deleted account")
	}
	um.mu.RLock()
	stored := *um.users["ghost"]
	um.mu.RUnlock()
	if stored.TimesCalled != 3 {
		t.Errorf("TimesCalled = %d, want 3 — a rejected session must not stamp bookkeeping", stored.TimesCalled)
	}
}

func TestBeginSessionUnknownHandle(t *testing.T) {
	um := mgrWithAccount(t, &User{ID: 1, Handle: "Known"})
	if _, ok := um.BeginSession("Nobody"); ok {
		t.Error("BeginSession succeeded for a handle that does not exist")
	}
}
