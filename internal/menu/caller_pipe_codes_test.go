package menu

import (
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// hostileUser is an old account whose caller-set fields hold every screen code,
// as signup and the user editors used to allow.
func hostileUser(id int) *user.User {
	return &user.User{ID: id, Handle: chatScreenCodesMsg, RealName: chatScreenCodesMsg, GroupLocation: chatScreenCodesMsg,
		PrivateNote: chatScreenCodesMsg, AccessLevel: 10, Validated: true, TimeLimit: 60}
}

// The admin screens name the user they act on, and an old handle holding |CL
// must print on them instead of clearing the sysop's screen.
func TestAdminScreensCannotRunHandleScreenCodes(t *testing.T) {
	for _, cmd := range []string{"BANUSER", "DELETEUSER", "UNVALIDATEUSER"} {
		t.Run(cmd, func(t *testing.T) {
			env := newMenuEnv(t)
			env.seedUsers(hostileUser(3))
			r := env.runCmd(cmd, env.sysop, "", "jj\rY\r\r")
			if r.err != nil {
				t.Fatalf("err = %v", r.err)
			}
			t.Logf("sysop received %q", r.raw)
			chatCheckNoScreenCodes(t, cmd, r.raw)
			if !strings.Contains(r.raw, chatScreenCodesMsg) {
				t.Errorf("the handle is not shown as typed: %q", r.raw)
			}
		})
	}
}

// A colour code in an old handle still colours it, as it does in chat.
func TestAdminScreensKeepHandleColourCodes(t *testing.T) {
	env := newMenuEnv(t)
	u := hostileUser(3)
	u.Handle = "|04Red"
	env.seedUsers(u)
	r := env.runCmd("BANUSER", env.sysop, "", "jj\rY\r\r")
	if r.err != nil {
		t.Fatalf("err = %v", r.err)
	}
	if want := string(ansi.ReplacePipeCodes([]byte("|04Red"))); !strings.Contains(r.raw, want) {
		t.Errorf("output lacks the coloured handle %q: %q", want, r.raw)
	}
}
