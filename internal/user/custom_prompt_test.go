package user

import (
	"strings"
	"testing"
)

func TestCleanCustomPrompt(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"", ""},
		{"  |11Mine>  ", "|11Mine>"},
		{"one\r\ntwo\x1b[2J\x07", "onetwo[2J"},
		{"\u009b2J", "2J"},
		{strings.Repeat("x", 100), strings.Repeat("x", CustomPromptMaxLen)},
		{strings.Repeat("é", 90), strings.Repeat("é", CustomPromptMaxLen)},
	} {
		if got := CleanCustomPrompt(tc.in); got != tc.want {
			t.Errorf("CleanCustomPrompt(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
