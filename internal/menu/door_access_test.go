package menu

import (
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// TestDoorAllowed checks the rule every door entry point shares: the door's
// min_access_level and its acs must both pass.
func TestDoorAllowed(t *testing.T) {
	caller := &user.User{ID: 2, AccessLevel: 50, Flags: "G", Validated: true}
	sysop := &user.User{ID: 1, AccessLevel: 255}
	for _, tc := range []struct {
		name string
		door config.DoorConfig
		u    *user.User
		want bool
	}{
		{"no rules", config.DoorConfig{}, caller, true},
		{"level only, met", config.DoorConfig{MinAccessLevel: 50}, caller, true},
		{"level only, not met", config.DoorConfig{MinAccessLevel: 51}, caller, false},
		{"acs only, met", config.DoorConfig{ACS: "FG"}, caller, true},
		{"acs only, not met", config.DoorConfig{ACS: "FX"}, caller, false},
		{"both met", config.DoorConfig{MinAccessLevel: 20, ACS: "FG&V"}, caller, true},
		{"level met, acs not", config.DoorConfig{MinAccessLevel: 20, ACS: "FX"}, caller, false},
		{"acs met, level not", config.DoorConfig{MinAccessLevel: 99, ACS: "FG"}, caller, false},
		// an empty or wildcard acs never lifts the numeric minimum
		{"empty acs keeps the level", config.DoorConfig{MinAccessLevel: 99}, caller, false},
		{"wildcard acs keeps the level", config.DoorConfig{MinAccessLevel: 99, ACS: "*"}, caller, false},
		// no sysop bypass: the door's rules apply to everyone
		{"sysop without the flag", config.DoorConfig{ACS: "FG"}, sysop, false},
		{"sysop passing", config.DoorConfig{ACS: "SYSOP"}, sysop, true},
		// a malformed acs denies, as everywhere else
		{"malformed acs", config.DoorConfig{ACS: "S20&"}, caller, false},
		{"malformed acs, sysop", config.DoorConfig{ACS: "S20&"}, sysop, false},
		// strict: a term Validate reports denies the door, even negated,
		// where elsewhere !Q (Q is no ACS letter) passes everyone
		{"negated unknown term", config.DoorConfig{ACS: "!Q"}, caller, false},
		{"negated unknown term, sysop", config.DoorConfig{ACS: "!Q"}, sysop, false},
		{"negated unknown term in an or", config.DoorConfig{ACS: "FG|!Q"}, caller, false},
		{"bad argument", config.DoorConfig{ACS: "H25"}, caller, false},
		{"dropped character", config.DoorConfig{ACS: "S20@"}, caller, false},
		{"not logged in", config.DoorConfig{}, nil, false},
		// a blank acs is no rule, however much whitespace it holds
		{"whitespace acs is no rule", config.DoorConfig{ACS: " \t "}, caller, true},
		{"whitespace acs, sysop", config.DoorConfig{ACS: "  "}, sysop, true},
		{"whitespace acs keeps the level", config.DoorConfig{MinAccessLevel: 99, ACS: " "}, caller, false},
		{"padded acs", config.DoorConfig{ACS: " FG "}, caller, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := doorAllowed(tc.door, tc.u, nil, nil, time.Now()); got != tc.want {
				t.Errorf("doorAllowed = %v, want %v", got, tc.want)
			}
		})
	}
}

// The generated door menu lists a door only when its own rules pass; a
// category's rules filter the menu without changing what a door requires.
func TestDoorMenuEntriesDoorACS(t *testing.T) {
	doors := map[string]config.DoorConfig{
		"OPEN":   {Code: "OPEN", Name: "Open", Category: "GAMES"},
		"FLAG":   {Code: "FLAG", Name: "Flagged", Category: "GAMES", ACS: "FG"},
		"LOCKED": {Code: "LOCKED", Name: "Locked", Category: "GAMES", ACS: "FX"},
		"BOTH":   {Code: "BOTH", Name: "Both", Category: "GAMES", MinAccessLevel: 99, ACS: "FG"},
		"HIDDEN": {Code: "HIDDEN", Name: "Hidden", Category: "GAMES", ACS: "FG", Hidden: true},
		"VIP":    {Code: "VIP", Name: "VIP", Category: "VIP"},
		"BLANK":  {Code: "BLANK", Name: "Blank", Category: "GAMES", ACS: "  "},
		"NOTQ":   {Code: "NOTQ", Name: "Not Q", Category: "GAMES", ACS: "!Q"},
		"BROKEN": {Code: "BROKEN", Name: "Broken", Category: "GAMES", ACS: "S10&"},
	}
	cfg := config.ServerConfig{DoorCategories: []config.DoorCategory{
		{Code: "GAMES", Name: "Games"},
		{Code: "VIP", Name: "VIP Lounge", ACS: "FV"},
	}}
	caller := &user.User{AccessLevel: 50, Flags: "G"}
	allows := func(acs string) bool { return checkACS(acs, caller, nil, nil, time.Now()) }

	got := doorMenuCodes(buildDoorMenuEntries(doors, cfg, "GAMES", caller.AccessLevel, allows, "Other"))
	want := []string{"BLANK", "FLAG", "OPEN"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("GAMES lists %v, want %v (door acs, strict rule, level and hidden applied)", got, want)
	}
	// a category the caller fails is left out of the menu, door and all
	if got := doorMenuCodes(buildDoorMenuEntries(doors, cfg, "", caller.AccessLevel, allows, "Other")); len(got) != 1 || got[0] != "GAMES" {
		t.Errorf("categories = %v, want only GAMES", got)
	}
}

// The generated menu evaluates each door's ACS once while the caller pages
// through it, not on every redraw.
func TestDoorMenuEvaluatesACSOncePerVisit(t *testing.T) {
	c, _, _ := doorMenuHarness(t, "list", "][][q", 100)
	doors := c.e.DoorRegistry()
	d := doors["D50"]
	d.ACS = "S5"
	doors["D50"] = d
	c.e.SetDoorRegistry(doors)
	var logged strings.Builder
	restore := captureSlog(&logged, &slog.HandlerOptions{Level: slog.LevelDebug})
	_, _, err := runDoorMenu(c, "")
	restore()
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, line := range strings.Split(logged.String(), "\n") {
		if strings.Contains(line, "checking ACS") && strings.Contains(line, "acs=S5 ") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("acs evaluated %d times over five redraws, want 1:\n%s", n, logged.String())
	}
}

// The generated menu leaves out a door whose acs the strict rule rejects, a
// negated unknown term (!Q) or a malformed string, even for a sysop: the
// first entry is the door after it.
func TestDoorMenuStrictDoorACS(t *testing.T) {
	for _, rule := range []string{"!Q", "S10&"} {
		t.Run(rule, func(t *testing.T) {
			c, s, calls := doorMenuHarness(t, "list", "1\rq", 0)
			c.currentUser.AccessLevel = 255
			c.e.SetDoorRegistry(map[string]config.DoorConfig{
				"BAD":  {Code: "BAD", Name: "Door BAD", ACS: rule},
				"GOOD": {Code: "GOOD", Name: "Door GOOD", ACS: "S10"},
			})
			if _, _, err := runDoorMenu(c, ""); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(s.output(), "Door BAD") {
				t.Errorf("door with acs %q listed", rule)
			}
			if want := []string{"GOOD"}; !reflect.DeepEqual(*calls, want) {
				t.Errorf("launched %v, want %v", *calls, want)
			}
		})
	}
}

// The remembered answers are dropped after a door, which may have changed the
// caller's record: a flag a door grants lists the door that needs it.
func TestDoorMenuReevaluatesACSAfterDoor(t *testing.T) {
	c, _, calls := doorMenuHarness(t, "list", "1\r1\rq", 0)
	c.e.SetDoorRegistry(map[string]config.DoorConfig{
		"FLAG": {Code: "FLAG", Name: "Flagged", ACS: "FG"},
		"GIVE": {Code: "GIVE", Name: "Gives G"},
	})
	c.e.RunRegistry["DOOR:"] = func(c *cmdCtx, code string) (*user.User, string, error) {
		*calls = append(*calls, code)
		u := *c.currentUser
		u.Flags = "G"
		return &u, "", nil
	}
	if _, _, err := runDoorMenu(c, ""); err != nil {
		t.Fatal(err)
	}
	if want := []string{"GIVE", "FLAG"}; !reflect.DeepEqual(*calls, want) {
		t.Errorf("launched %v, want %v", *calls, want)
	}
}
