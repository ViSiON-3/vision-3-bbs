package perm

import (
	"fmt"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// Group is one user group as configs/roles.json writes it, under "groups":
// a name that ACS strings test with GROUP:name. Membership is on each user's
// record and grants no admin permission; a group only identifies people.
type Group struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// parseGroups reads the file's groups into r, returning a diagnostic for
// each one left out.
func (r *Roles) parseGroups(groups []Group) []Diagnostic {
	var diags []Diagnostic
	r.inGroup = map[string]bool{}
	for i, g := range groups {
		bad := func(format string, a ...any) {
			diags = append(diags, Diagnostic{Kind: "group", Index: i + 1, Role: g.Name, Message: fmt.Sprintf(format, a...)})
		}
		name := NormalizeRoleName(g.Name)
		if err := checkName("group", name); err != nil {
			bad("%v; the group is ignored", err)
			continue
		}
		if r.inGroup[name] {
			bad("another group is already named %s; this one is ignored", name)
			continue
		}
		r.inGroup[name] = true
		r.groups = append(r.groups, Group{Name: name, Description: strings.TrimSpace(g.Description)})
	}
	return diags
}

// GroupDefined reports whether a group named name exists, ignoring case.
func (r *Roles) GroupDefined(name string) bool {
	if r == nil {
		return false
	}
	return r.inGroup[NormalizeRoleName(name)]
}

// GroupNames is the defined groups' names, in file order.
func (r *Roles) GroupNames() []string {
	if r == nil {
		return nil
	}
	names := make([]string, len(r.groups))
	for i, g := range r.groups {
		names[i] = g.Name
	}
	return names
}

// Groups is the defined groups, in file order.
func (r *Roles) Groups() []Group {
	if r == nil {
		return nil
	}
	return append([]Group(nil), r.groups...)
}

// InGroup reports whether u's record lists the group name and roles defines
// it. Being a sysop doesn't put anyone in a group.
func InGroup(u *user.User, name string, roles *Roles) bool {
	if u == nil || u.DeletedUser {
		return false
	}
	if roles == nil {
		roles = Current()
	}
	name = NormalizeRoleName(name)
	if !roles.GroupDefined(name) {
		return false
	}
	for _, g := range NormalizeRoles(u.Groups) {
		if g == name {
			return true
		}
	}
	return false
}

// CheckGroupName is CheckRoleName for a group name.
func CheckGroupName(name string) error { return checkName("group", name) }

// ParseGroupList is ParseRoleList for groups.
func ParseGroupList(s string, roles *Roles) ([]string, error) {
	return parseNameList("group", s, roles.GroupDefined, roles.GroupNames())
}

// UndefinedGroups is the group names on a record that roles doesn't define,
// normalized.
func UndefinedGroups(names []string, roles *Roles) []string {
	var out []string
	for _, n := range NormalizeRoles(names) {
		if !roles.GroupDefined(n) {
			out = append(out, n)
		}
	}
	return out
}
