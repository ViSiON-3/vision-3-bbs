package protocol

import "testing"

func TestParseChatTarget(t *testing.T) {
	for _, tc := range []struct {
		in, handle, node string
		ok               bool
	}{
		{"Bob@n1", "Bob", "n1", true},
		{" Pat Twin @ n2 ", "Pat Twin", "n2", true},
		{"a@b@c", "a@b", "c", true},
		{"Bob", "", "", false},
		{"@n1", "", "", false},
		{"Bob@", "", "", false},
		{"Bob@n 1", "", "", false},
		{"Bob\x1b@n1", "", "", false},
	} {
		handle, node, ok := ParseChatTarget(tc.in)
		if ok != tc.ok || handle != tc.handle || node != tc.node {
			t.Errorf("ParseChatTarget(%q) = (%q, %q, %v), want (%q, %q, %v)", tc.in, handle, node, ok, tc.handle, tc.node, tc.ok)
		}
	}
}
