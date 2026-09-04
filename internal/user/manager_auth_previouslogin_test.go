package user

import (
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func seedAuthUser(t *testing.T, handle, password string, lastLogin time.Time) *UserMgr {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("bcrypt: %v", err)
	}
	um := NewUserMgrForTest(&User{
		ID:           1,
		Handle:       handle,
		PasswordHash: string(hash),
		LastLogin:    lastLogin,
	})
	um.path = t.TempDir()
	return um
}

// Authenticate must roll the old LastLogin into PreviousLogin. Everything that
// asks "what is new since this user was last here?" depends on it — LastLogin
// itself is overwritten with now() before the login sequence even runs.
func TestAuthenticateRollsLastLoginIntoPreviousLogin(t *testing.T) {
	prior := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	um := seedAuthUser(t, "Tester", "pw123456", prior)

	got, ok := um.Authenticate("Tester", "pw123456")
	if !ok {
		t.Fatal("authentication failed")
	}
	if !got.PreviousLogin.Equal(prior) {
		t.Errorf("PreviousLogin = %v, want the prior stamp %v", got.PreviousLogin, prior)
	}
	if !got.LastLogin.After(prior) {
		t.Errorf("LastLogin = %v, expected it to advance past %v", got.LastLogin, prior)
	}
	// The distinction the whole feature rests on.
	if got.LastLogin.Equal(got.PreviousLogin) {
		t.Error("LastLogin and PreviousLogin must not be the same instant")
	}
}

// A first-time caller has no prior visit, so PreviousLogin stays zero and
// callers can treat that as "never here before".
func TestAuthenticateFirstLoginHasZeroPreviousLogin(t *testing.T) {
	um := seedAuthUser(t, "Newbie", "pw123456", time.Time{})

	got, ok := um.Authenticate("Newbie", "pw123456")
	if !ok {
		t.Fatal("authentication failed")
	}
	if !got.PreviousLogin.IsZero() {
		t.Errorf("PreviousLogin = %v, want zero for a first-time caller", got.PreviousLogin)
	}
}

// The returned record is snapshotted under the lock that wrote it. Re-reading
// after unlocking would let a concurrent login for the same handle overwrite
// PreviousLogin first, and that session would then measure "new since your
// last visit" against another session's timestamp.
func TestAuthenticateSnapshotIsSelfConsistent(t *testing.T) {
	prior := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	um := seedAuthUser(t, "Racer", "pw123456", prior)

	const n = 8
	var wg sync.WaitGroup
	results := make([]*User, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if u, ok := um.Authenticate("Racer", "pw123456"); ok {
				results[i] = u
			}
		}(i)
	}
	wg.Wait()

	for i, u := range results {
		if u == nil {
			t.Fatalf("session %d failed to authenticate", i)
		}
		if !u.PreviousLogin.Before(u.LastLogin) {
			t.Errorf("session %d: PreviousLogin %v is not before its own LastLogin %v",
				i, u.PreviousLogin, u.LastLogin)
		}
	}
}
