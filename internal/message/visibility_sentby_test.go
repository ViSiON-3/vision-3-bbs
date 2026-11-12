package message

import "testing"

// SentBy names the sender by SenderHandle when a post records one, else by
// From, for private mail only, ignoring case and surrounding space.
func TestSentBy(t *testing.T) {
	for _, tc := range []struct {
		from, sender, handle string
		private, want        bool
	}{
		{"Caller", "", "caller", true, true},
		{" Caller ", "", "Caller", true, true},
		{"Carl Caller", "Caller", "Caller", true, true}, // signed with a real name
		{"Caller", "Someone", "Caller", true, false},    // the recorded handle decides
		{"Carl Caller", "Caller", "Carl Caller", true, false},
		{"Caller", "", "Caller", false, false}, // public: not mail
		{"Caller", "", " ", true, false},       // blank handle
	} {
		m := &DisplayMessage{From: tc.from, SenderHandle: tc.sender, IsPrivate: tc.private}
		if got := m.SentBy(tc.handle); got != tc.want {
			t.Errorf("SentBy(%q) on from %q sender %q private %v = %v, want %v", tc.handle, tc.from, tc.sender, tc.private, got, tc.want)
		}
	}
}
