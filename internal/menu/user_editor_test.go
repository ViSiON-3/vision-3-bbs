package menu

import (
	"strings"
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

func TestApplyPendingUserChanges_TextFields(t *testing.T) {
	e, um := newUserEditorTestEnv(t)
	u, err := um.AddUser("password", "Alice", "Alice Real", "GroupA")
	if err != nil {
		t.Fatalf("AddUser: %v", err)
	}
	admin := &user.User{ID: 1, Handle: "SysOp"}
	orig := map[int]time.Time{u.ID: u.UpdatedAt}
	pending := map[string]interface{}{
		"realname": "Alice Updated",
		"grouploc": "GroupB",
		"note":     "private note",
		"flags":    "XYZ",
	}

	msg, saved := e.applyPendingUserChanges(um, admin, u, pending, orig)
	if !saved {
		t.Fatalf("expected saved=true, got false (msg=%q)", msg)
	}
	got, ok := um.GetUserByID(u.ID)
	if !ok {
		t.Fatal("user not found after save")
	}
	if got.RealName != "Alice Updated" {
		t.Errorf("RealName = %q, want %q", got.RealName, "Alice Updated")
	}
	if got.GroupLocation != "GroupB" {
		t.Errorf("GroupLocation = %q, want %q", got.GroupLocation, "GroupB")
	}
	if got.PrivateNote != "private note" {
		t.Errorf("PrivateNote = %q, want %q", got.PrivateNote, "private note")
	}
	if got.Flags != "XYZ" {
		t.Errorf("Flags = %q, want %q", got.Flags, "XYZ")
	}
}

func TestApplyPendingUserChanges_SoftDelete(t *testing.T) {
	e, um := newUserEditorTestEnv(t)
	u, _ := um.AddUser("password", "ToDelete", "Real", "Loc")
	admin := &user.User{ID: 1, Handle: "SysOp"}
	orig := map[int]time.Time{u.ID: u.UpdatedAt}

	msg, saved := e.applyPendingUserChanges(um, admin, u, map[string]interface{}{"deleted": true}, orig)
	if !saved {
		t.Fatalf("expected saved=true, got false (msg=%q)", msg)
	}
	got, ok := um.GetUserByID(u.ID)
	if !ok {
		t.Fatal("user not found after save")
	}
	if !got.DeletedUser {
		t.Error("expected DeletedUser=true after soft delete")
	}
	if got.DeletedAt == nil {
		t.Error("expected DeletedAt to be set after soft delete")
	}
}

func TestApplyPendingUserChanges_Undelete(t *testing.T) {
	e, um := newUserEditorTestEnv(t)
	u, _ := um.AddUser("password", "WasDeleted", "Real", "Loc")
	now := time.Now()
	u.DeletedUser = true
	u.DeletedAt = &now
	if err := um.UpdateUserByID(u); err != nil {
		t.Fatalf("UpdateUserByID: %v", err)
	}
	admin := &user.User{ID: 1, Handle: "SysOp"}
	orig := map[int]time.Time{u.ID: u.UpdatedAt}

	msg, saved := e.applyPendingUserChanges(um, admin, u, map[string]interface{}{"deleted": false}, orig)
	if !saved {
		t.Fatalf("expected saved=true, got false (msg=%q)", msg)
	}
	got, ok := um.GetUserByID(u.ID)
	if !ok {
		t.Fatal("user not found after save")
	}
	if got.DeletedUser {
		t.Error("expected DeletedUser=false after undelete")
	}
	if got.DeletedAt != nil {
		t.Error("expected DeletedAt to be nil after undelete")
	}
}

func TestApplyPendingUserChanges_PasswordChange(t *testing.T) {
	e, um := newUserEditorTestEnv(t)
	u, _ := um.AddUser("oldpassword", "Bob", "Real", "Loc")
	// Capture the original hash before calling applyPendingUserChanges, because
	// the function mutates the target struct in place.
	originalHash := u.PasswordHash
	admin := &user.User{ID: 1, Handle: "SysOp"}
	orig := map[int]time.Time{u.ID: u.UpdatedAt}

	msg, saved := e.applyPendingUserChanges(um, admin, u, map[string]interface{}{"password": "newpassword"}, orig)
	if !saved {
		t.Fatalf("expected saved=true, got false (msg=%q)", msg)
	}
	got, ok := um.GetUserByID(u.ID)
	if !ok {
		t.Fatal("user not found after save")
	}
	if got.PasswordHash == "" {
		t.Error("expected PasswordHash to be set after password change")
	}
	// Should be a different hash than the original.
	if got.PasswordHash == originalHash {
		t.Error("expected PasswordHash to change after password update")
	}
}

func TestApplyPendingUserChanges_UserOneProtectsLevel(t *testing.T) {
	e, um := newUserEditorTestEnv(t)
	// e.ServerCfg.SysOpLevel = 255
	u1, ok := um.GetUserByID(1)
	if !ok {
		t.Skip("no default user #1 present")
	}
	admin := &user.User{ID: 1, Handle: "SysOp"}
	orig := map[int]time.Time{1: u1.UpdatedAt}
	pending := map[string]interface{}{"level": 10} // Below SysOpLevel (255)

	_, saved := e.applyPendingUserChanges(um, admin, u1, pending, orig)
	if saved {
		t.Fatal("expected lowering User #1 below SysOp level to be blocked")
	}
	if _, still := pending["level"]; still {
		t.Error("expected the protected level change to be dropped from pendingChanges")
	}
}

func TestApplyPendingUserChanges_UserOneProtectsDelete(t *testing.T) {
	e, um := newUserEditorTestEnv(t)
	u1, ok := um.GetUserByID(1)
	if !ok {
		t.Skip("no default user #1 present")
	}
	admin := &user.User{ID: 1, Handle: "SysOp"}
	orig := map[int]time.Time{1: u1.UpdatedAt}
	pending := map[string]interface{}{"deleted": true}

	_, saved := e.applyPendingUserChanges(um, admin, u1, pending, orig)
	if saved {
		t.Fatal("expected deleting User #1 to be blocked")
	}
	if _, still := pending["deleted"]; still {
		t.Error("expected the protected delete change to be dropped from pendingChanges")
	}
}

// ---------------------------------------------------------------------------
// isPendingValidationUser tests
// ---------------------------------------------------------------------------

func TestIsPendingValidationUser(t *testing.T) {
	tests := []struct {
		name string
		u    *user.User
		want bool
	}{
		{
			name: "nil user",
			u:    nil,
			want: false,
		},
		{
			name: "validated user",
			u:    &user.User{Validated: true, DeletedUser: false, AccessLevel: 10},
			want: false,
		},
		{
			name: "deleted unvalidated user",
			u:    &user.User{Validated: false, DeletedUser: true, AccessLevel: 10},
			want: false,
		},
		{
			name: "banned user (level 0)",
			u:    &user.User{Validated: false, DeletedUser: false, AccessLevel: 0},
			want: false,
		},
		{
			name: "pending validation user",
			u:    &user.User{Validated: false, DeletedUser: false, AccessLevel: 5},
			want: true,
		},
		{
			name: "unvalidated high-level user",
			u:    &user.User{Validated: false, DeletedUser: false, AccessLevel: 100},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isPendingValidationUser(tt.u)
			if got != tt.want {
				t.Errorf("isPendingValidationUser() = %v, want %v", got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// pendingValidationCount tests
// ---------------------------------------------------------------------------

func TestPendingValidationCount(t *testing.T) {
	t.Run("nil manager returns zero", func(t *testing.T) {
		if got := pendingValidationCount(nil); got != 0 {
			t.Errorf("pendingValidationCount(nil) = %d, want 0", got)
		}
	})

	t.Run("empty manager returns zero", func(t *testing.T) {
		_, um := newUserEditorTestEnv(t)
		// NewUserManager may or may not create user #1; either way count must be 0
		// since any auto-created users are validated.
		got := pendingValidationCount(um)
		// All auto-created users should be validated, so count should be 0.
		users := um.GetAllUsers()
		expected := 0
		for _, u := range users {
			if isPendingValidationUser(u) {
				expected++
			}
		}
		if got != expected {
			t.Errorf("pendingValidationCount = %d, want %d", got, expected)
		}
	})

	t.Run("counts only unvalidated non-deleted non-banned users", func(t *testing.T) {
		_, um := newUserEditorTestEnv(t)
		// Add a mix of users.
		pending1, _ := um.AddUser("p", "Pending1", "R", "L")
		pending1.Validated = false
		pending1.AccessLevel = 5
		_ = um.UpdateUserByID(pending1)

		pending2, _ := um.AddUser("p", "Pending2", "R", "L")
		pending2.Validated = false
		pending2.AccessLevel = 1
		_ = um.UpdateUserByID(pending2)

		banned, _ := um.AddUser("p", "Banned", "R", "L")
		banned.Validated = false
		banned.AccessLevel = 0
		_ = um.UpdateUserByID(banned)

		deleted, _ := um.AddUser("p", "Deleted", "R", "L")
		deleted.Validated = false
		deleted.DeletedUser = true
		deleted.AccessLevel = 5
		_ = um.UpdateUserByID(deleted)

		validated, _ := um.AddUser("p", "Validated", "R", "L")
		validated.Validated = true
		validated.AccessLevel = 10
		_ = um.UpdateUserByID(validated)

		got := pendingValidationCount(um)
		// Only Pending1 and Pending2 should count.
		if got < 2 {
			t.Errorf("pendingValidationCount = %d, want at least 2", got)
		}
		// Banned, deleted, and validated should not count.
		all := um.GetAllUsers()
		want := 0
		for _, u := range all {
			if isPendingValidationUser(u) {
				want++
			}
		}
		if got != want {
			t.Errorf("pendingValidationCount = %d, want %d", got, want)
		}
	})
}

// ---------------------------------------------------------------------------
// sortedUsersByID tests
// ---------------------------------------------------------------------------

func TestSortedUsersByID(t *testing.T) {
	t.Run("nil entries are filtered out", func(t *testing.T) {
		input := []*user.User{
			{ID: 3, Handle: "C"},
			nil,
			{ID: 1, Handle: "A"},
			nil,
			{ID: 2, Handle: "B"},
		}
		got := sortedUsersByID(input)
		if len(got) != 3 {
			t.Fatalf("len = %d, want 3", len(got))
		}
		for i, id := range []int{1, 2, 3} {
			if got[i].ID != id {
				t.Errorf("got[%d].ID = %d, want %d", i, got[i].ID, id)
			}
		}
	})

	t.Run("empty input returns empty slice", func(t *testing.T) {
		got := sortedUsersByID(nil)
		if len(got) != 0 {
			t.Errorf("len = %d, want 0", len(got))
		}
	})

	t.Run("single element unchanged", func(t *testing.T) {
		input := []*user.User{{ID: 42, Handle: "solo"}}
		got := sortedUsersByID(input)
		if len(got) != 1 || got[0].ID != 42 {
			t.Errorf("unexpected result: %v", got)
		}
	})

	t.Run("already sorted input stays sorted", func(t *testing.T) {
		input := []*user.User{
			{ID: 10}, {ID: 20}, {ID: 30},
		}
		got := sortedUsersByID(input)
		for i, want := range []int{10, 20, 30} {
			if got[i].ID != want {
				t.Errorf("got[%d].ID = %d, want %d", i, got[i].ID, want)
			}
		}
	})

	t.Run("reverse sorted input becomes sorted", func(t *testing.T) {
		input := []*user.User{
			{ID: 30}, {ID: 20}, {ID: 10},
		}
		got := sortedUsersByID(input)
		for i, want := range []int{10, 20, 30} {
			if got[i].ID != want {
				t.Errorf("got[%d].ID = %d, want %d", i, got[i].ID, want)
			}
		}
	})

	t.Run("all-nil input returns empty slice", func(t *testing.T) {
		input := []*user.User{nil, nil, nil}
		got := sortedUsersByID(input)
		if len(got) != 0 {
			t.Errorf("len = %d, want 0", len(got))
		}
	})
}

// ---------------------------------------------------------------------------
// adminTruncate tests
// ---------------------------------------------------------------------------

func TestAdminTruncate(t *testing.T) {
	tests := []struct {
		name  string
		input string
		max   int
		want  string
	}{
		{
			name:  "short string unchanged",
			input: "hello",
			max:   10,
			want:  "hello",
		},
		{
			name:  "exact length unchanged",
			input: "hello",
			max:   5,
			want:  "hello",
		},
		{
			name:  "truncate with ellipsis",
			input: "hello world",
			max:   8,
			want:  "hello w…",
		},
		{
			name:  "leading/trailing whitespace trimmed",
			input: "  hello  ",
			max:   10,
			want:  "hello",
		},
		{
			name:  "max=1 returns single char",
			input: "hello",
			max:   1,
			want:  "h",
		},
		{
			name:  "max=0 returns empty",
			input: "hello",
			max:   0,
			want:  "",
		},
		{
			name:  "empty string unchanged",
			input: "",
			max:   5,
			want:  "",
		},
		{
			name:  "unicode runes counted correctly",
			input: "日本語テスト",
			max:   4,
			want:  "日本語…",
		},
		{
			name:  "whitespace-only string becomes empty",
			input: "   ",
			max:   5,
			want:  "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := adminTruncate(tt.input, tt.max)
			if got != tt.want {
				t.Errorf("adminTruncate(%q, %d) = %q, want %q", tt.input, tt.max, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// adminTime tests
// ---------------------------------------------------------------------------

func TestAdminTime(t *testing.T) {
	t.Run("zero time returns N/A", func(t *testing.T) {
		got := adminTime(time.Time{})
		if got != "N/A" {
			t.Errorf("adminTime(zero) = %q, want %q", got, "N/A")
		}
	})

	t.Run("non-zero time formats correctly", func(t *testing.T) {
		ts := time.Date(2025, 3, 15, 9, 5, 0, 0, time.UTC)
		got := adminTime(ts)
		want := "2025-03-15 09:05"
		if got != want {
			t.Errorf("adminTime(%v) = %q, want %q", ts, got, want)
		}
	})

	t.Run("format includes hour and minute", func(t *testing.T) {
		ts := time.Date(2024, 12, 31, 23, 59, 45, 0, time.UTC)
		got := adminTime(ts)
		if !strings.Contains(got, "23:59") {
			t.Errorf("adminTime(%v) = %q, expected to contain time portion", ts, got)
		}
	})
}

// ---------------------------------------------------------------------------
// adminDate tests
// ---------------------------------------------------------------------------

func TestAdminDate(t *testing.T) {
	t.Run("zero time returns N/A", func(t *testing.T) {
		got := adminDate(time.Time{})
		if got != "N/A" {
			t.Errorf("adminDate(zero) = %q, want %q", got, "N/A")
		}
	})

	t.Run("non-zero time formats date only", func(t *testing.T) {
		ts := time.Date(2025, 6, 27, 15, 30, 0, 0, time.UTC)
		got := adminDate(ts)
		want := "2025-06-27"
		if got != want {
			t.Errorf("adminDate(%v) = %q, want %q", ts, got, want)
		}
	})

	t.Run("date does not include time portion", func(t *testing.T) {
		ts := time.Date(2024, 1, 1, 23, 59, 59, 0, time.UTC)
		got := adminDate(ts)
		if strings.Contains(got, ":") {
			t.Errorf("adminDate(%v) = %q, expected no time portion", ts, got)
		}
		if got != "2024-01-01" {
			t.Errorf("adminDate(%v) = %q, want %q", ts, got, "2024-01-01")
		}
	})
}
