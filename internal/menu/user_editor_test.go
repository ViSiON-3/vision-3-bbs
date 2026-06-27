package menu

import (
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// newUserEditorTestEnv builds a MenuExecutor with an in-memory user manager for
// exercising applyPendingUserChanges (the I/O-free core of the user editor).
func newUserEditorTestEnv(t *testing.T) (*MenuExecutor, *user.UserMgr) {
	t.Helper()
	um, err := user.NewUserManager(t.TempDir())
	if err != nil {
		t.Fatalf("NewUserManager: %v", err)
	}
	e := &MenuExecutor{ServerCfg: config.ServerConfig{SysOpLevel: 255, RegularUserLevel: 10}}
	return e, um
}

func TestApplyPendingUserChanges_PersistsFields(t *testing.T) {
	e, um := newUserEditorTestEnv(t)
	u, err := um.AddUser("password", "TestUser", "Real Name", "Loc")
	if err != nil {
		t.Fatalf("AddUser: %v", err)
	}
	admin := &user.User{ID: 1, Handle: "SysOp"}
	orig := map[int]time.Time{u.ID: u.UpdatedAt}
	pending := map[string]interface{}{"handle": "Renamed", "level": 50, "flags": "ABC"}

	msg, saved := e.applyPendingUserChanges(um, admin, u, pending, orig)
	if !saved {
		t.Fatalf("expected saved=true, got false (msg=%q)", msg)
	}
	got, ok := um.GetUserByID(u.ID)
	if !ok {
		t.Fatal("user not found after save")
	}
	if got.Handle != "Renamed" {
		t.Errorf("handle = %q, want Renamed", got.Handle)
	}
	if got.AccessLevel != 50 {
		t.Errorf("level = %d, want 50", got.AccessLevel)
	}
	if got.Flags != "ABC" {
		t.Errorf("flags = %q, want ABC", got.Flags)
	}
	// originalTimestamps must advance to the new UpdatedAt for subsequent saves.
	if !orig[u.ID].Equal(got.UpdatedAt) {
		t.Errorf("originalTimestamps not refreshed: %v != %v", orig[u.ID], got.UpdatedAt)
	}
}

func TestApplyPendingUserChanges_BlankHandleRejected(t *testing.T) {
	e, um := newUserEditorTestEnv(t)
	u, _ := um.AddUser("password", "TestUser", "Real Name", "Loc")
	admin := &user.User{ID: 1, Handle: "SysOp"}
	orig := map[int]time.Time{u.ID: u.UpdatedAt}

	_, saved := e.applyPendingUserChanges(um, admin, u, map[string]interface{}{"handle": "   "}, orig)
	if saved {
		t.Fatal("expected blank handle to be rejected")
	}
}

func TestApplyPendingUserChanges_OptimisticLock(t *testing.T) {
	e, um := newUserEditorTestEnv(t)
	u, _ := um.AddUser("password", "TestUser", "Real Name", "Loc")
	admin := &user.User{ID: 1, Handle: "SysOp"}
	// Stale timestamp: someone else modified the record since editing began.
	stale := map[int]time.Time{u.ID: u.UpdatedAt.Add(-time.Hour)}

	_, saved := e.applyPendingUserChanges(um, admin, u, map[string]interface{}{"level": 20}, stale)
	if saved {
		t.Fatal("expected optimistic-lock failure on stale timestamp")
	}
}

func TestApplyPendingUserChanges_ProtectsUserOne(t *testing.T) {
	e, um := newUserEditorTestEnv(t)
	u1, ok := um.GetUserByID(1)
	if !ok {
		t.Skip("no default user #1 present")
	}
	admin := &user.User{ID: 1, Handle: "SysOp"}
	orig := map[int]time.Time{1: u1.UpdatedAt}
	pending := map[string]interface{}{"validated": false}

	_, saved := e.applyPendingUserChanges(um, admin, u1, pending, orig)
	if saved {
		t.Fatal("expected User #1 unvalidate to be blocked")
	}
	if _, still := pending["validated"]; still {
		t.Error("expected the protected change to be dropped from pendingChanges")
	}
}

func TestApplyPendingUserChanges_ValidateUpgradesLevel(t *testing.T) {
	e, um := newUserEditorTestEnv(t)
	u, _ := um.AddUser("password", "LowLevel", "Real Name", "Loc")
	u.AccessLevel = 1
	u.Validated = false
	if err := um.UpdateUserByID(u); err != nil {
		t.Fatalf("UpdateUserByID: %v", err)
	}
	admin := &user.User{ID: 1, Handle: "SysOp"}
	orig := map[int]time.Time{u.ID: u.UpdatedAt}

	_, saved := e.applyPendingUserChanges(um, admin, u, map[string]interface{}{"validated": true}, orig)
	if !saved {
		t.Fatal("expected validate to succeed")
	}
	got, _ := um.GetUserByID(u.ID)
	if !got.Validated {
		t.Error("expected user to be validated")
	}
	if got.AccessLevel != 10 {
		t.Errorf("level = %d, want 10 (RegularUserLevel upgrade)", got.AccessLevel)
	}
}
