package acs

import (
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/perm"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

func testRoles(t *testing.T) *perm.Roles {
	t.Helper()
	r, diags, err := perm.Parse([]byte(`{"roles": [
		{"name": "validator", "permissions": ["users.validate"]},
		{"name": "msg_ops", "permissions": ["users.validate"]},
		{"name": "greeter-2", "permissions": ["users.validate"]}
	]}`))
	if err != nil || len(diags) > 0 {
		t.Fatalf("roles: %v %v", err, diags)
	}
	return r
}

func roleCtx(t *testing.T, u *user.User) Context {
	ctx := ctxFor(u)
	ctx.Roles = testRoles(t)
	return ctx
}

func TestRoleAndPermTerms(t *testing.T) {
	member := &user.User{ID: 2, AccessLevel: 20, Roles: []string{"Validator", "greeter-2"}}
	plain := &user.User{ID: 3, AccessLevel: 250}
	sysop := &user.User{ID: 1, AccessLevel: oracleSysOp}
	tests := []struct {
		acs  string
		u    *user.User
		want bool
	}{
		{"ROLE:validator", member, true},
		{"role:VALIDATOR", member, true},
		{"ROLE:greeter-2", member, true},
		{"ROLE:msg_ops", member, false},
		{"!ROLE:msg_ops", member, true},
		{"ROLE:validator", plain, false},
		// A sysop holds every permission without being a member of any role.
		{"ROLE:validator", sysop, false},
		{"PERM:users.validate", sysop, true},
		{"PERM:USERS.VALIDATE", member, true},
		// A co-sysop holds no permission a role doesn't give them.
		{"PERM:users.validate", plain, false},
		{"!PERM:users.validate", plain, true},
		{"S20&PERM:users.validate", member, true},
		{"(ROLE:msg_ops|ROLE:validator)&V", &user.User{AccessLevel: 20, Roles: []string{"msg_ops"}, Validated: true}, true},
	}
	for _, tt := range tests {
		ctx := roleCtx(t, tt.u)
		if got := Check(tt.acs, ctx); got != tt.want {
			t.Errorf("Check(%q) for %+v = %v, want %v", tt.acs, tt.u, got, tt.want)
		}
		if x := Explain(tt.acs, ctx); x.Result != tt.want {
			t.Errorf("Explain(%q) = %v, want %v", tt.acs, x.Result, tt.want)
		} else if x.Root != nil {
			checkSteps(t, tt.acs, x.Root, ctx)
		}
	}
}

// An undefined role fails the whole string, so a misspelling can't pass
// through '!' or the other side of '|'.
func TestUndefinedRoleFailsWholeString(t *testing.T) {
	u := &user.User{AccessLevel: oracleSysOp, Roles: []string{"validatr"}}
	for _, s := range []string{"ROLE:validatr", "!ROLE:validatr", "SYSOP|ROLE:validatr", "!(ROLE:validatr&S10)"} {
		ctx := roleCtx(t, u)
		if Check(s, ctx) {
			t.Errorf("Check(%q) passed", s)
		}
		x := Explain(s, ctx)
		if x.Result || x.Reason != Undefined {
			t.Errorf("Explain(%q) = %v, %v", s, x.Result, x.Reason)
		}
	}
	x := Explain("!ROLE:validatr", roleCtx(t, u))
	lines := strings.Join(Render(x, 0), "\n")
	for _, want := range []string{"FAIL: a part names a role configs/roles.json doesn't define", "no role named validatr (defined: validator, msg_ops, greeter-2)", "~"} {
		if !strings.Contains(lines, want) {
			t.Errorf("render lacks %q:\n%s", want, lines)
		}
	}
}

func TestRolePermDiagnostics(t *testing.T) {
	tests := map[string]string{
		"PERM:users.vaildate": `"users.vaildate" is not a permission (known: users.validate, messages.moderate)`,
		"PERM:":               "needs a permission name",
		"ROLE:9x":             "must start with a letter",
		"ROLE:":               "a role needs a name",
		"ROLE:a.b":            `has '.' in it`,
	}
	for s, want := range tests {
		diags := Validate(s)
		if len(diags) == 0 || !strings.Contains(diags[0].Message, want) {
			t.Errorf("Validate(%q) = %v, want %q", s, diags, want)
		}
		if Check(s, roleCtx(t, &user.User{AccessLevel: oracleSysOp})) {
			t.Errorf("Check(%q) passed for a sysop", s)
		}
	}
	// '.' and '_' are term characters now; elsewhere they still fail.
	if d := Validate("S20.5"); len(d) == 0 {
		t.Error("S20.5 validated")
	}
}

func TestExplainPermSources(t *testing.T) {
	tests := []struct {
		u    *user.User
		want string
	}{
		{&user.User{AccessLevel: oracleSysOp}, "has users.validate: level 255 is at least 255 (sysOpLevel), which holds every permission"},
		{&user.User{AccessLevel: 20, Roles: []string{"validator", "msg_ops"}}, "has users.validate through roles msg_ops, validator"},
		{&user.User{AccessLevel: 20, Roles: []string{"gone"}}, "doesn't have users.validate: level 20 is below 255 (sysOpLevel) and no role grants it (roles: gone)"},
	}
	for _, tt := range tests {
		x := Explain("PERM:users.validate", roleCtx(t, tt.u))
		if x.Root == nil || x.Root.Detail != tt.want {
			t.Errorf("detail = %+v, want %q", x.Root, tt.want)
		}
	}
	x := Explain("ROLE:validator", roleCtx(t, &user.User{AccessLevel: oracleSysOp}))
	if x.Root == nil || !strings.Contains(x.Root.Detail, "a sysop's level doesn't make them a member") {
		t.Errorf("sysop ROLE detail = %+v", x.Root)
	}
}

func TestRoleNames(t *testing.T) {
	e := Parse("ROLE:B|(ROLE:a&!ROLE:b)|PERM:users.validate")
	if got := strings.Join(e.RoleNames(), ","); got != "b,a" {
		t.Fatalf("RoleNames = %q", got)
	}
}

func TestTesterCarriesRoles(t *testing.T) {
	roles := testRoles(t)
	u := &user.User{AccessLevel: 20, Roles: []string{"validator"}}
	x := Tester{SysOpLevel: oracleSysOp, Roles: roles}.Explain("ROLE:validator", u)
	if !x.Result {
		t.Fatalf("tester result = %+v", x)
	}
	if lines := strings.Join(Render(x, 0), "\n"); !strings.Contains(lines, "Roles:      validator") {
		t.Errorf("render lacks the roles line:\n%s", lines)
	}
}

func TestExplainScopedPerm(t *testing.T) {
	roles, _, err := perm.Parse([]byte(`{"roles": [{"name": "gen", "permissions": [{"permission": "messages.moderate", "areas": ["GENERAL"]}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	u := &user.User{AccessLevel: 20, Roles: []string{"gen"}, CurrentMessageAreaTag: "GENERAL"}
	for _, tt := range []struct {
		area, want string
		pass       bool
	}{
		{"GENERAL", "has messages.moderate in GENERAL through role gen", true},
		{"OTHER", "doesn't have messages.moderate in OTHER: level 20 is below 250 (coSysOpLevel) and no role grants it (roles: gen)", false},
		{"PRIVMAIL", "doesn't have messages.moderate in PRIVMAIL: below its level, nothing grants it in private mail", false},
	} {
		u.CurrentMessageAreaTag = tt.area
		ctx := ctxFor(u)
		ctx.Roles = roles
		x := Explain("PERM:messages.moderate", ctx)
		if x.Result != tt.pass || x.Root == nil || x.Root.Detail != tt.want {
			t.Errorf("%s: %v %+v, want %q", tt.area, x.Result, x.Root, tt.want)
		}
	}
	// The BBS's own scope (with the area's sponsor) wins over the record's tags.
	ctx := ctxFor(&user.User{Handle: "Guru", AccessLevel: 20})
	ctx.Roles = roles
	ctx.Scope = func() perm.Scope { return perm.Scope{Area: "TECH", Sponsor: "guru"} }
	if !Check("PERM:messages.moderate", ctx) {
		t.Error("a sponsor through Context.Scope fails PERM:messages.moderate")
	}
}
