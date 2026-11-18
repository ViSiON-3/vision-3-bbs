package perm

import (
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

func TestExpiringRoles(t *testing.T) {
	r, _, err := Parse([]byte(`{"roles": [
		{"name": "validator", "permissions": ["users.validate"]},
		{"name": "msgops", "permissions": ["users.edit"]}
	]}`))
	if err != nil {
		t.Fatal(err)
	}
	expiry := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	u := &user.User{AccessLevel: 20, Roles: []string{"msgops"}, ExpiringRoles: map[string]string{"Validator": expiry.Format(time.RFC3339)}}
	before := expiry.Add(-time.Second)

	if d := CheckAt(u, UsersValidate, Scope{}, levels, r, before); !d.Allowed || d.Source() != "role validator" {
		t.Errorf("before the expiry: %+v", d)
	}
	if d := CheckAt(u, UsersValidate, Scope{}, levels, r, expiry); d.Allowed {
		t.Errorf("at the expiry instant: %+v", d)
	}
	if !CheckAt(u, UsersEdit, Scope{}, levels, r, expiry.AddDate(10, 0, 0)).Allowed {
		t.Error("a role held for good stopped")
	}
	if !MemberAt(u, "validator", r, before) || MemberAt(u, "validator", r, expiry) || !MemberAt(u, "msgops", r, expiry) {
		t.Error("MemberAt wrong around the expiry")
	}
	if got := strings.Join(ActiveRoles(u, expiry), ","); got != "msgops" {
		t.Errorf("ActiveRoles at the expiry = %q", got)
	}
	if got := strings.Join(RoleNamesOf(u), ","); got != "msgops,validator" {
		t.Errorf("RoleNamesOf = %q", got)
	}
	// A role held for good and until a time is held for good.
	both := &user.User{Roles: []string{"validator"}, ExpiringRoles: map[string]string{"validator": "2001-01-01T00:00:00Z"}}
	if g := RoleGrants(both); len(g) != 1 || !g[0].Live(expiry) {
		t.Errorf("RoleGrants for a role in both = %+v", g)
	}
	// An unreadable expiry, or the zero instant, never counts.
	for _, stored := range []string{"soon", "0001-01-01T00:00:00Z"} {
		bad := &user.User{AccessLevel: 20, ExpiringRoles: map[string]string{"validator": stored}}
		if CheckAt(bad, UsersValidate, Scope{}, levels, r, before).Allowed || !RoleGrants(bad)[0].Bad {
			t.Errorf("expiry %q granted", stored)
		}
	}
}

func TestRoleGrantListRoundTrip(t *testing.T) {
	r, _, _ := Parse([]byte(`{"roles": [
		{"name": "validator", "permissions": ["users.validate"]},
		{"name": "msgops", "permissions": ["users.edit"]}
	]}`))
	loc := time.FixedZone("PST", -8*3600)
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, loc)
	u := &user.User{Roles: []string{"msgops"}}

	roles, expiring, err := ParseRoleGrants("msgops validator=2027-01-01", r, loc, now, u)
	if err == nil {
		t.Fatalf("a space before = was read: %v %v", roles, expiring)
	}
	roles, expiring, err = ParseRoleGrants("MsgOps, Validator=2027-01-01", r, loc, now, u)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(roles, ",") != "msgops" || expiring["validator"] != "2027-01-01T08:00:00Z" || len(expiring) != 1 {
		t.Fatalf("ParseRoleGrants = %v %v", roles, expiring)
	}
	u.Roles, u.ExpiringRoles = roles, expiring
	if s := FormatRoleGrants(u, loc); s != "msgops, validator=2027-01-01" {
		t.Errorf("FormatRoleGrants = %q", s)
	}
	later := time.Date(2027, 2, 1, 0, 0, 0, 0, loc)
	if d := DescribeRoles(u, loc, later); d != "msgops, validator expired 2027-01-01" {
		t.Errorf("DescribeRoles = %q", d)
	}
	// Sent back unchanged after the expiry, the expired grant is kept.
	roles, expiring, err = ParseRoleGrants(FormatRoleGrants(u, loc), r, loc, later, u)
	if err != nil || !SameRoles(&user.User{Roles: roles, ExpiringRoles: expiring}, u) {
		t.Errorf("round trip = %v %v %v", roles, expiring, err)
	}
	// Taking the expiry off holds the role for good.
	roles, expiring, err = ParseRoleGrants("msgops, validator", r, loc, later, u)
	if err != nil || strings.Join(roles, ",") != "msgops,validator" || expiring != nil {
		t.Errorf("no expiry = %v %v %v", roles, expiring, err)
	}
	if _, _, err := ParseRoleGrants("validator=2027-01-15", r, loc, later, u); err == nil || !strings.Contains(err.Error(), "role validator: 2027-01-15 has already passed") {
		t.Errorf("past expiry: %v", err)
	}
	if _, _, err := ParseRoleGrants("nope", r, loc, now, u); err == nil || !strings.Contains(err.Error(), "no role named nope in configs/roles.json (defined: validator, msgops)") {
		t.Errorf("undefined: %v", err)
	}
	if _, _, err := ParseRoleGrants("x", &Roles{}, loc, now, u); err == nil || !strings.Contains(err.Error(), "defines no roles") {
		t.Errorf("no roles: %v", err)
	}
	if roles, expiring, err := ParseRoleGrants(" ", r, loc, now, u); err != nil || roles != nil || expiring != nil {
		t.Errorf("blank = %v %v %v", roles, expiring, err)
	}
}
