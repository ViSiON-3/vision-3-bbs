package user

import (
	"path/filepath"
	"testing"
)

func mgrForSignup(t *testing.T, autoValidate bool) *UserMgr {
	t.Helper()
	um := NewUserMgrForTest()
	um.path = filepath.Join(t.TempDir(), "users.json")
	um.SetNewUserLevel(10)
	um.SetAutoValidateNewUsers(autoValidate)
	return um
}

// Default: signups are unvalidated, as they have always been. An upgrading
// system must not silently start validating everyone.
func TestAddUserUnvalidatedByDefault(t *testing.T) {
	um := mgrForSignup(t, false)
	u, err := um.AddUser("pw123456", "Newbie", "New Bie", "")
	if err != nil {
		t.Fatalf("AddUser: %v", err)
	}
	if u.Validated {
		t.Error("signup was validated with autoValidateNewUsers off")
	}
	if u.AccessLevel != 10 {
		t.Errorf("AccessLevel = %d, want the configured new-user level 10", u.AccessLevel)
	}
}

func TestAddUserValidatedWhenEnabled(t *testing.T) {
	um := mgrForSignup(t, true)
	u, err := um.AddUser("pw123456", "Newbie", "New Bie", "")
	if err != nil {
		t.Fatalf("AddUser: %v", err)
	}
	if !u.Validated {
		t.Error("signup was not validated with autoValidateNewUsers on")
	}
	// The setting marks the account reviewed; it does not grant extra access.
	if u.AccessLevel != 10 {
		t.Errorf("AccessLevel = %d, want 10 — auto-validation must not change the level", u.AccessLevel)
	}
}

// The setting is read at creation time, so a config reload takes effect for
// subsequent signups without a restart.
func TestAutoValidateRespondsToConfigReload(t *testing.T) {
	um := mgrForSignup(t, false)

	before, err := um.AddUser("pw123456", "First", "First User", "")
	if err != nil {
		t.Fatalf("AddUser: %v", err)
	}
	um.SetAutoValidateNewUsers(true)
	after, err := um.AddUser("pw123456", "Second", "Second User", "")
	if err != nil {
		t.Fatalf("AddUser: %v", err)
	}

	if before.Validated {
		t.Error("account created before the reload should be unvalidated")
	}
	if !after.Validated {
		t.Error("account created after the reload should be validated")
	}
}
