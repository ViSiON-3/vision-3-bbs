package menu

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/acs"
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

// TestUserEditor_AssignsGroups pins the sysop's [U] key, as [R] for roles.
func TestUserEditor_AssignsGroups(t *testing.T) {
	useRoles(t, `{"roles": [], "groups": [{"name": "fsxnet-ops"}]}`)
	env := newMenuEnv(t)
	r := env.runCmd("ADMINLISTUSERS", env.sysop, "", "j"+"u"+"opz\r"+"u"+"FSXNET-OPS\r"+"s"+"q")
	if r.err != nil {
		t.Fatalf("err = %v", r.err)
	}
	if !r.has("no group named opz in configs/roles.json (defined: fsxnet-ops)", "[U] Groups", "Changes saved for Caller.") {
		t.Errorf("output:\n%s", r.text())
	}
	u, _ := env.diskUser(2)
	if strings.Join(u.Groups, ",") != "fsxnet-ops" || len(u.Roles) != 0 {
		t.Fatalf("saved record: groups %v, roles %v", u.Groups, u.Roles)
	}
	if l, ok := modLogField(modAdminLog(t, env), 2, "groups"); !ok || l.NewValue != "fsxnet-ops" {
		t.Errorf("groups audit entry = %+v (found %v)", l, ok)
	}
	if !checkACS("GROUP:fsxnet-ops", u, nil, nil, time.Now()) {
		t.Error("GROUP: fails for the saved record")
	}
}

// TestUserEditor_GrantsAndRevokesNamedFlags pins the sysop's [L] key
// (#1074): an undeclared flag or a past expiry is refused as typed, a grant
// with an expiry passes FLAG: until that instant, and leaving a flag out
// revokes it.
func TestUserEditor_GrantsAndRevokesNamedFlags(t *testing.T) {
	useRoles(t, `{"roles": [], "flags": [{"name": "donor"}, {"name": "beta"}]}`)
	env := newMenuEnv(t)
	input := "j" +
		"l" + "donr\r" +
		"l" + "donor=2001-01-01\r" +
		"l" + "donor=2099-01-01, BETA\r" +
		"s" + "q"
	r := env.runCmd("ADMINLISTUSERS", env.sysop, "", input)
	if r.err != nil {
		t.Fatalf("err = %v", r.err)
	}
	if !r.has("no flag named donr in configs/roles.json (declared: donor, beta)", "already passed", "[L] Named Flags", "Changes saved for Caller.") {
		t.Errorf("output:\n%s", r.text())
	}
	u, _ := env.diskUser(2)
	expiry, err := time.Parse(time.RFC3339, u.NamedFlags["donor"])
	if err != nil || len(u.NamedFlags) != 2 || u.NamedFlags["beta"] != "" {
		t.Fatalf("saved flags = %v", u.NamedFlags)
	}
	if !expiry.Equal(time.Date(2099, 1, 1, 0, 0, 0, 0, boardLocation())) {
		t.Errorf("expiry = %v, want midnight 2099-01-01 on the board's clock", expiry)
	}
	if l, ok := modLogField(modAdminLog(t, env), 2, "namedflags"); !ok || l.OldValue != "" || l.NewValue != "beta, donor=2099-01-01" {
		t.Errorf("audit entry = %+v (found %v)", l, ok)
	}
	if !checkACS("FLAG:donor&FLAG:beta", u, nil, nil, time.Now()) {
		t.Error("FLAG: fails for the saved record")
	}
	// The same record, checked again at the expiry instant.
	at := acsContext(u, nil, time.Now())
	at.Now = expiry
	if acs.Check("FLAG:donor", at) || !acs.Check("FLAG:beta", at) {
		t.Error("FLAG:donor still passes at its expiry")
	}

	r = env.runCmd("ADMINLISTUSERS", env.sysop, "", "j"+"l"+strings.Repeat(ueBS, 40)+"beta\r"+"s"+"q")
	if r.err != nil {
		t.Fatalf("err = %v", r.err)
	}
	u, _ = env.diskUser(2)
	if _, kept := u.NamedFlags["donor"]; kept || len(u.NamedFlags) != 1 {
		t.Errorf("after revoking donor: %v\n%s", u.NamedFlags, r.text())
	}
}

// A value longer than the status row scrolls sideways in it rather than
// wrapping onto the rows below.
func TestUserEditor_LongFieldInputScrolls(t *testing.T) {
	useRoles(t, `{"roles": [], "flags": [{"name": "donor"}]}`)
	env := newMenuEnv(t)
	long := strings.Repeat("a", 120) + strings.Repeat("b", 30)
	r := env.runCmd("ADMINLISTUSERS", env.sysop, "", "j"+"l"+long+ueEsc+"q")
	if r.err != nil {
		t.Fatalf("err = %v", r.err)
	}
	// "Named Flags: " takes 13 columns of the 80, leaving 66 for the value
	// and the cursor after it.
	if strings.Contains(r.raw, strings.Repeat("a", 67)) {
		t.Errorf("more of the value than the row holds was drawn:\n%q", r.raw)
	}
	if !strings.Contains(r.raw, strings.Repeat("a", 35)+strings.Repeat("b", 30)) {
		t.Errorf("the end of the value isn't in view:\n%q", r.raw)
	}
}

// TestUserEditor_GivesRoleUntilATime pins [R] with an expiry (#1125): a
// past expiry is refused, a future one saved apart from the roles held for
// good, and the role stops granting at that instant.
func TestUserEditor_GivesRoleUntilATime(t *testing.T) {
	useRoles(t, validatorRole)
	env := newMenuEnv(t)
	r := env.runCmd("ADMINLISTUSERS", env.sysop, "", "j"+"r"+"validator=2001-01-01\r"+"r"+"Validator=2099-01-01\r"+"s"+"q")
	if r.err != nil {
		t.Fatalf("err = %v", r.err)
	}
	if !r.has("role validator: 2001-01-01 has already passed", "Changes saved for Caller.") {
		t.Errorf("output:\n%s", r.text())
	}
	u, _ := env.diskUser(2)
	expiry, err := time.Parse(time.RFC3339, u.ExpiringRoles["validator"])
	if err != nil || len(u.Roles) != 0 || !expiry.Equal(time.Date(2099, 1, 1, 0, 0, 0, 0, boardLocation())) {
		t.Fatalf("saved roles %v, expiring %v", u.Roles, u.ExpiringRoles)
	}
	if l, ok := modLogField(modAdminLog(t, env), 2, "roles"); !ok || l.NewValue != "validator=2099-01-01" {
		t.Errorf("audit entry = %+v (found %v)", l, ok)
	}
	at := acsContext(u, nil, time.Now())
	if !acs.Check("ROLE:validator&PERM:users.validate", at) {
		t.Error("the role doesn't count before its expiry")
	}
	at.Now = expiry
	if acs.Check("ROLE:validator", at) || acs.Check("PERM:users.validate", at) {
		t.Error("the role still counts at its expiry")
	}
}

// A validator whose role has expired is refused at the save, as one whose
// role was taken away is: the check reads the stored record and the clock.
func TestValidatorExpiredRoleStopsSave(t *testing.T) {
	useRoles(t, validatorRole)
	e, um := newUserEditorTestEnv(t)
	val := addUserAt(t, um, "Val", 20, true)
	pending := addUserAt(t, um, "Newbie", 1, false)
	for _, tt := range []struct {
		expiry string
		saved  bool
	}{
		{time.Now().Add(-time.Minute).UTC().Format(time.RFC3339), false},
		{time.Now().Add(time.Hour).UTC().Format(time.RFC3339), true},
	} {
		stored, _ := um.GetUserByID(val.ID)
		stored.ExpiringRoles = map[string]string{"validator": tt.expiry}
		if err := um.UpdateUserByID(stored); err != nil {
			t.Fatal(err)
		}
		target, _ := um.GetUserByID(pending.ID)
		msg, saved := e.applyPendingUserChanges(um, val, target, map[string]interface{}{"validated": true}, map[int]time.Time{target.ID: target.UpdatedAt})
		if saved != tt.saved {
			t.Errorf("expiry %s: saved = %v (%q), want %v", tt.expiry, saved, msg, tt.saved)
		}
		if !saved && msg != msgAdminNoLongerAllowed {
			t.Errorf("expiry %s: msg = %q", tt.expiry, msg)
		}
	}
}
