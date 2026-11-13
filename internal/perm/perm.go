// Package perm decides who may perform an admin action below sysop level.
//
// A permission is an action the BBS defines, such as users.validate. A role
// is a named set of permissions the sysop defines in configs/roles.json, and
// a user holds the roles listed on their record. A user may perform an action
// when their access level holds it (sysOpLevel holds every permission,
// coSysOpLevel the ones co-sysops always had), when they sponsor the message
// area a scoped permission is asked about, or when one of their roles grants
// it there. A role can grant a scoped permission (messages.moderate in
// message areas, files.manage in file areas) in some areas or conferences
// only.
//
// Roles only ever add: there are no deny rules and no inheritance, and a
// board with no roles behaves exactly as one without this package. Holding a
// permission is not membership in a role: a sysop holds every permission and
// is a member of no role they aren't assigned. Co-sysops and area sponsors
// keep the powers their level and sponsorship already give them; a role
// neither replaces nor widens those, and neither a role nor a sponsorship
// reaches private mail.
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
	// MessagesModerate deletes any message in a message area from the
	// reader. A role can grant it in some areas or conferences only, and it
	// never covers private mail through a role or a sponsorship.
	MessagesModerate Permission = "messages.moderate"
	// FilesManage edits, deletes, moves and renames the files in a file
	// area, and their descriptions, including the review queue. A role can
	// grant it in some file areas or conferences only; a move needs it in
	// both areas. It gives nothing over the areas themselves (their ACS,
	// path or name), which stay with the sysop.
	FilesManage Permission = "files.manage"
)

// AreaKind is the kind of area a scoped permission applies in, which is what
// a grant's "areas" tags name.
type AreaKind int

const (
	// MessageAreas are configs/message_areas.json's areas.
	MessageAreas AreaKind = iota
	// FileAreas are configs/file_areas.json's areas.
	FileAreas
)

// String is the kind as the documentation and messages name it.
func (k AreaKind) String() string {
	if k == FileAreas {
		return "file area"
	}
	return "message area"
}

// Level is the access level that holds a permission without a role.
type Level int

const (
	// SysOpLevel is sysOpLevel in config.json.
	SysOpLevel Level = iota
	// CoSysOpLevel is coSysOpLevel in config.json: what co-sysops could
	// already do before roles existed.
	CoSysOpLevel
)

// Info describes a permission, for diagnostics and the documentation.
type Info struct {
	Name    Permission
	Summary string
	// Level is the lowest access level that holds the permission whatever
	// the user's roles.
	Level Level
	// Scoped permissions apply in an area of kind Kind: a role can grant
	// them in some areas or conferences only, and a message area's sponsor
	// holds them in that area.
	Scoped bool
	Kind   AreaKind
}

// registry is every permission the BBS defines, in the order they are listed.
var registry = []Info{
	{UsersValidate, "review accounts awaiting validation and approve them with the board's validation defaults", SysOpLevel, false, MessageAreas},
	{MessagesModerate, "delete any message in a message area (not private mail)", CoSysOpLevel, true, MessageAreas},
	{FilesManage, "edit, delete, move and rename the files in a file area, and review uploads there", CoSysOpLevel, true, FileAreas},
}

// Known lists the permissions the BBS defines.
func Known() []Info { return append([]Info(nil), registry...) }

// KnownNames is Known's names, for messages: "users.validate, ...".
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

// info is p's registry entry; a permission not in the registry is held by
// sysop level only and isn't scoped.
func info(p Permission) Info {
	for _, i := range registry {
		if i.Name == p {
			return i
		}
	}
	return Info{Name: p, Level: SysOpLevel}
}

// IsScoped reports whether p applies in an area.
func IsScoped(p Permission) bool { return info(p).Scoped }

// KindOf is the kind of area a scoped permission p applies in.
func KindOf(p Permission) AreaKind { return info(p).Kind }

// LevelOf is the access level that holds p without a role.
func LevelOf(p Permission) Level { return info(p).Level }

// Levels are config.json's sysOpLevel and coSysOpLevel.
type Levels struct {
	SysOp, CoSysOp int
}

// holds reports whether level is at or above l's threshold. A co-sysop
// threshold of 0 or less is taken as unset, not as everyone.
func (lv Levels) holds(level int, l Level) bool {
	if level >= lv.SysOp {
		return true
	}
	return l == CoSysOpLevel && lv.CoSysOp > 0 && level >= lv.CoSysOp
}

// PrivateMailTag is the private mail area's tag. A message-area permission
// never reaches it through a role or a sponsorship: private mail has no
// moderator.
const PrivateMailTag = "PRIVMAIL"

// Scope is the area a scoped permission is asked about, of the permission's
// kind: its tag, its conference's tag, and for a message area its
// sponsor's account ID (0 for none; the sponsor is an account, not a
// handle, #1020). The zero Scope is no area, which only a grant for every
// area, a level, covers.
type Scope struct {
	Area, Conference string
	SponsorID        int
}

// private reports whether s is the private mail area.
func (s Scope) private() bool { return strings.EqualFold(s.Area, PrivateMailTag) }

// Decision is whether a user holds a permission, and where it came from.
type Decision struct {
	Permission Permission
	Allowed    bool
	// Level is set when the user's access level holds the permission:
	// sysOpLevel, which holds every permission, or coSysOpLevel for those
	// co-sysops hold.
	Level   bool
	LevelOf Level
	// Sponsor is set when the user sponsors the area asked about.
	Sponsor bool
	Area    string
	// Roles are the user's roles that grant the permission, sorted.
	Roles []string
}

// Source says where an allowed decision's grant came from, for the ACS
// tester and the audit log: "sysop level", "co-sysop level", "sponsor of
// GENERAL", "role validator", "roles a, b". It is "" for a decision that
// isn't allowed.
func (d Decision) Source() string {
	if !d.Allowed {
		return ""
	}
	if d.Level {
		if d.LevelOf == CoSysOpLevel {
			return "co-sysop level"
		}
		return "sysop level"
	}
	var parts []string
	if d.Sponsor {
		parts = append(parts, "sponsor of "+d.Area)
	}
	switch len(d.Roles) {
	case 0:
	case 1:
		parts = append(parts, "role "+d.Roles[0])
	default:
		parts = append(parts, "roles "+strings.Join(d.Roles, ", "))
	}
	return strings.Join(parts, " and ")
}

// Check decides whether u holds p in the area at describes (the zero Scope
// for a permission that isn't scoped). A deleted account holds nothing. A
// level at or above the permission's Level holds it everywhere; below that,
// an area's sponsor holds a scoped permission there, and each role on the
// record that roles defines and that grants p where at is, is a source. A
// role the record names but roles.json doesn't define grants nothing, and
// neither a role nor a sponsorship reaches private mail.
//
// Callers that guard a write pass the stored record, read just before the
// write, so a role or level taken away mid-session stops the next protected
// action rather than the next call.
func Check(u *user.User, p Permission, at Scope, lv Levels, roles *Roles) Decision {
	d := Decision{Permission: p, Area: at.Area}
	if u == nil || u.DeletedUser {
		return d
	}
	pi := info(p)
	if lv.holds(u.AccessLevel, pi.Level) {
		d.Allowed, d.Level = true, true
		d.LevelOf = SysOpLevel
		if u.AccessLevel < lv.SysOp {
			d.LevelOf = CoSysOpLevel
		}
		return d
	}
	if pi.Scoped && pi.Kind == MessageAreas && at.private() {
		return d
	}
	if pi.Scoped && pi.Kind == MessageAreas && at.SponsorID > 0 && at.SponsorID == u.ID {
		d.Sponsor = true
	}
	if roles == nil {
		roles = Current()
	}
	for _, name := range NormalizeRoles(u.Roles) {
		if roles.grants(name, p, at) {
			d.Roles = append(d.Roles, name)
		}
	}
	sort.Strings(d.Roles)
	d.Allowed = d.Sponsor || len(d.Roles) > 0
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
