package perm

import (
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

func flagRoles(t *testing.T) *Roles {
	t.Helper()
	r, diags, err := Parse([]byte(`{"roles": [], "flags": [
		{"name": "Donor", "description": " gave money "},
		{"name": "beta"},
		{"name": "old-timer"}
	]}`))
	if err != nil || len(diags) > 0 {
		t.Fatalf("flags: %v %v", err, diags)
	}
	return r
}

func TestFlagDeclarations(t *testing.T) {
	r, diags, err := Parse([]byte(`{"roles": [], "flags": [
		{"name": "donor", "description": " gave "},
		{"name": "DONOR"},
		{"name": "x"},
		{"name": "Level"},
		{"name": "group"},
		{"name": "1st"},
		{"name": "beta-"}
	]}`))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		`flag 2 ("DONOR"): another flag is already named donor`,
		`flag 3 ("x"): flag name "x" is one character, which is the legacy flag X`,
		`flag 4 ("Level"): flag name "level" is reserved`,
		`flag 5 ("group"): flag name "group" is reserved`,
		`flag 6 ("1st"): flag name "1st" must start with a letter`,
		`flag 7 ("beta-"): flag name "beta-" ends in '-'`,
	}
	if len(diags) != len(want) {
		t.Fatalf("diagnostics = %v", diags)
	}
	for i, w := range want {
		if !strings.HasPrefix(diags[i].String(), w) {
			t.Errorf("diagnostic %d = %q, want prefix %q", i, diags[i], w)
		}
	}
	if got := strings.Join(r.FlagNames(), ","); got != "donor" || r.Flags()[0].Description != "gave" {
		t.Errorf("flags = %q %+v", got, r.Flags())
	}
	// A flag, a group and a role may share a name: they are separate.
	r2, diags, _ := Parse([]byte(`{"roles": [{"name": "ops", "permissions": ["users.validate"]}], "groups": [{"name": "ops"}], "flags": [{"name": "ops"}]}`))
	if len(diags) > 0 || !r2.FlagDefined("OPS") || !r2.GroupDefined("ops") || !r2.Defined("ops") {
		t.Errorf("shared name: %v", diags)
	}
}

func TestHasFlag(t *testing.T) {
	r := flagRoles(t)
	expiry := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	u := &user.User{AccessLevel: 20, NamedFlags: map[string]string{
		"DONOR":     expiry.Format(time.RFC3339),
		"beta":      "",
		"old-timer": "next tuesday",
		"gone":      "",
	}}
	before, at := expiry.Add(-time.Second), expiry
	for _, tt := range []struct {
		name string
		now  time.Time
		want bool
	}{
		{"donor", before, true},
		{"Donor", at, false}, // stops at the instant
		{"beta", at, true},
		{"old-timer", before, false}, // an unreadable expiry never passes
		{"gone", before, false},      // not declared
		{"nope", before, false},
	} {
		if got := HasFlag(u, tt.name, r, tt.now); got != tt.want {
			t.Errorf("HasFlag(%s, %v) = %v, want %v", tt.name, tt.now, got, tt.want)
		}
	}
	if HasFlag(&user.User{AccessLevel: 255}, "beta", r, before) {
		t.Error("a sysop has a flag they weren't given")
	}
	if HasFlag(&user.User{NamedFlags: map[string]string{"beta": ""}, DeletedUser: true}, "beta", r, before) {
		t.Error("a deleted user has a flag")
	}
	// Granting a flag grants no permission.
	if Check(&user.User{AccessLevel: 20, NamedFlags: map[string]string{"beta": ""}}, UsersValidate, Scope{}, levels, r).Allowed {
		t.Error("a flag granted a permission")
	}
	if got := UndefinedFlags(FlagNamesOf(u), r); strings.Join(got, ",") != "gone" {
		t.Errorf("UndefinedFlags = %v", got)
	}
}

func TestFlagListRoundTrip(t *testing.T) {
	r := flagRoles(t)
	loc := time.FixedZone("PST", -8*3600)
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, loc)
	u := &user.User{}

	got, err := ParseFlagList("donor=2027-01-01, beta old-timer=2026-12-01 18:30", r, loc, now, u)
	if err == nil {
		t.Fatalf("a space-separated name before = was read: %v", got)
	}
	got, err = ParseFlagList("Donor=2027-01-01, beta, old-timer=2026-12-01 18:30", r, loc, now, u)
	if err != nil {
		t.Fatal(err)
	}
	if got["donor"] != "2027-01-01T08:00:00Z" || got["beta"] != "" || got["old-timer"] != "2026-12-02T02:30:00Z" || len(got) != 3 {
		t.Fatalf("ParseFlagList = %v", got)
	}
	u.NamedFlags = got
	if s := FormatFlagList(u, loc); s != "beta, donor=2027-01-01, old-timer=2026-12-01 18:30" {
		t.Errorf("FormatFlagList = %q", s)
	}
	// After the expiry, the grant stays and shows as expired; sending the
	// list back unchanged keeps it as stored.
	later := time.Date(2026, 12, 5, 0, 0, 0, 0, loc)
	if d := DescribeFlags(u, loc, later); d != "beta, donor until 2027-01-01, old-timer expired 2026-12-01 18:30" {
		t.Errorf("DescribeFlags = %q", d)
	}
	again, err := ParseFlagList(FormatFlagList(u, loc), r, loc, later, u)
	if err != nil || !SameFlags(again, u.NamedFlags) {
		t.Errorf("round trip = %v, %v", again, err)
	}
	// A new grant can't be born expired.
	if _, err := ParseFlagList("old-timer=2026-12-04", r, loc, later, u); err == nil || !strings.Contains(err.Error(), "already passed") {
		t.Errorf("past expiry: %v", err)
	}
	// Revoking is leaving the flag out.
	if got, err := ParseFlagList("beta", r, loc, later, u); err != nil || len(got) != 1 {
		t.Errorf("revoke = %v, %v", got, err)
	}
	if got, err := ParseFlagList("  ", r, loc, later, u); err != nil || got != nil {
		t.Errorf("blank = %v, %v", got, err)
	}
	for in, want := range map[string]string{
		"nope":             "no flag named nope in configs/roles.json (declared: donor, beta, old-timer)",
		"x":                "legacy flag X",
		"beta, beta":       "listed twice",
		"beta=":            "leave the = off",
		"beta=soon":        "isn't an expiry",
		"beta=2027-13-01":  "isn't an expiry",
		"beta=2027-01-01T": "isn't an expiry",
	} {
		if _, err := ParseFlagList(in, r, loc, now, &user.User{}); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("ParseFlagList(%q) = %v, want %q", in, err, want)
		}
	}
	if _, err := ParseFlagList("beta", &Roles{}, loc, now, &user.User{}); err == nil || !strings.Contains(err.Error(), "declares no flags") {
		t.Errorf("no flags: %v", err)
	}
	// RFC 3339 with seconds keeps them.
	got, _ = ParseFlagList("beta=2027-01-01T10:00:30Z", r, loc, now, &user.User{})
	if got["beta"] != "2027-01-01T10:00:30Z" || FormatFlagList(&user.User{NamedFlags: got}, loc) != "beta=2027-01-01T02:00:30-08:00" {
		t.Errorf("seconds = %v", got)
	}
	// An unreadable stored expiry has to be written again.
	bad := &user.User{NamedFlags: map[string]string{"beta": "soon"}}
	if _, err := ParseFlagList(FormatFlagList(bad, loc), r, loc, now, bad); err == nil || !strings.Contains(err.Error(), "can't be read") {
		t.Errorf("bad stored expiry: %v", err)
	}
}
