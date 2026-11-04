package menu

import (
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
		{"not logged in", config.DoorConfig{}, nil, false},
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
	}
	cfg := config.ServerConfig{DoorCategories: []config.DoorCategory{
		{Code: "GAMES", Name: "Games"},
		{Code: "VIP", Name: "VIP Lounge", ACS: "FV"},
	}}
	caller := &user.User{AccessLevel: 50, Flags: "G"}
	allows := func(acs string) bool { return checkACS(acs, caller, nil, nil, time.Now()) }

	got := doorMenuCodes(buildDoorMenuEntries(doors, cfg, "GAMES", caller.AccessLevel, allows, "Other"))
	want := []string{"FLAG", "OPEN"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("GAMES lists %v, want %v (door acs, level and hidden applied)", got, want)
	}
	// a category the caller fails is left out of the menu, door and all
	if got := doorMenuCodes(buildDoorMenuEntries(doors, cfg, "", caller.AccessLevel, allows, "Other")); len(got) != 1 || got[0] != "GAMES" {
		t.Errorf("categories = %v, want only GAMES", got)
	}
}
