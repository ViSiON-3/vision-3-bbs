package perm

import (
	"fmt"
	"strings"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// Flag is one named flag as configs/roles.json writes it, under "flags": a
// name that ACS strings test with FLAG:name. A flag only marks a user for
// ACS; unlike a role it grants no admin permission, so gating a menu on
// FLAG:validator doesn't open the validation queue (users.validate does).
//
// A grant is on each user's record (user.User.NamedFlags) with an optional
// expiry, after which FLAG:name stops passing for that user.
type Flag struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// reservedFlagNames can't be declared as flags: the ACS term names, lower
// case, so a flag name never reads as a term. internal/acs's tests check
// that every term name it reads is here.
var reservedFlagNames = map[string]bool{
	"sysop": true, "cosysop": true,
	"level": true, "user": true, "filepoints": true, "uploadspercall": true,
	"uldlratio": true, "timeleft": true, "hour": true, "weekday": true,
	"msgconf": true, "fileconf": true, "flag": true, "note": true, "time": true,
	"validated": true, "local": true, "terminal": true,
	"calls": true, "posts": true, "uploads": true, "downloads": true, "age": true,
	"realname": true,
	"role":     true, "perm": true, "group": true,
	"node": true, "conn": true, "enc": true, "width": true, "ip": true,
	"month": true, "date": true,
}

// ReservedFlagName reports whether name (any case) is one a flag can't take.
func ReservedFlagName(name string) bool { return reservedFlagNames[NormalizeRoleName(name)] }

// CheckFlagName reports what is wrong with a named flag's name, nil for one
// that can be used: a role name's characters, at least two of them, so it
// never shadows a legacy one-letter flag (FLAG:A), and not an ACS term name.
func CheckFlagName(name string) error {
	if err := checkName("flag", name); err != nil {
		return err
	}
	if len(name) < 2 {
		return fmt.Errorf("flag name %q is one character, which is the legacy flag %s; a named flag needs two or more", name, strings.ToUpper(name))
	}
	if ReservedFlagName(name) {
		return fmt.Errorf("flag name %q is reserved: it is an ACS term name", name)
	}
	return nil
}

// parseFlags reads the file's flags into r, returning a diagnostic for each
// one left out.
func (r *Roles) parseFlags(flags []Flag) []Diagnostic {
	var diags []Diagnostic
	r.inFlag = map[string]bool{}
	for i, f := range flags {
		bad := func(format string, a ...any) {
			diags = append(diags, Diagnostic{Kind: "flag", Index: i + 1, Role: f.Name, Message: fmt.Sprintf(format, a...)})
		}
		name := NormalizeRoleName(f.Name)
		if err := CheckFlagName(name); err != nil {
			bad("%v; the flag is ignored", err)
			continue
		}
		if r.inFlag[name] {
			bad("another flag is already named %s; this one is ignored", name)
			continue
		}
		r.inFlag[name] = true
		r.flags = append(r.flags, Flag{Name: name, Description: strings.TrimSpace(f.Description)})
	}
	return diags
}

// FlagDefined reports whether a named flag called name is declared,
// ignoring case.
func (r *Roles) FlagDefined(name string) bool {
	if r == nil {
		return false
	}
	return r.inFlag[NormalizeRoleName(name)]
}

// FlagNames is the declared flags' names, in file order.
func (r *Roles) FlagNames() []string {
	if r == nil {
		return nil
	}
	names := make([]string, len(r.flags))
	for i, f := range r.flags {
		names[i] = f.Name
	}
	return names
}

// Flags is the declared flags, in file order.
func (r *Roles) Flags() []Flag {
	if r == nil {
		return nil
	}
	return append([]Flag(nil), r.flags...)
}

// FlagGrants is the named flags on u's record, sorted by name. A record
// that lists a flag twice under different cases keeps the grant that lasts
// longest.
func FlagGrants(u *user.User) []TimedGrant {
	if u == nil {
		return nil
	}
	return timedGrants(nil, u.NamedFlags)
}

// FlagNamesOf is the named flags on u's record, expired or not, sorted.
func FlagNamesOf(u *user.User) []string { return grantNames(FlagGrants(u)) }

// HasFlag reports whether u's record grants the named flag name, roles
// declares it, and the grant hasn't expired at now. Being a sysop doesn't
// set anyone's flags.
func HasFlag(u *user.User, name string, roles *Roles, now time.Time) bool {
	if u == nil || u.DeletedUser {
		return false
	}
	if roles == nil {
		roles = Current()
	}
	name = NormalizeRoleName(name)
	return roles.FlagDefined(name) && liveGrant(FlagGrants(u), name, now)
}

// UndefinedFlags is the flag names in names that roles doesn't declare,
// normalized.
func UndefinedFlags(names []string, roles *Roles) []string {
	var out []string
	for _, n := range NormalizeRoles(names) {
		if !roles.FlagDefined(n) {
			out = append(out, n)
		}
	}
	return out
}

// FormatFlagList is a record's named flags as the user editors take them
// back: "beta, donor=2027-01-01", "" for none.
func FormatFlagList(u *user.User, loc *time.Location) string {
	return formatGrants(FlagGrants(u), loc)
}

// DescribeFlags is a record's named flags as the editors and the ACS tester
// show them at now: "beta, donor until 2027-01-01, old expired 2026-10-01",
// "" for none.
func DescribeFlags(u *user.User, loc *time.Location, now time.Time) string {
	return describeGrants(FlagGrants(u), loc, now)
}

// flagKind is the named flags roles declares, for parseGrants.
func flagKind(roles *Roles) grantKind {
	return grantKind{kind: "flag", check: CheckFlagName, defined: roles.FlagDefined, all: roles.FlagNames(), verb: "declares"}
}

// ParseFlagList reads a list of named flags typed into a user editor, as
// FormatFlagList writes it: "beta, donor=2027-01-01", each name declared
// in roles, each expiry in loc (see parseGrants). A blank list is no flags.
// The result is the record's new NamedFlags.
func ParseFlagList(s string, roles *Roles, loc *time.Location, now time.Time, u *user.User) (map[string]string, error) {
	grants, err := parseGrants(s, flagKind(roles), loc, now, FlagGrants(u))
	if err != nil || len(grants) == 0 {
		return nil, err
	}
	out := make(map[string]string, len(grants))
	for _, g := range grants {
		out[g.Name] = g.Stored
	}
	return out, nil
}

// SameFlags reports whether two NamedFlags maps grant the same flags with
// the same stored expiries, names compared normalized.
func SameFlags(a, b map[string]string) bool {
	return sameGrants(timedGrants(nil, a), timedGrants(nil, b))
}
