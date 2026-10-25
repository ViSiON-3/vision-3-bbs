package menu

import (
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// TestPostingName pins the name new posts and replies are signed with: the
// real name in a real_name_only area, the handle otherwise, and always the
// handle on private mail outside netmail.
func TestPostingName(t *testing.T) {
	realOnly := &message.MessageArea{Tag: "FIDOECHO", AreaType: "echomail", RealNameOnly: true}
	plain := &message.MessageArea{Tag: "GENERAL", AreaType: "local"}
	poster := &user.User{Handle: "pat", RealName: "Pat Example"}
	noReal := &user.User{Handle: "pat"}

	tests := []struct {
		desc         string
		area         *message.MessageArea
		u            *user.User
		signByHandle bool
		want         string
	}{
		{"real-name area uses the real name", realOnly, poster, false, "Pat Example"},
		{"plain area uses the handle", plain, poster, false, "pat"},
		{"private mail keeps the handle", realOnly, poster, true, "pat"},
		{"no real name falls back to the handle", realOnly, noReal, false, "pat"},
		{"a missing area uses the handle", nil, poster, false, "pat"},
	}
	for _, tt := range tests {
		if got := postingName(tt.area, tt.u, tt.signByHandle); got != tt.want {
			t.Errorf("%s: postingName = %q, want %q", tt.desc, got, tt.want)
		}
	}
}
