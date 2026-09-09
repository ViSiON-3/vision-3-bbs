package menu

import (
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// TestRecordAbandonedIntroAttempt covers the reconnect/removal rule: each
// abandoned attempt bumps the counter and keeps the account pending, until the
// limit is reached and the account is soft-deleted.
func TestRecordAbandonedIntroAttempt(t *testing.T) {
	now := time.Unix(1000, 0)
	u := &user.User{ID: 2, Handle: "Newbie", IntroPending: true}

	// Attempts below the limit keep the account alive and still pending.
	for i := 1; i < newUserIntroMaxAttempts; i++ {
		if removed := recordAbandonedIntroAttempt(u, now); removed {
			t.Fatalf("attempt %d: removed=true, want false", i)
		}
		if u.IntroAttempts != i {
			t.Fatalf("attempt %d: IntroAttempts=%d, want %d", i, u.IntroAttempts, i)
		}
		if !u.IntroPending {
			t.Fatalf("attempt %d: IntroPending=false, want true", i)
		}
		if u.DeletedUser {
			t.Fatalf("attempt %d: DeletedUser=true, want false", i)
		}
	}

	// The attempt that reaches the limit soft-deletes the account and stops
	// nagging (IntroPending cleared, DeletedAt stamped).
	if removed := recordAbandonedIntroAttempt(u, now); !removed {
		t.Fatalf("final attempt: removed=false, want true")
	}
	if !u.DeletedUser {
		t.Fatalf("final attempt: DeletedUser=false, want true")
	}
	if u.DeletedAt == nil || !u.DeletedAt.Equal(now) {
		t.Fatalf("final attempt: DeletedAt=%v, want %v", u.DeletedAt, now)
	}
	if u.IntroPending {
		t.Fatalf("final attempt: IntroPending=true, want false")
	}
	if u.IntroAttempts != newUserIntroMaxAttempts {
		t.Fatalf("final attempt: IntroAttempts=%d, want %d", u.IntroAttempts, newUserIntroMaxAttempts)
	}
}

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
