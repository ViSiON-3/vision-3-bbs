package acs

import (
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/perm"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

func flagTestRoles(t *testing.T) *perm.Roles {
	t.Helper()
	r, diags, err := perm.Parse([]byte(`{"roles": [], "flags": [{"name": "donor"}, {"name": "beta_tester"}]}`))
	if err != nil || len(diags) > 0 {
		t.Fatalf("roles: %v %v", err, diags)
	}
	return r
}

func TestNamedFlagTerm(t *testing.T) {
	roles := flagTestRoles(t)
	expiry := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	u := &user.User{AccessLevel: 20, Flags: "AB", NamedFlags: map[string]string{
		"donor": expiry.Format(time.RFC3339), "BETA_TESTER": "",
	}}
	sysop := &user.User{AccessLevel: oracleSysOp}
	before := expiry.Add(-time.Minute)
	for _, tt := range []struct {
		acs  string
		u    *user.User
		now  time.Time
		want bool
	}{
		{"FLAG:donor", u, before, true},
		{"flag:DONOR&FLAG:beta_tester", u, before, true},
		// Stops at the expiry instant, for the same caller later on.
		{"FLAG:donor", u, expiry, false},
		{"!FLAG:donor", u, expiry, true},
		{"FLAG:beta_tester", u, expiry.Add(1000 * time.Hour), true},
		// The legacy flags are untouched: one character is the F term.
		{"FLAG:A&FB", u, before, true},
		{"FLAG:D", u, before, false},
		{"FLAG:donor", sysop, before, false},
		// An undeclared name fails the whole string, '!' and '|' included.
		{"FLAG:donr", u, before, false},
		{"!FLAG:donr", u, before, false},
		{"SYSOP|FLAG:donr", sysop, before, false},
	} {
		ctx := ctxFor(tt.u)
		ctx.Roles, ctx.Now = roles, tt.now
		if got := Check(tt.acs, ctx); got != tt.want {
			t.Errorf("Check(%q) at %v = %v, want %v", tt.acs, tt.now, got, tt.want)
		}
		if x := Explain(tt.acs, ctx); x.Result != tt.want {
			t.Errorf("Explain(%q) = %v", tt.acs, x.Result)
		} else if x.Root != nil {
			checkSteps(t, tt.acs, x.Root, ctx)
		}
	}

	ctx := ctxFor(u)
	ctx.Roles, ctx.Location = roles, time.UTC
	for now, want := range map[time.Time]string{
		before: "has flag donor until 2027-01-01",
		expiry: "had flag donor, which expired 2027-01-01",
	} {
		ctx.Now = now
		if x := Explain("FLAG:donor", ctx); x.Root == nil || x.Root.Detail != want {
			t.Errorf("detail at %v = %+v, want %q", now, x.Root, want)
		}
	}
	ctx.Now = expiry
	lines := strings.Join(Render(Explain("FLAG:beta_tester", ctx), 0), "\n")
	if !strings.Contains(lines, "Flags:      beta_tester, donor expired 2027-01-01") || !strings.Contains(lines, "has flag beta_tester, with no expiry") {
		t.Errorf("render:\n%s", lines)
	}
	x := Explain("FLAG:donr", ctx)
	if x.Reason != Undefined || x.Root == nil || !strings.Contains(x.Root.Detail, "no flag named donr (declared: donor, beta_tester)") {
		t.Errorf("undeclared = %+v", x.Root)
	}
	ctx.User = &user.User{}
	if x := Explain("FLAG:donor", ctx); x.Root == nil || x.Root.Detail != "doesn't have flag donor (named flags: none)" {
		t.Errorf("no grant = %+v", x.Root)
	}
	if got := strings.Join(Parse("FLAG:donor|FLAG:A|GROUP:x|FLAG:DONOR|FLAG:beta_tester").FlagNames(), ","); got != "donor,beta_tester" {
		t.Errorf("FlagNames = %q", got)
	}
}

// Every ACS term name is reserved, so a named flag never reads as a term.
func TestTermNamesAreReservedFlagNames(t *testing.T) {
	names := []string{"SYSOP", "COSYSOP"}
	for name := range termNames {
		names = append(names, name)
	}
	for _, name := range names {
		if !perm.ReservedFlagName(name) {
			t.Errorf("term name %s isn't a reserved flag name", name)
		}
		if err := perm.CheckFlagName(strings.ToLower(name)); err == nil {
			t.Errorf("flag name %s is allowed", name)
		}
	}
}
