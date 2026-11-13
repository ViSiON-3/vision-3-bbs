package menu

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/perm"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// useRoles makes data the BBS's roles for the test.
func useRoles(t *testing.T, data string) {
	t.Helper()
	r, diags, err := perm.Parse([]byte(data))
	if err != nil || len(diags) > 0 {
		t.Fatalf("roles: %v %v", err, diags)
	}
	perm.SetCurrent(r)
	t.Cleanup(func() { perm.SetCurrent(nil) })
}

const validatorRole = `{"roles": [{"name": "validator", "permissions": ["users.validate"]}]}`

// addUserAt adds a user and stores the level, validation and roles given.
func addUserAt(t *testing.T, um *user.UserMgr, handle string, level int, validated bool, roles ...string) *user.User {
	t.Helper()
	u, err := um.AddUser("password", handle, handle+" Real", "Loc")
	if err != nil {
		t.Fatalf("AddUser(%s): %v", handle, err)
	}
	u.AccessLevel, u.Validated, u.Roles = level, validated, roles
	if err := um.UpdateUserByID(u); err != nil {
		t.Fatalf("UpdateUserByID(%s): %v", handle, err)
	}
	got, _ := um.GetUserByID(u.ID)
	return got
}

func TestValidatorMayOnlyApprovePendingAccounts(t *testing.T) {
	useRoles(t, validatorRole)
	e, um := newUserEditorTestEnv(t)
	validator := addUserAt(t, um, "Val", 20, true, "validator")
	tests := []struct {
		name    string
		changes map[string]interface{}
		target  func() *user.User
		want    string // "" for saved
	}{
		{"approve", map[string]interface{}{"validated": true}, func() *user.User { return addUserAt(t, um, "New1", 1, false) }, ""},
		{"level with approval", map[string]interface{}{"validated": true, "level": 200}, func() *user.User { return addUserAt(t, um, "New2", 1, false) }, msgValidatorApproveOnly},
		{"flags", map[string]interface{}{"flags": "Z"}, func() *user.User { return addUserAt(t, um, "New3", 1, false) }, msgValidatorApproveOnly},
		{"roles", map[string]interface{}{"roles": "validator"}, func() *user.User { return addUserAt(t, um, "New4", 1, false) }, msgValidatorApproveOnly},
		{"unvalidate", map[string]interface{}{"validated": false}, func() *user.User { return addUserAt(t, um, "Old1", 10, true) }, msgValidatorApproveOnly},
		{"delete", map[string]interface{}{"deleted": true}, func() *user.User { return addUserAt(t, um, "New5", 1, false) }, msgValidatorApproveOnly},
		{"already validated", map[string]interface{}{"validated": true}, func() *user.User {
			u := addUserAt(t, um, "Old2", 10, true)
			u.Validated = false // what a stale editor list still shows
			return u
		}, msgValidatorNotPending},
		{"banned", map[string]interface{}{"validated": true}, func() *user.User { return addUserAt(t, um, "Banned", 0, false) }, msgValidatorNotPending},
		{"self escalation", map[string]interface{}{"level": 255}, func() *user.User { got, _ := um.GetUserByID(validator.ID); return got }, msgValidatorApproveOnly},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target := tt.target()
			stored, _ := um.GetUserByID(target.ID)
			orig := map[int]time.Time{target.ID: stored.UpdatedAt}
			msg, saved := e.applyPendingUserChanges(um, validator, target, tt.changes, orig)
			after, _ := um.GetUserByID(target.ID)
			if tt.want == "" {
				if !saved || !after.Validated || after.AccessLevel != 10 {
					t.Fatalf("approve: saved=%v msg=%q after=%+v", saved, msg, after)
				}
				return
			}
			if saved || msg != tt.want {
				t.Fatalf("saved=%v msg=%q, want %q", saved, msg, tt.want)
			}
			if after.AccessLevel != stored.AccessLevel || after.Validated != stored.Validated || after.Flags != stored.Flags || after.DeletedUser != stored.DeletedUser {
				t.Fatalf("record changed: %+v -> %+v", stored, after)
			}
		})
	}
}

// A permission taken away while the queue is open stops the next save, and
// the actor's own session copy can't stand in for the stored record.
func TestValidatorRevocationStopsNextSave(t *testing.T) {
	useRoles(t, validatorRole)
	e, um := newUserEditorTestEnv(t)
	session := addUserAt(t, um, "Val", 20, true, "validator")
	pending := addUserAt(t, um, "New", 1, false)
	approve := func() (string, bool) {
		stored, _ := um.GetUserByID(pending.ID)
		return e.applyPendingUserChanges(um, session, pending, map[string]interface{}{"validated": true}, map[int]time.Time{pending.ID: stored.UpdatedAt})
	}

	stored, _ := um.GetUserByID(session.ID)
	stored.Roles = nil
	if err := um.UpdateUserByID(stored); err != nil {
		t.Fatal(err)
	}
	if msg, saved := approve(); saved || msg != msgAdminNoLongerAllowed {
		t.Fatalf("role removed from the record: saved=%v msg=%q", saved, msg)
	}

	stored.Roles = []string{"validator"}
	if err := um.UpdateUserByID(stored); err != nil {
		t.Fatal(err)
	}
	useRoles(t, `{"roles": []}`)
	if msg, saved := approve(); saved || msg != msgAdminNoLongerAllowed {
		t.Fatalf("role removed from roles.json: saved=%v msg=%q", saved, msg)
	}

	useRoles(t, validatorRole)
	if msg, saved := approve(); !saved {
		t.Fatalf("role restored: msg=%q", msg)
	}
}

// A sysop's saves are as before, roles or not, and leave no grant note.
func TestSysopSavesUnchangedWithRoles(t *testing.T) {
	useRoles(t, validatorRole)
	e, um := newUserEditorTestEnv(t)
	sysop := addUserAt(t, um, "Boss", 255, true)
	target := addUserAt(t, um, "Someone", 10, true)
	orig := map[int]time.Time{target.ID: target.UpdatedAt}
	changes := map[string]interface{}{"level": 50, "flags": "AB", "roles": "Validator"}
	if msg, saved := e.applyPendingUserChanges(um, sysop, target, changes, orig); !saved {
		t.Fatalf("sysop save refused: %q", msg)
	}
	got, _ := um.GetUserByID(target.ID)
	if got.AccessLevel != 50 || got.Flags != "AB" || strings.Join(got.Roles, ",") != "validator" {
		t.Fatalf("saved record = %+v", got)
	}

	// A role that has since left roles.json is refused at save.
	useRoles(t, `{"roles": []}`)
	orig[target.ID] = got.UpdatedAt
	if msg, saved := e.applyPendingUserChanges(um, sysop, got, map[string]interface{}{"roles": "validator", "level": 70, "flags": "Z"}, orig); saved || !strings.Contains(msg, "no role named validator") {
		t.Fatalf("undefined role: saved=%v msg=%q", saved, msg)
	}
	// The refused save left the editor's copy as it was, so nothing staged
	// with the bad role rides along on a later save.
	if got.AccessLevel != 50 || got.Flags != "AB" {
		t.Fatalf("refused save changed the editor's copy: level %d, flags %q", got.AccessLevel, got.Flags)
	}

	// A sysop demoted while the editor is open is refused too.
	demoted, _ := um.GetUserByID(sysop.ID)
	demoted.AccessLevel = 20
	if err := um.UpdateUserByID(demoted); err != nil {
		t.Fatal(err)
	}
	if msg, saved := e.applyPendingUserChanges(um, sysop, got, map[string]interface{}{"level": 60}, orig); saved || msg != msgAdminNoLongerAllowed {
		t.Fatalf("demoted sysop: saved=%v msg=%q", saved, msg)
	}
}

func TestValidatorApprovalIsAudited(t *testing.T) {
	useRoles(t, validatorRole)
	e, _ := newUserEditorTestEnv(t)
	dir := t.TempDir()
	um, err := user.NewUserManager(dir)
	if err != nil {
		t.Fatal(err)
	}
	validator := addUserAt(t, um, "Val", 20, true, "validator")
	pending := addUserAt(t, um, "New", 1, false)
	orig := map[int]time.Time{pending.ID: pending.UpdatedAt}
	if msg, saved := e.applyPendingUserChanges(um, validator, pending, map[string]interface{}{"validated": true}, orig); !saved {
		t.Fatalf("approve refused: %q", msg)
	}
	data, err := os.ReadFile(filepath.Join(dir, "admin_activity.json"))
	if err != nil {
		t.Fatal(err)
	}
	var entries []user.AdminActivityLog
	if err := json.Unmarshal(data, &entries); err != nil {
		t.Fatal(err)
	}
	last := entries[len(entries)-1]
	if last.AdminID != validator.ID || last.TargetUserID != pending.ID || last.FieldName != "validated" || last.Notes != "users.validate via role validator" {
		t.Fatalf("audit entry = %+v", last)
	}
}

func TestMayValidateUsers(t *testing.T) {
	useRoles(t, validatorRole)
	e, um := newUserEditorTestEnv(t)
	cases := []struct {
		u    *user.User
		want bool
	}{
		{addUserAt(t, um, "Boss", 255, true), true},
		{addUserAt(t, um, "CoSysop", 250, true), false},
		{addUserAt(t, um, "Val", 20, true, "validator"), true},
		{addUserAt(t, um, "Typo", 20, true, "validatr"), false},
		{nil, false},
	}
	for _, c := range cases {
		if got := e.mayValidateUsers(um, c.u); got != c.want {
			t.Errorf("mayValidateUsers(%+v) = %v, want %v", c.u, got, c.want)
		}
	}
	// The session copy claiming a role the record doesn't list grants nothing.
	plain := addUserAt(t, um, "Plain", 20, true)
	plain.Roles = []string{"validator"}
	if e.mayValidateUsers(um, plain) {
		t.Error("a role only on the session copy granted users.validate")
	}
}

func TestValidatorEditorKeys(t *testing.T) {
	for _, k := range "gGsSqQxXjJkK" {
		if !validatorEditorKey(int(k)) {
			t.Errorf("key %q refused", k)
		}
	}
	for _, k := range "aAbBcCdDeEfFnNpPrRiItTvV09" {
		if validatorEditorKey(int(k)) {
			t.Errorf("key %q allowed", k)
		}
	}
}

// TestUserEditor_AssignsRoles pins the sysop's [R] key: a role roles.json
// doesn't define is refused as typed, a defined one is staged, saved and
// audited, and the session picks it up for PERM:.
func TestUserEditor_AssignsRoles(t *testing.T) {
	useRoles(t, validatorRole)
	env := newMenuEnv(t)
	input := "j" +
		"r" + "nobody\r" +
		"r" + "Validator\r" +
		"s" + "q"
	r := env.runCmd("ADMINLISTUSERS", env.sysop, "", input)
	if r.err != nil {
		t.Fatalf("err = %v", r.err)
	}
	if !r.has("no role named nobody in configs/roles.json (defined: validator)", "Field marked for update.", "Changes saved for Caller.") {
		t.Errorf("output:\n%s", r.text())
	}
	u, _ := env.diskUser(2)
	if strings.Join(u.Roles, ",") != "validator" {
		t.Fatalf("roles on disk = %v", u.Roles)
	}
	if l, ok := modLogField(modAdminLog(t, env), 2, "roles"); !ok || l.OldValue != "" || l.NewValue != "validator" || l.Notes != "" {
		t.Errorf("roles audit entry = %+v (found %v)", l, ok)
	}
	if !checkACS("PERM:users.validate&ROLE:validator", u, nil, nil, time.Now()) {
		t.Error("PERM:/ROLE: fail for the saved record")
	}
}

// The editor's mode comes from the stored record: a sysop demoted since
// login, holding no role, is turned away rather than shown the full editor
// on the strength of their session copy.
func TestUserEditor_DemotedSysopTurnedAway(t *testing.T) {
	useRoles(t, validatorRole)
	env := newMenuEnv(t)
	stored, _ := env.um.GetUserByID(env.sysop.ID)
	stored.AccessLevel = 20
	if err := env.um.UpdateUserByID(stored); err != nil {
		t.Fatal(err)
	}
	r := env.runCmd("ADMINLISTUSERS", env.sysop, "", "q")
	if r.err != nil {
		t.Fatalf("err = %v", r.err)
	}
	if !r.has("Access denied.") {
		t.Errorf("output:\n%s", r.text())
	}
}

// The path the save rollback closes, by keys: a level staged with a role
// that leaves roles.json before the save is refused; X discards both; a
// later save of something else writes only that, not the discarded level.
func TestUserEditor_RefusedSaveThenAbortWritesNothingDiscarded(t *testing.T) {
	useRoles(t, validatorRole)
	env := newMenuEnv(t)
	env.whenOutput("Field marked for update.", func() { useRoles(t, `{"roles": []}`) })
	input := "j" +
		"r" + "Validator\r" + // staged; then roles.json loses it
		"f" + "60\r" +
		"s" + // refused: no role named validator
		"x" +
		"e" + "AB\r" +
		"s" + "q"
	r := env.runCmd("ADMINLISTUSERS", env.sysop, "", input)
	if r.err != nil {
		t.Fatalf("err = %v", r.err)
	}
	if !r.has("no role named validator", "Changes discarded.", "Changes saved for Caller.") {
		t.Errorf("output:\n%s", r.text())
	}
	u, _ := env.diskUser(2)
	if u.AccessLevel != 10 || len(u.Roles) != 0 || u.Flags != "AB" {
		t.Fatalf("saved record: level %d, roles %v, flags %q; want 10, none, AB", u.AccessLevel, u.Roles, u.Flags)
	}
	if _, ok := modLogField(modAdminLog(t, env), 2, "level"); ok {
		t.Error("a level change was audited")
	}
}
