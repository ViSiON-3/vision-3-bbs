package perm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

const sysOp = 255

func mustParse(t *testing.T, data string) *Roles {
	t.Helper()
	r, diags, err := Parse([]byte(data))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(diags) > 0 {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	return r
}

const validatorRoles = `{"roles": [
	{"name": "Validator", "description": "validates new users", "permissions": ["users.validate"]},
	{"name": "helper", "permissions": ["USERS.VALIDATE"]}
]}`

func TestCheckSysopHoldsEveryPermissionWithoutRoles(t *testing.T) {
	u := &user.User{AccessLevel: sysOp}
	d := Check(u, UsersValidate, sysOp, &Roles{})
	if !d.Allowed || !d.Sysop || d.Source() != "sysop level" {
		t.Fatalf("sysop decision = %+v (%q)", d, d.Source())
	}
	if Member(u, "validator", mustParse(t, validatorRoles)) {
		t.Fatal("a sysop's level must not make them a member of a role")
	}
}

func TestCheckRoleGrantsBelowSysop(t *testing.T) {
	roles := mustParse(t, validatorRoles)
	u := &user.User{AccessLevel: 20, Roles: []string{"VALIDATOR", "helper", "validator"}}
	d := Check(u, UsersValidate, sysOp, roles)
	if !d.Allowed || d.Sysop {
		t.Fatalf("decision = %+v", d)
	}
	if got := d.Source(); got != "roles helper, validator" {
		t.Fatalf("Source = %q", got)
	}
	if !Member(u, "Validator", roles) {
		t.Fatal("Member(validator) = false")
	}
}

func TestCheckDenies(t *testing.T) {
	roles := mustParse(t, validatorRoles)
	cases := map[string]*user.User{
		"nil user":       nil,
		"no roles":       {AccessLevel: 250},
		"undefined role": {AccessLevel: 20, Roles: []string{"validatr"}},
		"deleted member": {AccessLevel: 20, Roles: []string{"validator"}, DeletedUser: true},
		"deleted sysop":  {AccessLevel: sysOp, DeletedUser: true},
	}
	for name, u := range cases {
		if d := Check(u, UsersValidate, sysOp, roles); d.Allowed || d.Source() != "" {
			t.Errorf("%s: decision = %+v", name, d)
		}
	}
}

func TestCheckNilRolesReadsCurrent(t *testing.T) {
	t.Cleanup(func() { SetCurrent(nil) })
	u := &user.User{AccessLevel: 20, Roles: []string{"validator"}}
	if Check(u, UsersValidate, sysOp, nil).Allowed {
		t.Fatal("allowed before any roles were loaded")
	}
	SetCurrent(mustParse(t, validatorRoles))
	if !Check(u, UsersValidate, sysOp, nil).Allowed {
		t.Fatal("not allowed after SetCurrent")
	}
	SetCurrent(&Roles{})
	if Check(u, UsersValidate, sysOp, nil).Allowed {
		t.Fatal("still allowed after the role was removed")
	}
}

func TestParseDiagnosticsOnlyGrantLess(t *testing.T) {
	r, diags, err := Parse([]byte(`{"roles": [
		{"name": "ok", "permissions": ["users.validate", "users.validate"]},
		{"name": "9lives", "permissions": ["users.validate"]},
		{"name": "OK", "permissions": ["users.validate"]},
		{"name": "typo", "permissions": ["users.vaildate"]},
		{"name": "", "permissions": ["users.validate"]},
		{"name": "has space", "permissions": ["users.validate"]}
	]}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(r.Names(), ","); got != "ok,typo" {
		t.Fatalf("Names = %q", got)
	}
	if got := r.List()[0].Permissions; len(got) != 1 {
		t.Fatalf("duplicate permission kept: %v", got)
	}
	want := []string{
		`role 2 ("9lives"): role name "9lives" must start with a letter`,
		`role 3 ("OK"): another role is already named ok`,
		`role 4 ("typo"): "users.vaildate" is not a permission`,
		`role 4 ("typo"): grants no permissions`,
		`role 5 (""): a role needs a name`,
		`role 6 ("has space"): role name "has space" has ' ' in it`,
	}
	if len(diags) != len(want) {
		t.Fatalf("diagnostics = %v", diags)
	}
	for i, w := range want {
		if !strings.HasPrefix(diags[i].String(), w) {
			t.Errorf("diagnostic %d = %q, want prefix %q", i, diags[i], w)
		}
	}
	if Check(&user.User{AccessLevel: 1, Roles: []string{"typo"}}, UsersValidate, sysOp, r).Allowed {
		t.Fatal("a misspelt permission granted access")
	}
}

func TestParseRejectsBadJSON(t *testing.T) {
	if _, _, err := Parse([]byte(`{"roles": [`)); err == nil {
		t.Fatal("no error for truncated JSON")
	}
	r, diags, err := Parse([]byte("\xef\xbb\xbf  "))
	if err != nil || len(diags) > 0 || r.Len() != 0 {
		t.Fatalf("blank file: %v %v %d", err, diags, r.Len())
	}
}

func TestLoadMissingFileIsEmpty(t *testing.T) {
	dir := t.TempDir()
	r, diags, err := Load(dir)
	if err != nil || len(diags) > 0 || r.Len() != 0 {
		t.Fatalf("Load = %v %v %v", r, diags, err)
	}
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(validatorRoles), 0o600); err != nil {
		t.Fatal(err)
	}
	if r, _, err = Load(dir); err != nil || !r.Defined("VALIDATOR") {
		t.Fatalf("Load after write = %v, %v", r, err)
	}
}

func TestParseRoleList(t *testing.T) {
	roles := mustParse(t, validatorRoles)
	got, err := ParseRoleList(" Validator, helper validator ", roles)
	if err != nil || strings.Join(got, ",") != "validator,helper" {
		t.Fatalf("ParseRoleList = %v, %v", got, err)
	}
	if got, err := ParseRoleList("  ", roles); err != nil || got != nil {
		t.Fatalf("blank = %v, %v", got, err)
	}
	if _, err := ParseRoleList("validator, nope", roles); err == nil || !strings.Contains(err.Error(), "defined: validator, helper") {
		t.Fatalf("undefined role error = %v", err)
	}
	if _, err := ParseRoleList("nope", &Roles{}); err == nil || !strings.Contains(err.Error(), "defines no roles") {
		t.Fatalf("no roles error = %v", err)
	}
	if got := FormatRoleList([]string{"B", "a", "b"}); got != "b, a" {
		t.Fatalf("FormatRoleList = %q", got)
	}
	if got := Undefined([]string{"helper", "Gone"}, roles); strings.Join(got, ",") != "gone" {
		t.Fatalf("Undefined = %v", got)
	}
}
