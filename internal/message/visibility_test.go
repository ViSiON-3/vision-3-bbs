package message

import "testing"

// TestVisibleTo pins who may read a message: anyone for public mail; for
// private mail only the sender or recipient, by handle or real name,
// case-insensitively; and never a user with no names.
func TestVisibleTo(t *testing.T) {
	priv := &DisplayMessage{IsPrivate: true, From: "Sam Sysop", To: "Bob"}
	pub := &DisplayMessage{From: "Sam Sysop", To: "Bob"}
	cases := []struct {
		name         string
		m            *DisplayMessage
		handle, real string
		want         bool
	}{
		{"public, stranger", pub, "Carol", "Carol Singer", true},
		{"public, no user", pub, "", "", true},
		{"recipient by handle", priv, "bob", "Bob Builder", true},
		{"sender by real name", priv, "Sysop", "sam sysop", true},
		{"recipient by real name", &DisplayMessage{IsPrivate: true, From: "x", To: "Bob Builder"}, "Bob", "Bob Builder", true},
		{"stranger", priv, "Carol", "Carol Singer", false},
		{"no user", priv, "", "", false},
		{"blank names never match blank fields", &DisplayMessage{IsPrivate: true}, "", "", false},
		{"surrounding space ignored", priv, "  Bob ", "", true},
	}
	for _, tc := range cases {
		if got := tc.m.VisibleTo(tc.handle, tc.real); got != tc.want {
			t.Errorf("%s: VisibleTo(%q, %q) = %v, want %v", tc.name, tc.handle, tc.real, got, tc.want)
		}
	}
}
