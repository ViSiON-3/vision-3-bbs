package menu

import (
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// TestNewUserSysopRecipient covers who a new user's introduction message is
// addressed to: user #1, unless that account is the caller's own freshly
// created account (a brand-new board's first signup), in which case there is no
// distinct SysOp to write to.
func TestNewUserSysopRecipient(t *testing.T) {
	sysop := &user.User{ID: 1, Handle: "SysOp"}
	newbie := &user.User{ID: 2, Handle: "Newbie"}

	t.Run("addresses user #1 for a later signup", func(t *testing.T) {
		um := user.NewUserMgrForTest(sysop, newbie)
		got, ok := newUserSysopRecipient(um, newbie.ID)
		if !ok || got == nil || got.ID != 1 {
			t.Fatalf("got %+v ok=%v, want user #1", got, ok)
		}
	})

	t.Run("no recipient when the new user is user #1", func(t *testing.T) {
		um := user.NewUserMgrForTest(sysop)
		if got, ok := newUserSysopRecipient(um, sysop.ID); ok || got != nil {
			t.Fatalf("got %+v ok=%v, want no recipient", got, ok)
		}
	})

	t.Run("no recipient when user #1 is absent", func(t *testing.T) {
		um := user.NewUserMgrForTest(&user.User{ID: 7, Handle: "Someone"})
		if got, ok := newUserSysopRecipient(um, 99); ok || got != nil {
			t.Fatalf("got %+v ok=%v, want no recipient", got, ok)
		}
	})
}
