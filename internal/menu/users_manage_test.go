package menu

import (
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// usersManageRoles: editor holds users.edit, banner users.ban, both holds
// the two.
const usersManageRoles = `{"roles": [
	{"name": "editor", "permissions": ["users.edit"]},
	{"name": "banner", "permissions": ["users.ban"]},
	{"name": "both", "permissions": ["users.edit", "users.ban"]},
	{"name": "validator", "permissions": ["users.validate"]}
]}`

// The save rule for users.edit and users.ban (#1045): which fields each
// changes, on which accounts, and what stays the sysop's.
func TestUsersEditAndBanSaveRule(t *testing.T) {
	useRoles(t, usersManageRoles)
	e, um := newUserEditorTestEnv(t)
	e.SetServerConfig(config.ServerConfig{SysOpLevel: 255, CoSysOpLevel: 250, RegularUserLevel: 10})
	editor := addUserAt(t, um, "Ed", 20, true, "editor")
	banner := addUserAt(t, um, "Ban", 20, true, "banner")
	both := addUserAt(t, um, "Both", 20, true, "both")
	caller := func() *user.User { return addUserAt(t, um, "C"+time.Now().Format("150405.000000000"), 10, true) }
	tests := []struct {
		name    string
		actor   *user.User
		changes map[string]interface{}
		target  func() *user.User
		want    string // "" for saved
		notes   string
	}{
		{"edit real name", editor, map[string]interface{}{"realname": "New Name"}, caller, "", "users.edit via role editor"},
		{"edit group and note", editor, map[string]interface{}{"grouploc": "Here", "note": "hi"}, caller, "", "users.edit via role editor"},
		{"edit can't set level", editor, map[string]interface{}{"level": 50}, caller, msgAdminFieldNotAllowed, ""},
		{"edit can't set flags", editor, map[string]interface{}{"flags": "Z"}, caller, msgAdminFieldNotAllowed, ""},
		{"edit can't rename", editor, map[string]interface{}{"handle": "Other"}, caller, msgAdminFieldNotAllowed, ""},
		{"edit can't change password", editor, map[string]interface{}{"password": "x"}, caller, msgAdminFieldNotAllowed, ""},
		{"edit can't delete", editor, map[string]interface{}{"deleted": true}, caller, msgAdminFieldNotAllowed, ""},
		{"edit can't ban", editor, map[string]interface{}{"validated": false, "level": 0}, caller, msgAdminFieldNotAllowed, ""},
		{"ceiling: co-sysop", editor, map[string]interface{}{"realname": "X"}, func() *user.User { return addUserAt(t, um, "Co", 250, true) }, msgAdminAboveCeiling, ""},
		{"ceiling: user 1", editor, map[string]interface{}{"realname": "X"}, func() *user.User { u, _ := um.GetUserByID(1); return u }, msgAdminAboveCeiling, ""},
		{"ban", banner, map[string]interface{}{"validated": false, "level": 0}, caller, "", "users.ban via role banner"},
		{"un-validate", banner, map[string]interface{}{"validated": false}, caller, "", "users.ban via role banner"},
		{"un-ban", banner, map[string]interface{}{"validated": true, "level": 10}, func() *user.User { return addUserAt(t, um, "Banned", 0, false) }, "", "users.ban via role banner"},
		{"un-ban to another level", banner, map[string]interface{}{"validated": true, "level": 200}, func() *user.User { return addUserAt(t, um, "Banned2", 0, false) }, msgAdminFieldNotAllowed, ""},
		{"ban can't edit", banner, map[string]interface{}{"realname": "X"}, caller, msgAdminFieldNotAllowed, ""},
		{"no self-ban", banner, map[string]interface{}{"validated": false, "level": 0}, func() *user.User { u, _ := um.GetUserByID(banner.ID); return u }, msgAdminNoSelfBan, ""},
		{"edit and ban together", both, map[string]interface{}{"note": "spam", "validated": false, "level": 0}, caller, "", "users.edit via role both; users.ban via role both"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target := tt.target()
			stored, _ := um.GetUserByID(target.ID)
			orig := map[int]time.Time{target.ID: stored.UpdatedAt}
			msg, saved := e.applyPendingUserChanges(um, tt.actor, target, tt.changes, orig)
			after, _ := um.GetUserByID(target.ID)
			if tt.want != "" {
				if saved || msg != tt.want {
					t.Fatalf("saved=%v msg=%q, want %q", saved, msg, tt.want)
				}
				if after.AccessLevel != stored.AccessLevel || after.Validated != stored.Validated || after.RealName != stored.RealName || after.Flags != stored.Flags {
					t.Fatalf("record changed: %+v -> %+v", stored, after)
				}
				return
			}
			if !saved {
				t.Fatalf("refused: %q", msg)
			}
			via, denied := e.authorizeUserChanges(um, tt.actor, stored, tt.changes)
			if denied != "" || via != tt.notes {
				t.Errorf("via = %q (%q), want %q", via, denied, tt.notes)
			}
		})
	}
}

// A validator's rules are unchanged by the new permissions.
func TestValidatorUnchangedByUsersEdit(t *testing.T) {
	useRoles(t, usersManageRoles)
	e, um := newUserEditorTestEnv(t)
	val := addUserAt(t, um, "Val", 20, true, "validator")
	target := addUserAt(t, um, "Old", 10, true)
	stored, _ := um.GetUserByID(target.ID)
	if msg, saved := e.applyPendingUserChanges(um, val, target, map[string]interface{}{"realname": "X"}, map[int]time.Time{target.ID: stored.UpdatedAt}); saved || msg != msgValidatorApproveOnly {
		t.Errorf("validator editing a name: saved=%v msg=%q", saved, msg)
	}
}

// By keys: a users.edit holder opens the full list, sees only accounts below
// co-sysop level, changes a real name, and can't use the sysop's keys.
func TestUserEditor_UsersEditByKeys(t *testing.T) {
	useRoles(t, usersManageRoles)
	env := newMenuEnv(t)
	cfg := env.e.GetServerConfig()
	cfg.CoSysOpLevel = 250
	env.e.SetServerConfig(cfg)
	stored, _ := env.um.GetUserByID(env.caller.ID)
	stored.Roles = []string{"editor"}
	if err := env.um.UpdateUserByID(stored); err != nil {
		t.Fatal(err)
	}
	target := addUserAt(t, env.um, "Target", 10, true)
	addUserAt(t, env.um, "CoSys", 250, true)

	// The list starts at the caller (#2), so J moves to Target.
	r := env.runCmd("ADMINLISTUSERS", env.caller, "", "j"+"e"+"b"+ueClear(len("Target Real"))+"Fixed Name\r"+"s"+"q")
	if r.err != nil {
		t.Fatalf("err = %v", r.err)
	}
	if r.has("CoSys") || r.has("Sysop") {
		t.Errorf("accounts above the ceiling listed:\n%s", r.text())
	}
	if !r.has("Your role doesn't allow that here.", "Changes saved for Target.") {
		t.Errorf("output:\n%s", r.text())
	}
	if u, _ := env.um.GetUserByID(target.ID); u.RealName != "Fixed Name" {
		t.Errorf("real name = %q", u.RealName)
	}
	if l, ok := modLogField(modAdminLog(t, env), target.ID, "realname"); !ok || l.Notes != "users.edit via role editor" {
		t.Errorf("audit entry = %+v (found %v)", l, ok)
	}
}

// BANUSER opens for users.ban, lists only accounts it reaches, and bans.
func TestBanUser_UsersBan(t *testing.T) {
	useRoles(t, usersManageRoles)
	env := newMenuEnv(t)
	cfg := env.e.GetServerConfig()
	cfg.CoSysOpLevel = 250
	env.e.SetServerConfig(cfg)
	stored, _ := env.um.GetUserByID(env.caller.ID)
	stored.Roles = []string{"banner"}
	if err := env.um.UpdateUserByID(stored); err != nil {
		t.Fatal(err)
	}
	target := addUserAt(t, env.um, "Spammer", 10, true)

	r := env.runCmd("BANUSER", env.caller, "", "\rY\r")
	if r.has("Access denied.") {
		t.Fatalf("users.ban refused:\n%s", r.text())
	}
	if !r.has("User banned: Spammer") {
		t.Errorf("output:\n%s", r.text())
	}
	if u, _ := env.um.GetUserByID(target.ID); u.AccessLevel != 0 || u.Validated {
		t.Errorf("not banned: %+v", u)
	}

	// Without the role, BANUSER is refused.
	stored.Roles = nil
	if err := env.um.UpdateUserByID(stored); err != nil {
		t.Fatal(err)
	}
	if r := env.runCmd("BANUSER", env.caller, "", "\r"); !r.has("Access denied.") {
		t.Errorf("BANUSER without users.ban:\n%s", r.text())
	}
}
