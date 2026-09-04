package menu

import (
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// newscanSince returns the timestamp that "new since your last visit" is
// measured from.
//
// This is deliberately PreviousLogin and not LastLogin. Authenticate() stamps
// LastLogin with time.Now() at the start of the session, before the login
// sequence runs, so comparing against it means "new in the last few seconds"
// and never matches anything. Every newscan-style feature — system news,
// rumors, the file newscan — must use this.
func newscanSince(u *user.User) time.Time {
	if u == nil {
		return time.Time{}
	}
	return u.PreviousLogin
}

// isNewSince reports whether something posted at postedAt should count as new
// for a caller whose last visit was since. A zero cutoff means the user has no
// previous visit, so everything is new to them.
func isNewSince(postedAt, since time.Time) bool {
	if since.IsZero() {
		return true
	}
	return postedAt.After(since)
}
