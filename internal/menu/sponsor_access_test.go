package menu

import (
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

func TestCanAccessSponsorMenu(t *testing.T) {
	cfg := config.ServerConfig{
		SysOpLevel:   255,
		CoSysOpLevel: 250,
	}

	generalArea := &message.MessageArea{
		ID:      1,
		Tag:     "GENERAL",
		Sponsor: "Alice",
	}
	otherArea := &message.MessageArea{
		ID:      2,
		Tag:     "OTHER",
		Sponsor: "Bob",
	}
	noSponsorArea := &message.MessageArea{
		ID:  3,
		Tag: "EMPTY",
	}

	tests := []struct {
		name string
		u    *user.User
		area *message.MessageArea
		want bool
	}{
		{
			name: "sysop always has access",
			u:    &user.User{Handle: "SysOp", AccessLevel: 255},
			area: generalArea,
			want: true,
		},
		{
			name: "cosysop has access",
			u:    &user.User{Handle: "CoSysOp", AccessLevel: 250},
			area: generalArea,
			want: true,
		},
		{
			name: "named sponsor has access (exact case)",
			u:    &user.User{Handle: "Alice", AccessLevel: 10},
			area: generalArea,
			want: true,
		},
		{
			name: "named sponsor has access (different case)",
			u:    &user.User{Handle: "ALICE", AccessLevel: 10},
			area: generalArea,
			want: true,
		},
		{
			name: "named sponsor has access (lower case)",
			u:    &user.User{Handle: "alice", AccessLevel: 10},
			area: generalArea,
			want: true,
		},
		{
			name: "regular user without sponsor status is denied",
			u:    &user.User{Handle: "Regular", AccessLevel: 10},
			area: generalArea,
			want: false,
		},
		{
			name: "sponsor of a different area is denied",
			u:    &user.User{Handle: "Alice", AccessLevel: 10},
			area: otherArea,
			want: false,
		},
		{
			name: "area with no sponsor — regular user denied",
			u:    &user.User{Handle: "Nobody", AccessLevel: 10},
			area: noSponsorArea,
			want: false,
		},
		{
			name: "area with no sponsor — sysop still gets access",
			u:    &user.User{Handle: "SysOp", AccessLevel: 255},
			area: noSponsorArea,
			want: true,
		},
		{
			name: "nil user returns false",
			u:    nil,
			area: generalArea,
			want: false,
		},
		{
			name: "nil area returns false",
			u:    &user.User{Handle: "Alice", AccessLevel: 10},
			area: nil,
			want: false,
		},
		{
			name: "level just below cosysop is denied",
			u:    &user.User{Handle: "AlmostCo", AccessLevel: 249},
			area: generalArea,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CanAccessSponsorMenu(tt.u, tt.area, cfg)
			if got != tt.want {
				t.Errorf("CanAccessSponsorMenu() = %v, want %v", got, tt.want)
			}
		})
	}
}

type fakeAreaLookup struct {
	areas map[int]*message.MessageArea
}

func (f fakeAreaLookup) GetAreaByID(id int) (*message.MessageArea, bool) {
	a, ok := f.areas[id]
	return a, ok
}

func TestSponsorKeyword(t *testing.T) {
	cfg := config.ServerConfig{SysOpLevel: 255, CoSysOpLevel: 250}
	lookup := fakeAreaLookup{areas: map[int]*message.MessageArea{
		1: {ID: 1, Tag: "GENERAL", Sponsor: "Alice"},
		2: {ID: 2, Tag: "EMPTY"},
	}}

	tests := []struct {
		name string
		u    *user.User
		want bool
	}{
		{"sponsor of current area", &user.User{Handle: "Alice", AccessLevel: 50, CurrentMessageAreaID: 1}, true},
		{"sponsor match is case-insensitive", &user.User{Handle: "alice", AccessLevel: 50, CurrentMessageAreaID: 1}, true},
		{"non-sponsor denied", &user.User{Handle: "Mallory", AccessLevel: 50, CurrentMessageAreaID: 1}, false},
		{"cosysop allowed without sponsorship", &user.User{Handle: "Carol", AccessLevel: 250, CurrentMessageAreaID: 2}, true},
		{"no current area denied", &user.User{Handle: "Alice", AccessLevel: 50, CurrentMessageAreaID: 0}, false},
		{"unknown area id denied", &user.User{Handle: "Alice", AccessLevel: 50, CurrentMessageAreaID: 99}, false},
		{"nil user denied", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sponsorKeyword(tt.u, lookup, cfg); got != tt.want {
				t.Errorf("sponsorKeyword() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSponsorKeywordNilLookup(t *testing.T) {
	u := &user.User{Handle: "Alice", AccessLevel: 50, CurrentMessageAreaID: 1}
	if sponsorKeyword(u, nil, config.ServerConfig{}) {
		t.Error("sponsorKeyword() with nil lookup should be false")
	}
}
