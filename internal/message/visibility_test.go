package message

import "testing"

// TestVisibleTo pins who may read a message: anyone for public mail; for
// private mail only the sender or recipient by handle, case-insensitively.
// A real name is never an identity, so adopting someone's real name grants
// nothing, and a blank handle sees no private mail.
func TestVisibleTo(t *testing.T) {
	priv := &DisplayMessage{IsPrivate: true, From: "Sysop", To: "Bob"}
	pub := &DisplayMessage{From: "Sysop", To: "Bob"}
	cases := []struct {
		name   string
		m      *DisplayMessage
		handle string
		want   bool
	}{
		{"public, stranger", pub, "Carol", true},
		{"public, no user", pub, "", true},
		{"recipient", priv, "bob", true},
		{"sender", priv, "SYSOP", true},
		{"surrounding space ignored", priv, "  Bob ", true},
		{"stranger", priv, "Carol", false},
		{"no user", priv, "", false},
		{"blank handle never matches blank fields", &DisplayMessage{IsPrivate: true}, "", false},
		{"addressed by real name is not the handle's", &DisplayMessage{IsPrivate: true, From: "x", To: "Bob Builder"}, "Bob", false},
	}
	for _, tc := range cases {
		if got := tc.m.VisibleTo(tc.handle); got != tc.want {
			t.Errorf("%s: VisibleTo(%q) = %v, want %v", tc.name, tc.handle, got, tc.want)
		}
	}
}
