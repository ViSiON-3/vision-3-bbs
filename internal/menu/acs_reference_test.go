package menu

import (
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/acs"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// The examples in docs/sysop/reference/acs.md, as checkACS evaluates them
// today. Each case pins behaviour the reference documents, including the
// quirks it calls out, so the page and the evaluator can't drift apart. A
// change that breaks one of these changes what an existing ACS string
// means: update the reference with it.

// acsRefUser is the example caller the reference's examples are written for.
func acsRefUser() *user.User {
	return &user.User{
		ID:                      5,
		AccessLevel:             20,
		Flags:                   "AC",
		Validated:               true,
		TimesCalled:             10,
		NumUploads:              3,
		NumDownloads:            6,
		FilePoints:              40,
		PrivateNote:             "trusted caller",
		CurrentMsgConferenceID:  2,
		CurrentFileConferenceID: 3,
	}
}

func TestACSReference(t *testing.T) {
	tests := []struct {
		name string
		acs  string
		edit func(*user.User) // changes to acsRefUser for this case
		want bool
	}{
		// Empty and wildcard
		{"empty passes", "", nil, true},
		{"wildcard passes", "*", nil, true},
		{"wildcard only alone", "**", nil, false},
		{"blank is not empty", " ", nil, false},

		// S, D: access level
		{"S at the level", "S20", nil, true},
		{"S above the level", "S21", nil, false},
		{"lower case", "s20", nil, true},
		{"D is the access level", "D20", nil, true},
		{"D above the level", "D21", nil, false},
		{"S without a number", "S", nil, false},

		// SYSOP, COSYSOP
		{"SYSOP below the level", "SYSOP", nil, false},
		{"SYSOP at the level", "sysop", func(u *user.User) { u.AccessLevel = 255 }, true},
		{"COSYSOP at the level", "COSYSOP", func(u *user.User) { u.AccessLevel = 250 }, true},

		// F, I: flags
		{"F flag set", "FA", nil, true},
		{"F case-insensitive", "fc", nil, true},
		{"F flag not set", "FB", nil, false},
		{"F takes one character", "FAC", nil, false},
		{"I is flag I", "I", func(u *user.User) { u.Flags = "I" }, true},
		{"I same as FI", "FI", func(u *user.User) { u.Flags = "I" }, true},
		{"I without flag I", "I", nil, false},
		{"I ignores its argument", "I5", func(u *user.User) { u.Flags = "I" }, true},

		// V, U
		{"V validated", "V", nil, true},
		{"V not validated", "V", func(u *user.User) { u.Validated = false }, false},
		{"V ignores its argument", "V5", nil, true},
		{"U matches the ID", "U5", nil, true},
		{"U other ID", "U6", nil, false},

		// E: uploads per call, percent, rounded down
		{"E 3 uploads in 10 calls is 30", "E30", nil, true},
		{"E above 30", "E31", nil, false},
		{"E rounds down", "E33", func(u *user.User) { u.NumUploads, u.TimesCalled = 1, 3 }, true},
		{"E rounds down, not up", "E34", func(u *user.User) { u.NumUploads, u.TimesCalled = 1, 3 }, false},
		{"E with no calls fails", "E0", func(u *user.User) { u.TimesCalled = 0 }, false},

		// B: uploads per download, percent, rounded down
		{"B 3 uploads per 6 downloads is 50", "B50", nil, true},
		{"B above 50", "B51", nil, false},
		{"B with no downloads passes", "B999", func(u *user.User) { u.NumDownloads = 0 }, true},

		// P, Z, C, X
		{"P points", "P40", nil, true},
		{"P too few points", "P41", nil, false},
		{"Z in the private note", "Ztrusted", nil, true},
		{"Z case-insensitive", "ZTRUSTED", nil, true},
		{"Z not in the note", "Zbanned", nil, false},
		{"Z with nothing passes", "Z", nil, true},
		{"C message conference", "C2", nil, true},
		{"C other conference", "C3", nil, false},
		{"X file conference", "X3", nil, true},

		// T with no time limit
		{"T no limit passes", "T999", func(u *user.User) { u.TimeLimit = 0 }, true},
		{"T within the limit", "T30", func(u *user.User) { u.TimeLimit = 60 }, true},

		// Session terms without a session
		{"L without a session", "L", nil, false},
		{"A without a session", "A", nil, false},
		{"not L without a session", "!L", nil, true},

		// Operators and precedence: ! before & before |
		{"and", "S20&FA", nil, true},
		{"or", "S99|FA", nil, true},
		{"not", "!FB", nil, true},
		{"and binds tighter than or", "S99|FA&FC", nil, true},
		{"and binds tighter, left side", "FB&FA|FC", nil, true},
		{"parentheses", "(S99|FA)&FB", nil, false},
		{"not a group", "!(FA|FB)", nil, false},
		{"spaces around operators", "S20 & FA", nil, true},

		// The Examples table
		{"validated without flag X", "V&!FX", nil, true},
		{"validated with flag X", "V&!FX", func(u *user.User) { u.Flags = "X" }, false},
		{"sysops and user 1", "SYSOP|U1", func(u *user.User) { u.ID = 1 }, true},
		{"not a sysop, not user 1", "SYSOP|U1", nil, false},
		{"level or flag, with time left", "(S50|FM)&T10", func(u *user.User) { u.Flags, u.TimeLimit = "M", 60 }, true},
		{"level or flag, neither", "(S50|FM)&T10", func(u *user.User) { u.TimeLimit = 60 }, false},

		// What doesn't work as it reads
		{"no operator between terms", "S20FA", nil, false},
		{"space is not an operator", "S20 FA", nil, false},
		{"double not is an error", "!!FA", nil, false},
		{"trailing operator", "S20&", nil, false},
		{"unclosed parenthesis", "(S20", nil, false},
		{"unknown term fails", "Q", nil, false},
		{"not an unknown term passes", "!Q", nil, true},
		{"not a bad value passes", "!Sx", nil, true},
		// a dropped character splits the term: S@20 is S and 20 with no
		// operator between them, so the string fails at any level
		{"a dropped character splits the term", "S@20", func(u *user.User) { u.AccessLevel = 99 }, false},
		// comparisons, ranges and readable names
		{"less than", "S<30", nil, true},
		{"less than, at level 99", "S<20", func(u *user.User) { u.AccessLevel = 99 }, false},
		{"a level range", "S10-30", nil, true},
		{"a readable name", "LEVEL>=20&FLAG:A", nil, true},
		{"a range that runs backwards fails as a whole", "!LEVEL=99-20", nil, false},
		// the Examples table's comparison rows
		{"example: levels below 20", "S<20", func(u *user.User) { u.AccessLevel = 19 }, true},
		{"example: levels below 20, at 20", "S<20", nil, false},
		{"example: a range with a flag", "LEVEL=20-99&FLAG:C", nil, true},
		{"example: every level but 20", "!(S=20)", nil, false},
		{"example: every level but 20, at 21", "!(S=20)", func(u *user.User) { u.AccessLevel = 21 }, true},
		{"example: level or file points", "S>=50|FILEPOINTS>=40", nil, true},
		{"parentheses don't take a flag", "F(A)", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u := acsRefUser()
			if tt.edit != nil {
				tt.edit(u)
			}
			start := time.Now()
			if got := checkACS(tt.acs, u, nil, nil, start); got != tt.want {
				t.Errorf("checkACS(%q) = %v, want %v", tt.acs, got, tt.want)
			}
			// The tester explains the same decision (#608).
			if got := acs.Explain(tt.acs, acsContext(u, nil, start)).Result; got != tt.want {
				t.Errorf("acs.Explain(%q).Result = %v, want %v", tt.acs, got, tt.want)
			}
		})
	}
}

// Before login there is no user: only an empty string and * pass.
func TestACSReference_NoUser(t *testing.T) {
	for s, want := range map[string]bool{"": true, "*": true, "S0": false, "!FA": false} {
		if got := checkACS(s, nil, nil, nil, time.Now()); got != want {
			t.Errorf("checkACS(%q, no user) = %v, want %v", s, got, want)
		}
		if got := acs.Explain(s, acsContext(nil, nil, time.Now())).Result; got != want {
			t.Errorf("acs.Explain(%q, no user).Result = %v, want %v", s, got, want)
		}
	}
}
