package user

import "testing"

// TestResolveRecipient pins how a name on mail maps to an account: handle
// first, then "Sysop" to user #1, then a unique real name; ambiguous or
// deleted matches resolve to nothing.
func TestResolveRecipient(t *testing.T) {
	um := NewUserMgrForTest(
		&User{ID: 1, Handle: "Felonius", RealName: "Robbie W"},
		&User{ID: 2, Handle: "Bob", RealName: "Bob Builder"},
		&User{ID: 3, Handle: "Twin1", RealName: "Pat Same"},
		&User{ID: 4, Handle: "Twin2", RealName: "Pat Same"},
		&User{ID: 5, Handle: "Gone", RealName: "Del Eted", DeletedUser: true},
		&User{ID: 6, Handle: "Bob Builder", RealName: "Someone Else"},
	)
	cases := []struct {
		name string
		want string // handle, or "" for no match
	}{
		{"bob", "Bob"},
		{"  FELONIUS ", "Felonius"},
		{"Sysop", "Felonius"},
		{"robbie w", "Felonius"},
		{"Bob Builder", "Bob Builder"}, // a handle beats another user's real name
		{"Pat Same", ""},               // ambiguous
		{"Gone", ""},                   // deleted
		{"Del Eted", ""},               // deleted
		{"Nobody Here", ""},
		{"", ""},
	}
	for _, tc := range cases {
		u, ok := um.ResolveRecipient(tc.name)
		got := ""
		if ok {
			got = u.Handle
		}
		if got != tc.want {
			t.Errorf("ResolveRecipient(%q) = %q, want %q", tc.name, got, tc.want)
		}
	}
	if !um.HandleExists("gone") || um.HandleExists("Robbie W") || um.HandleExists("") {
		t.Error("HandleExists: want true for a deleted account's handle, false for a real name or blank")
	}
}
