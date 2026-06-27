package menu

import (
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/message"
)

func TestOwnPrivateMailFilter(t *testing.T) {
	filter := ownPrivateMailFilter("Alice")
	cases := []struct {
		name string
		msg  *message.DisplayMessage
		want bool
	}{
		{"private to self", &message.DisplayMessage{IsPrivate: true, To: "Alice"}, true},
		{"private to self case-insensitive", &message.DisplayMessage{IsPrivate: true, To: "alice"}, true},
		{"private to someone else", &message.DisplayMessage{IsPrivate: true, To: "Bob"}, false},
		{"public addressed to self", &message.DisplayMessage{IsPrivate: false, To: "Alice"}, false},
		{"public to someone else", &message.DisplayMessage{IsPrivate: false, To: "Bob"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := filter(tc.msg); got != tc.want {
				t.Errorf("filter(%+v) = %v, want %v", tc.msg, got, tc.want)
			}
		})
	}
}
