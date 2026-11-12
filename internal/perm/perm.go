// Package perm decides who may perform an admin action below sysop level.
//
// A permission is an action the BBS defines, such as users.validate. A role
// is a named set of permissions the sysop defines in configs/roles.json, and
// a user holds the roles listed on their record. A user may perform an action
// when they are a sysop (access level at or above sysOpLevel), who holds
// every permission, or when one of their roles grants it.
//
// Roles only ever add: there are no deny rules and no inheritance, and a
// board with no roles behaves exactly as one without this package. Holding a
// permission is not membership in a role: a sysop holds every permission and
// is a member of no role they aren't assigned. Co-sysops and area sponsors
// keep the powers their level and sponsorship already give them; a role
// neither replaces nor widens those.
//
// Check is the one evaluator: the admin handlers, the ACS terms ROLE: and
// PERM:, and the ACS tester all go through it, so a menu that offers an
// action and the handler that performs it can't disagree.
package perm

import (
	"sort"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// Permission is an admin action a role can grant.
type Permission string

// The permissions the BBS defines.
const (
	// UsersValidate reviews accounts awaiting validation and approves them
	// with the board's validation defaults (regularUserLevel). It gives no
	// other change to an account: not its level or flags, roles, password,
	// a ban or a deletion.
	UsersValidate Permission = "users.validate"
)

// Info describes a permission, for diagnostics and the documentation.
type Info struct {
	Name    Permission
	Summary string
}

// registry is every permission the BBS defines, in the order they are listed.
var registry = []Info{
	{UsersValidate, "review accounts awaiting validation and approve them with the board's validation defaults"},
}

// Known lists the permissions the BBS defines.
func Known() []Info { return append([]Info(nil), registry...) }

// KnownNames is Known's names, for messages: "users.validate".
func KnownNames() string {
	names := make([]string, len(registry))
	for i, p := range registry {
		names[i] = string(p.Name)
	}
	return strings.Join(names, ", ")
}

// Lookup finds the permission named name, ignoring case and surrounding
// space.
func Lookup(name string) (Permission, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	for _, p := range registry {
		if string(p.Name) == name {
			return p.Name, true
		}
	}
	return "", false
}

// Decision is whether a user holds a permission, and where it came from.
type Decision struct {
	Permission Permission
	Allowed    bool
	// Sysop is set when the user's level is at or above sysOpLevel, which
	// holds every permission.
	Sysop bool
	// Roles are the user's roles that grant the permission, sorted.
	Roles []string
}

// Source says where an allowed decision's grant came from, for the ACS
// tester and the audit log: "sysop level", "role validator", "roles a, b".
// It is "" for a decision that isn't allowed.
func (d Decision) Source() string {
	switch {
	case !d.Allowed:
		return ""
	case d.Sysop:
		return "sysop level"
	case len(d.Roles) == 1:
		return "role " + d.Roles[0]
	default:
		return "roles " + strings.Join(d.Roles, ", ")
	}
}

// Check decides whether u holds p. A deleted account holds nothing. A level
// at or above sysOpLevel holds every permission; otherwise each role on the
// record that roles defines and that grants p is a source. A role the record
// names but roles.json doesn't define grants nothing.
//
// Callers that guard a write pass the stored record, read just before the
// write, so a role or level taken away mid-session stops the next protected
// action rather than the next call.
func Check(u *user.User, p Permission, sysOpLevel int, roles *Roles) Decision {
	d := Decision{Permission: p}
	if u == nil || u.DeletedUser {
		return d
	}
	if u.AccessLevel >= sysOpLevel {
		d.Allowed, d.Sysop = true, true
		return d
	}
	if roles == nil {
		roles = Current()
	}
	for _, name := range NormalizeRoles(u.Roles) {
		if roles.grants(name, p) {
			d.Roles = append(d.Roles, name)
		}
	}
	sort.Strings(d.Roles)
	d.Allowed = len(d.Roles) > 0
	return d
}

// Member reports whether u's record lists the role name and roles defines
// it. Being a sysop doesn't make anyone a member.
func Member(u *user.User, name string, roles *Roles) bool {
	if u == nil || u.DeletedUser {
		return false
	}
	if roles == nil {
		roles = Current()
	}
	name = NormalizeRoleName(name)
	if !roles.Defined(name) {
		return false
	}
	for _, r := range NormalizeRoles(u.Roles) {
		if r == name {
			return true
		}
	}
	return false
}
