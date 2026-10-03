package main

import (
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

func TestConnectEncoding(t *testing.T) {
	tests := []struct {
		name           string
		byName, probed ansi.OutputMode
		want           ansi.OutputMode
	}{
		{"probe overrides a CP437 name", ansi.OutputModeCP437, ansi.OutputModeUTF8, ansi.OutputModeUTF8},
		{"probe overrides a UTF-8 name", ansi.OutputModeUTF8, ansi.OutputModeCP437, ansi.OutputModeCP437},
		{"no probe answer keeps the name", ansi.OutputModeCP437, ansi.OutputModeAuto, ansi.OutputModeCP437},
	}
	for _, tt := range tests {
		if got := connectEncoding(tt.byName, tt.probed); got != tt.want {
			t.Errorf("%s: connectEncoding = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestLoginEncoding(t *testing.T) {
	tests := []struct {
		detected ansi.OutputMode
		pref     string
		want     ansi.OutputMode
	}{
		{ansi.OutputModeUTF8, "", ansi.OutputModeUTF8},
		{ansi.OutputModeCP437, "", ansi.OutputModeCP437},
		{ansi.OutputModeUTF8, "cp437", ansi.OutputModeCP437},
		{ansi.OutputModeCP437, "utf8", ansi.OutputModeUTF8},
		{ansi.OutputModeCP437, "bogus", ansi.OutputModeCP437},
	}
	for _, tt := range tests {
		if got := loginEncoding(tt.detected, tt.pref); got != tt.want {
			t.Errorf("loginEncoding(%v, %q) = %v, want %v", tt.detected, tt.pref, got, tt.want)
		}
	}
}

// An unset encoding means detect on every call; it is no reason to run
// first-login setup.
func TestNeedsFirstLoginSetup(t *testing.T) {
	tests := []struct {
		name string
		u    user.User
		want bool
	}{
		{"size and encoding set", user.User{ScreenWidth: 80, ScreenHeight: 25, PreferredEncoding: "utf8"}, false},
		{"encoding unset", user.User{ScreenWidth: 80, ScreenHeight: 25}, false},
		{"width unset", user.User{ScreenHeight: 25}, true},
		{"height unset", user.User{ScreenWidth: 80}, true},
	}
	for _, tt := range tests {
		if got := needsFirstLoginSetup(&tt.u); got != tt.want {
			t.Errorf("%s: needsFirstLoginSetup = %v, want %v", tt.name, got, tt.want)
		}
	}
}
