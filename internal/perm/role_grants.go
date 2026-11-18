package perm

import (
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// RoleGrants is the roles on u's record: those it holds for good (Roles),
// in record order, then those given until a time (ExpiringRoles), by name,
// expired or not. A role in both counts as held for good.
func RoleGrants(u *user.User) []TimedGrant {
	if u == nil {
		return nil
	}
	return timedGrants(u.Roles, u.ExpiringRoles)
}

// ActiveRoles is the names of the roles on u's record that count at now:
// held for good, or given until a time after now.
func ActiveRoles(u *user.User, now time.Time) []string {
	var names []string
	for _, g := range RoleGrants(u) {
		if g.Live(now) {
			names = append(names, g.Name)
		}
	}
	return names
}

// RoleNamesOf is the names of every role on u's record, expired or not.
func RoleNamesOf(u *user.User) []string { return grantNames(RoleGrants(u)) }

// FormatRoleGrants is a record's roles as the user editors take them back:
// "msgops, validator=2027-01-01", "" for none.
func FormatRoleGrants(u *user.User, loc *time.Location) string {
	return formatGrants(RoleGrants(u), loc)
}

// DescribeRoles is a record's roles as the editors and the ACS tester show
// them at now: "msgops, validator until 2027-01-01", "" for none.
func DescribeRoles(u *user.User, loc *time.Location, now time.Time) string {
	return describeGrants(RoleGrants(u), loc, now)
}

// ParseRoleGrants reads a list of roles typed into a user editor, as
// FormatRoleGrants writes it: names separated by commas (or spaces), each
// with = and an expiry in loc to give it until then. Each name must be a
// role roles defines; an expiry must be after now unless u already has the
// role until that time (see parseGrants). A blank list is no roles.
//
// The result is the record's new Roles and ExpiringRoles.
func ParseRoleGrants(s string, roles *Roles, loc *time.Location, now time.Time, u *user.User) (permanent []string, expiring map[string]string, err error) {
	k := grantKind{kind: "role", check: CheckRoleName, defined: roles.Defined, all: roles.Names(), verb: "defines"}
	grants, err := parseGrants(s, k, loc, now, RoleGrants(u))
	if err != nil {
		return nil, nil, err
	}
	permanent, expiring = splitGrants(grants)
	return permanent, expiring, nil
}

// SameRoles reports whether two records' roles are the same grants with
// the same expiries, in any order.
func SameRoles(a, b *user.User) bool { return sameGrants(RoleGrants(a), RoleGrants(b)) }
