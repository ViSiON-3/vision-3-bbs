package acs

import (
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// TestUserDataTerms pins the terms that read the user's record: the counts
// with comparisons and ranges, REALNAME, and AGE in the board's timezone.
func TestUserDataTerms(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skip("no tz database:", err)
	}
	now := time.Date(2026, 11, 5, 1, 30, 0, 0, time.UTC) // 20:30 on 11/04 in New York
	u := &user.User{ID: 5, AccessLevel: 20, TimesCalled: 12, MessagesPosted: 40, NumUploads: 3, NumDownloads: 0,
		RealName: "Robbie", CreatedAt: time.Date(2026, 10, 25, 3, 0, 0, 0, time.UTC)} // 10/24 23:00 in New York
	ctx := Context{User: u, Start: now, Now: now, Location: ny, SysOpLevel: 255, CoSysOpLevel: 250}
	for acs, want := range map[string]bool{
		"CALLS>=10":                         true,
		"CALLS<10":                          false,
		"calls=12":                          true,
		"POSTS=40-99":                       true,
		"POSTS>40":                          false,
		"UPLOADS>=3":                        true,
		"DOWNLOADS=0":                       true,
		"DOWNLOADS>0":                       false,
		"REALNAME":                          true,
		"!REALNAME":                         false,
		"AGE=11":                            true, // 10/24 to 11/04 in New York; 10/25 to 11/05 in UTC is 11 too
		"AGE>=30":                           false,
		"AGE<30&CALLS>10":                   true,
		"LEVEL>=20&AGE=0-11&REALNAME|SYSOP": true,
	} {
		if got := Check(acs, ctx); got != want {
			t.Errorf("Check(%q) = %v, want %v", acs, got, want)
		}
	}

	// A user with no sign-up date has no age: AGE fails whatever the test.
	noDate := *u
	noDate.CreatedAt, noDate.RealName = time.Time{}, "  "
	c := ctx
	c.User = &noDate
	for _, acs := range []string{"AGE>=0", "AGE<99999", "REALNAME"} {
		if Check(acs, c) {
			t.Errorf("Check(%q) passed for a user with no sign-up date and a blank real name", acs)
		}
	}
	x := Tester{At: now, Location: ny, SysOpLevel: 255, CoSysOpLevel: 250}.Explain("AGE>=0", &noDate)
	if x.Reason != Unavailable || !strings.Contains(x.Root.Detail, "no sign-up date") {
		t.Errorf("Explain(AGE) for no date = %s, %q", x.Reason, x.Root.Detail)
	}
	if r := strings.Join(Render(x, 79), "\n"); !strings.Contains(r, "resting on a value the user's record doesn't have") {
		t.Errorf("Render:\n%s", r)
	}
}

// TestAccountAge pins whole calendar days in the board's zone: a sign-up
// late in the evening is a day old after midnight there, not 24 hours on.
func TestAccountAge(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skip("no tz database:", err)
	}
	created := time.Date(2026, 11, 4, 3, 30, 0, 0, time.UTC) // 11/03 22:30 in New York
	for _, tc := range []struct {
		now  time.Time
		loc  *time.Location
		want int
	}{
		{time.Date(2026, 11, 4, 4, 0, 0, 0, time.UTC), ny, 0},       // 23:00 the same evening
		{time.Date(2026, 11, 4, 5, 0, 0, 0, time.UTC), ny, 1},       // 00:00 the next day
		{time.Date(2026, 11, 4, 5, 0, 0, 0, time.UTC), time.UTC, 0}, // still 11/04 in UTC
		{time.Date(2026, 12, 4, 5, 0, 0, 0, time.UTC), ny, 31},      // across the DST change
		{time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), ny, 0},       // before sign-up
	} {
		if got := AccountAge(created, tc.now, tc.loc); got != tc.want {
			t.Errorf("AccountAge(%v in %v) = %d, want %d", tc.now, tc.loc, got, tc.want)
		}
	}
}

// TestUserDataNamesParse: the names take what they always take, and a name
// written without it reads as its first letter, as before.
func TestUserDataNamesParse(t *testing.T) {
	for term, wantProblem := range map[string]bool{
		"CALLS>=10":   false,
		"AGE=1-30":    false,
		"REALNAME":    false,
		"CALLS":       true, // C with the argument ALLS
		"AGE=30-1":    true, // a backwards range on a count
		"POSTS:5":     true,
		"REALNAME=1":  true,
		"DOWNLOADS<x": true,
	} {
		if p := termProblem(term); (p != "") != wantProblem {
			t.Errorf("termProblem(%q) = %q, want a problem: %v", term, p, wantProblem)
		}
	}
}
