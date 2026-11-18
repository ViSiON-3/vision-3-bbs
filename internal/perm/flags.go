package perm

import (
	"fmt"
	"sort"
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

// FlagGrant is one named flag on a user's record.
type FlagGrant struct {
	// Name is the flag's name, normalized.
	Name string
	// Expires is the instant the grant stops passing; zero is never.
	Expires time.Time
	// Stored is the expiry as the record has it, "" for none.
	Stored string
	// Bad is set when Stored isn't an RFC 3339 instant: the grant never
	// passes, and doctor reports it.
	Bad bool
}

// Live reports whether the grant passes at now: now is before its expiry,
// or it has none. A grant whose expiry can't be read never passes.
func (g FlagGrant) Live(now time.Time) bool {
	return !g.Bad && (g.Expires.IsZero() || now.Before(g.Expires))
}

// FlagGrants is the named flags on u's record, sorted by name. A record
// that lists a flag twice under different cases keeps the grant that lasts
// longest.
func FlagGrants(u *user.User) []FlagGrant {
	if u == nil {
		return nil
	}
	byName := map[string]FlagGrant{}
	for raw, stored := range u.NamedFlags {
		name := NormalizeRoleName(raw)
		if name == "" {
			continue
		}
		g := FlagGrant{Name: name, Stored: strings.TrimSpace(stored)}
		if g.Stored != "" {
			t, err := time.Parse(time.RFC3339, g.Stored)
			// The zero instant would read as "never": it is no expiry anyone
			// wrote on purpose, so it is unreadable rather than permanent.
			g.Expires, g.Bad = t, err != nil || t.IsZero()
		}
		if prev, ok := byName[name]; ok && !outlasts(g, prev) {
			continue
		}
		byName[name] = g
	}
	out := make([]FlagGrant, 0, len(byName))
	for _, g := range byName {
		out = append(out, g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// outlasts reports whether grant a lasts longer than b.
func outlasts(a, b FlagGrant) bool {
	switch {
	case a.Bad != b.Bad:
		return b.Bad
	case a.Expires.IsZero() != b.Expires.IsZero():
		return a.Expires.IsZero()
	}
	return a.Expires.After(b.Expires) || (a.Expires.Equal(b.Expires) && a.Stored < b.Stored)
}

// FlagNamesOf is the named flags on u's record, expired or not, sorted.
func FlagNamesOf(u *user.User) []string {
	var names []string
	for _, g := range FlagGrants(u) {
		names = append(names, g.Name)
	}
	return names
}

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
	if !roles.FlagDefined(name) {
		return false
	}
	for _, g := range FlagGrants(u) {
		if g.Name == name {
			return g.Live(now)
		}
	}
	return false
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

// The ways an expiry can be typed, in the board's timezone. A date alone is
// the start of that day.
const (
	expiryDate   = "2006-01-02"
	expiryMinute = "2006-01-02 15:04"
)

// ParseFlagExpiry reads an expiry as the user editors take it: a date
// (2027-01-01, midnight at its start), a date and time (2027-01-01 18:00),
// both in loc, or an RFC 3339 instant. nil loc is the server's zone.
func ParseFlagExpiry(s string, loc *time.Location) (time.Time, error) {
	if loc == nil {
		loc = time.Local
	}
	s = strings.TrimSpace(s)
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	for _, layout := range []string{expiryMinute, expiryDate} {
		if t, err := time.ParseInLocation(layout, s, loc); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("%q isn't an expiry: write YYYY-MM-DD, YYYY-MM-DD HH:MM or an RFC 3339 time", s)
}

// FormatFlagExpiry is t as the user editors show it, in loc: the date alone
// at midnight, else the date and minute, else RFC 3339 so nothing is lost.
// ParseFlagExpiry reads it back as t exactly.
func FormatFlagExpiry(t time.Time, loc *time.Location) string {
	if loc == nil {
		loc = time.Local
	}
	t = t.In(loc)
	text := t.Format(time.RFC3339Nano)
	switch {
	case t.Hour() == 0 && t.Minute() == 0 && t.Second() == 0 && t.Nanosecond() == 0:
		text = t.Format(expiryDate)
	case t.Second() == 0 && t.Nanosecond() == 0:
		text = t.Format(expiryMinute)
	}
	// A wall time a clock change passes twice, or skips, reads back as
	// another instant: keep the offset then, so the text is always the
	// instant it shows.
	if back, err := ParseFlagExpiry(text, loc); err != nil || !back.Equal(t) {
		text = t.Format(time.RFC3339Nano)
	}
	return text
}

// FormatFlagList is a record's named flags as the user editors take them
// back: "beta, donor=2027-01-01", "" for none. An expiry that can't be read
// is written as stored, so the sysop sees what is there.
func FormatFlagList(u *user.User, loc *time.Location) string {
	var parts []string
	for _, g := range FlagGrants(u) {
		switch {
		case g.Bad:
			parts = append(parts, g.Name+"="+g.Stored)
		case g.Expires.IsZero():
			parts = append(parts, g.Name)
		default:
			parts = append(parts, g.Name+"="+FormatFlagExpiry(g.Expires, loc))
		}
	}
	return strings.Join(parts, ", ")
}

// DescribeFlags is a record's named flags as the editors and the ACS tester
// show them at now: "beta, donor until 2027-01-01, old expired 2026-10-01",
// "" for none.
func DescribeFlags(u *user.User, loc *time.Location, now time.Time) string {
	var parts []string
	for _, g := range FlagGrants(u) {
		parts = append(parts, DescribeFlagGrant(g, loc, now))
	}
	return strings.Join(parts, ", ")
}

// DescribeFlagGrant is one grant as DescribeFlags shows it.
func DescribeFlagGrant(g FlagGrant, loc *time.Location, now time.Time) string {
	switch {
	case g.Bad:
		return fmt.Sprintf("%s (unreadable expiry %q)", g.Name, g.Stored)
	case g.Expires.IsZero():
		return g.Name
	case g.Live(now):
		return g.Name + " until " + FormatFlagExpiry(g.Expires, loc)
	}
	return g.Name + " expired " + FormatFlagExpiry(g.Expires, loc)
}

// ParseFlagList reads a list of named flags typed into a user editor, as
// FormatFlagList writes it: entries separated by commas, each a flag name,
// or a name, = and an expiry (ParseFlagExpiry, in loc). Names separated by
// spaces in one entry are several flags with no expiry. Each name must be
// declared in roles. An expiry at or before now is refused unless the
// record u already has that grant with that expiry, so an expired grant
// can stay on the record but a new one can't be born expired. A blank list
// is no flags.
//
// The result is the record's new NamedFlags: an unchanged grant keeps its
// stored text, a new expiry is stored as RFC 3339 in UTC.
func ParseFlagList(s string, roles *Roles, loc *time.Location, now time.Time, u *user.User) (map[string]string, error) {
	stored := map[string]FlagGrant{}
	for _, g := range FlagGrants(u) {
		stored[g.Name] = g
	}
	out := map[string]string{}
	add := func(name, expiry string) error {
		name = NormalizeRoleName(name)
		if err := CheckFlagName(name); err != nil {
			return err
		}
		if !roles.FlagDefined(name) {
			all := roles.FlagNames()
			if len(all) == 0 {
				return fmt.Errorf("no flag named %s: configs/roles.json declares no flags", name)
			}
			return fmt.Errorf("no flag named %s in configs/roles.json (declared: %s)", name, strings.Join(all, ", "))
		}
		if _, dup := out[name]; dup {
			return fmt.Errorf("flag %s is listed twice", name)
		}
		old, had := stored[name]
		if expiry == "" {
			out[name] = ""
			return nil
		}
		if had && old.Bad && expiry == old.Stored {
			return fmt.Errorf("flag %s has an expiry that can't be read (%q); write it again or leave it off", name, old.Stored)
		}
		t, err := ParseFlagExpiry(expiry, loc)
		if err != nil {
			return fmt.Errorf("flag %s: %w", name, err)
		}
		if had && !old.Bad && !old.Expires.IsZero() && t.Equal(old.Expires) {
			out[name] = old.Stored
			return nil
		}
		if !now.Before(t) {
			return fmt.Errorf("flag %s: %s has already passed", name, FormatFlagExpiry(t, loc))
		}
		out[name] = t.UTC().Format(time.RFC3339Nano)
		return nil
	}
	for _, entry := range strings.Split(s, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if name, expiry, ok := strings.Cut(entry, "="); ok {
			name, expiry = strings.TrimSpace(name), strings.TrimSpace(expiry)
			if strings.ContainsAny(name, " \t") {
				return nil, fmt.Errorf("%q: put a comma between flags", entry)
			}
			if expiry == "" {
				return nil, fmt.Errorf("%q: an expiry goes after the =; leave the = off for none", entry)
			}
			if err := add(name, expiry); err != nil {
				return nil, err
			}
			continue
		}
		for _, name := range strings.Fields(entry) {
			if err := add(name, ""); err != nil {
				return nil, err
			}
		}
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// SameFlags reports whether two NamedFlags maps grant the same flags with
// the same stored expiries, names compared normalized.
func SameFlags(a, b map[string]string) bool {
	ga, gb := FlagGrants(&user.User{NamedFlags: a}), FlagGrants(&user.User{NamedFlags: b})
	if len(ga) != len(gb) {
		return false
	}
	for i := range ga {
		if ga[i].Name != gb[i].Name || ga[i].Stored != gb[i].Stored {
			return false
		}
	}
	return true
}
