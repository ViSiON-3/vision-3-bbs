package perm

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"
)

// Named flags (#1074) and role grants (#1125) can both expire. This file is
// what they share: a grant with an optional expiry, how the user editors
// type and show a list of them, and how one is checked at a moment.

// TimedGrant is one named flag or role on a user's record, with its expiry.
type TimedGrant struct {
	// Name is the flag's or role's name, normalized.
	Name string
	// Expires is the instant the grant stops counting; zero is never.
	Expires time.Time
	// Stored is the expiry as the record has it, "" for none.
	Stored string
	// Bad is set when Stored isn't a usable RFC 3339 instant: the grant
	// never counts, and doctor reports it.
	Bad bool
}

// Live reports whether the grant counts at now: now is before its expiry,
// or it has none. A grant whose expiry can't be read never counts.
func (g TimedGrant) Live(now time.Time) bool {
	return !g.Bad && (g.Expires.IsZero() || now.Before(g.Expires))
}

// timedGrants is the grants named in permanent (no expiry) and in timed
// (name to stored expiry), one per name: a name given more than once,
// under different cases or in both, keeps the grant that lasts longest.
// The permanent ones come first, in their order, then the timed ones by
// name. needExpiry is set when every entry in timed must have an expiry, as
// expiringRoles must: a blank one is then unreadable, not "never".
func timedGrants(permanent []string, timed map[string]string, needExpiry bool) []TimedGrant {
	var out []TimedGrant
	at := map[string]int{}
	add := func(g TimedGrant) {
		if i, ok := at[g.Name]; ok {
			if outlasts(g, out[i]) {
				out[i] = g
			}
			return
		}
		at[g.Name] = len(out)
		out = append(out, g)
	}
	for _, name := range NormalizeRoles(permanent) {
		add(TimedGrant{Name: name})
	}
	names := make([]string, 0, len(timed))
	for raw := range timed {
		names = append(names, raw)
	}
	sort.Strings(names)
	for _, raw := range names {
		name := NormalizeRoleName(raw)
		if name == "" {
			continue
		}
		add(readGrant(name, timed[raw], needExpiry))
	}
	return out
}

// unreadableGrants is every entry in timed whose expiry can't be read, by
// name, including one that timedGrants drops for a readable entry of the
// same name under another case. Doctor reports each.
func unreadableGrants(timed map[string]string, needExpiry bool) []TimedGrant {
	var out []TimedGrant
	for raw, stored := range timed {
		if name := NormalizeRoleName(raw); name != "" {
			if g := readGrant(name, stored, needExpiry); g.Bad {
				out = append(out, g)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Stored < out[j].Stored
	})
	return out
}

// readGrant is the grant of name with the stored expiry stored. A blank
// one is no expiry, unless needExpiry is set, and then it is unreadable.
func readGrant(name, stored string, needExpiry bool) TimedGrant {
	g := TimedGrant{Name: name, Stored: strings.TrimSpace(stored)}
	if g.Stored == "" && needExpiry {
		g.Bad = true
	}
	if g.Stored != "" {
		t, err := time.Parse(time.RFC3339, g.Stored)
		// The zero instant would read as "never": it is no expiry anyone
		// wrote on purpose, so it is unreadable rather than permanent.
		g.Expires, g.Bad = t, err != nil || t.IsZero()
	}
	return g
}

// outlasts reports whether grant a lasts longer than b.
func outlasts(a, b TimedGrant) bool {
	switch {
	case a.Bad != b.Bad:
		return b.Bad
	case a.Expires.IsZero() != b.Expires.IsZero():
		return a.Expires.IsZero()
	}
	return a.Expires.After(b.Expires) || (a.Expires.Equal(b.Expires) && a.Stored < b.Stored)
}

// grantNames is the grants' names, in order.
func grantNames(grants []TimedGrant) []string {
	var names []string
	for _, g := range grants {
		names = append(names, g.Name)
	}
	return names
}

// liveGrant reports whether grants has one named name that counts at now.
func liveGrant(grants []TimedGrant, name string, now time.Time) bool {
	for _, g := range grants {
		if g.Name == name {
			return g.Live(now)
		}
	}
	return false
}

// The ways an expiry can be typed, in the board's timezone. A date alone is
// the start of that day.
const (
	expiryDate   = "2006-01-02"
	expiryMinute = "2006-01-02 15:04"
)

// ParseExpiry reads an expiry as the user editors take it: a date
// (2027-01-01, midnight at its start), a date and time (2027-01-01 18:00),
// both in loc, or an RFC 3339 instant. nil loc is the server's zone.
func ParseExpiry(s string, loc *time.Location) (time.Time, error) {
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

// FormatExpiry is t as the user editors show it, in loc: the date alone at
// midnight, else the date and minute, else RFC 3339 so nothing is lost.
// ParseExpiry reads it back as t exactly.
func FormatExpiry(t time.Time, loc *time.Location) string {
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
	if back, err := ParseExpiry(text, loc); err != nil || !back.Equal(t) {
		text = t.Format(time.RFC3339Nano)
	}
	return text
}

// formatGrants is grants as the user editors take them back:
// "beta, donor=2027-01-01", "" for none. An expiry that can't be read is
// written as stored, so the sysop sees what is there.
func formatGrants(grants []TimedGrant, loc *time.Location) string {
	var parts []string
	for _, g := range grants {
		switch {
		case g.Bad:
			parts = append(parts, g.Name+"="+g.Stored)
		case g.Expires.IsZero():
			parts = append(parts, g.Name)
		default:
			parts = append(parts, g.Name+"="+FormatExpiry(g.Expires, loc))
		}
	}
	return strings.Join(parts, ", ")
}

// describeGrants is grants as the editors and the ACS tester show them at
// now: "beta, donor until 2027-01-01, old expired 2026-10-01", "" for none.
func describeGrants(grants []TimedGrant, loc *time.Location, now time.Time) string {
	var parts []string
	for _, g := range grants {
		parts = append(parts, DescribeGrant(g, loc, now))
	}
	return strings.Join(parts, ", ")
}

// DescribeGrant is one grant as the editors show it at now: "donor",
// "donor until 2027-01-01", "donor expired 2027-01-01".
func DescribeGrant(g TimedGrant, loc *time.Location, now time.Time) string {
	switch {
	case g.Bad:
		return fmt.Sprintf("%s (unreadable expiry %q)", g.Name, g.Stored)
	case g.Expires.IsZero():
		return g.Name
	case g.Live(now):
		return g.Name + " until " + FormatExpiry(g.Expires, loc)
	}
	return g.Name + " expired " + FormatExpiry(g.Expires, loc)
}

// grantKind is what a list of timed grants names: flags or roles.
type grantKind struct {
	kind  string // "flag", "role"
	check func(string) error
	// defined reports whether the board has the name; all is every name it
	// has, and verb how roles.json has them ("declares", "defines").
	defined func(string) bool
	all     []string
	verb    string
}

// parseGrants reads a list of grants typed into a user editor, as
// formatGrants writes it: entries separated by commas, each a name, or a
// name, = and an expiry (ParseExpiry, in loc). Names separated by spaces in
// one entry are several grants with no expiry. Each name must be one k
// has. An expiry at or before now is refused unless stored already has
// that grant with that expiry, so an expired grant can stay on the record
// but a new one can't be born expired.
//
// The result is in the order typed: an unchanged grant keeps its stored
// text, a new expiry is stored as RFC 3339 in UTC.
func parseGrants(s string, k grantKind, loc *time.Location, now time.Time, stored []TimedGrant) ([]TimedGrant, error) {
	byName := map[string]TimedGrant{}
	for _, g := range stored {
		byName[g.Name] = g
	}
	var out []TimedGrant
	seen := map[string]bool{}
	add := func(name, expiry string) error {
		name = NormalizeRoleName(name)
		if err := k.check(name); err != nil {
			return err
		}
		if !k.defined(name) {
			if len(k.all) == 0 {
				return fmt.Errorf("no %s named %s: configs/roles.json %s no %ss", k.kind, name, k.verb, k.kind)
			}
			return fmt.Errorf("no %s named %s in configs/roles.json (%s: %s)", k.kind, name, strings.TrimSuffix(k.verb, "s")+"d", strings.Join(k.all, ", "))
		}
		if seen[name] {
			return fmt.Errorf("%s %s is listed twice", k.kind, name)
		}
		seen[name] = true
		if expiry == "" {
			out = append(out, TimedGrant{Name: name})
			return nil
		}
		old, had := byName[name]
		if had && old.Bad && expiry == old.Stored {
			return fmt.Errorf("%s %s has an expiry that can't be read (%q); write it again or leave it off", k.kind, name, old.Stored)
		}
		t, err := ParseExpiry(expiry, loc)
		if err != nil {
			return fmt.Errorf("%s %s: %w", k.kind, name, err)
		}
		if had && !old.Bad && !old.Expires.IsZero() && t.Equal(old.Expires) {
			out = append(out, old)
			return nil
		}
		if !now.Before(t) {
			return fmt.Errorf("%s %s: %s has already passed", k.kind, name, FormatExpiry(t, loc))
		}
		stored := t.UTC().Format(time.RFC3339Nano)
		out = append(out, TimedGrant{Name: name, Expires: t, Stored: stored})
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
				return nil, fmt.Errorf("%q: put a comma between %ss", entry, k.kind)
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
	return out, nil
}

// splitGrants is grants as a record keeps them: the names with no expiry,
// in order, and the rest as name to stored expiry. Either is nil when
// empty.
func splitGrants(grants []TimedGrant) (permanent []string, timed map[string]string) {
	for _, g := range grants {
		if g.Stored == "" {
			permanent = append(permanent, g.Name)
			continue
		}
		if timed == nil {
			timed = map[string]string{}
		}
		timed[g.Name] = g.Stored
	}
	return permanent, timed
}

// sameGrants reports whether a and b are the same grants with the same
// stored expiries, in any order.
func sameGrants(a, b []TimedGrant) bool {
	key := func(gs []TimedGrant) []string {
		out := make([]string, len(gs))
		for i, g := range gs {
			out[i] = g.Name + "=" + g.Stored
		}
		sort.Strings(out)
		return out
	}
	return slices.Equal(key(a), key(b))
}
