package perm

import (
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

func TestGroups(t *testing.T) {
	r, diags, err := Parse([]byte(`{"roles": [{"name": "ops", "permissions": ["users.validate"]}], "groups": [
		{"name": "FSXNET-OPS", "description": " network ops "},
		{"name": "fsxnet-ops"},
		{"name": "1bad"},
		{"name": "ops"}
	]}`))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		`group 2 ("fsxnet-ops"): another group is already named fsxnet-ops`,
		`group 3 ("1bad"): group name "1bad" must start with a letter`,
	}
	if len(diags) != len(want) {
		t.Fatalf("diagnostics = %v", diags)
	}
	for i, w := range want {
		if !strings.HasPrefix(diags[i].String(), w) {
			t.Errorf("diagnostic %d = %q, want prefix %q", i, diags[i], w)
		}
	}
	// A group and a role may share a name: they are separate.
	if got := strings.Join(r.GroupNames(), ","); got != "fsxnet-ops,ops" {
		t.Fatalf("GroupNames = %q", got)
	}
	if r.Groups()[0].Description != "network ops" {
		t.Errorf("description = %q", r.Groups()[0].Description)
	}

	member := &user.User{AccessLevel: 20, Groups: []string{"Fsxnet-Ops"}}
	if !InGroup(member, "FSXNET-OPS", r) || InGroup(member, "ops", r) {
		t.Error("InGroup wrong for a member")
	}
	if InGroup(&user.User{AccessLevel: 255}, "ops", r) {
		t.Error("a sysop is in a group they weren't put in")
	}
	if InGroup(&user.User{Groups: []string{"gone"}}, "gone", r) {
		t.Error("an undefined group matched")
	}
	if InGroup(&user.User{Groups: []string{"ops"}, DeletedUser: true}, "ops", r) {
		t.Error("a deleted user is in a group")
	}
	// Membership grants nothing.
	if Check(&user.User{AccessLevel: 20, Groups: []string{"ops"}}, UsersValidate, Scope{}, levels, r).Allowed {
		t.Error("being in group ops granted role ops's permission")
	}

	if got, err := ParseGroupList("ops, FSXNET-OPS", r); err != nil || strings.Join(got, ",") != "ops,fsxnet-ops" {
		t.Errorf("ParseGroupList = %v, %v", got, err)
	}
	if _, err := ParseGroupList("nope", r); err == nil || !strings.Contains(err.Error(), "no group named nope in configs/roles.json (defined: fsxnet-ops, ops)") {
		t.Errorf("undefined group: %v", err)
	}
	if _, err := ParseGroupList("x", &Roles{}); err == nil || !strings.Contains(err.Error(), "defines no groups") {
		t.Errorf("no groups: %v", err)
	}
	if got := UndefinedGroups([]string{"ops", "Gone"}, r); strings.Join(got, ",") != "gone" {
		t.Errorf("UndefinedGroups = %v", got)
	}
}
