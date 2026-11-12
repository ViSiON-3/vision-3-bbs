package menu

import (
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/logging"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// loginPasswordResult is what checkLoginPassword found.
type loginPasswordResult int

const (
	loginPasswordOK     loginPasswordResult = iota // the password is the account's
	loginPasswordWrong                             // no such account, or the wrong password
	loginPasswordLocked                            // ip is locked out; the password wasn't checked
)

// checkLoginPassword checks a password typed at the LOGIN menu, keeping the
// per-IP failed-login lockout. A locked-out ip isn't checked at all, and a
// wrong password is counted against it (justLocked reports that this one
// locked ip out). With loginPasswordLocked, lockedUntil and attempts are
// the lockout's. On a match it returns a snapshot of the account; the call
// isn't recorded and the failed attempts aren't cleared until the login is
// admitted (admitLogin), after the checks that follow the password.
//
// Checks from one ip run one at a time, together with SSH's
// (IPLockoutChecker.LoginAttempt), from the lockout check to recording the
// outcome. Otherwise sessions from one address that send their passwords
// together all pass the lockout check before any failure is recorded.
func (e *MenuExecutor) checkLoginPassword(um *user.UserMgr, handle, password, ip string) (result loginPasswordResult, account *user.User, lockedUntil time.Time, attempts int, justLocked bool) {
	if e.IPLockoutCheck != nil {
		done := e.IPLockoutCheck.LoginAttempt(ip)
		defer done()
		if locked, until, n := e.IPLockoutCheck.IsIPLockedOut(ip); locked {
			return loginPasswordLocked, nil, until, n, false
		}
	}
	if um.VerifyPassword(handle, password) {
		if u, ok := um.GetUser(handle); ok {
			return loginPasswordOK, u, time.Time{}, 0, false
		}
	}
	if e.IPLockoutCheck != nil {
		justLocked = e.IPLockoutCheck.RecordFailedLoginAttempt(ip)
	}
	return loginPasswordWrong, nil, time.Time{}, 0, justLocked
}

// loginPasswordChecked runs between a LOGIN password checking out and the
// login being admitted. It does nothing; tests set it to play another login
// from the same address locking it out in between.
var loginPasswordChecked = func(ip string) {}

// admitLogin lets in a login from ip whose password checked out and that
// passed the checks after it, and records the call (BeginSession). The
// lockout is checked again as the failed attempts are cleared, in one step
// (IPLockoutChecker.AdmitLogin): another login from ip may have locked it out
// since the password was checked. Then it returns admitted false and nil,
// and nothing is recorded. It also returns nil if the account is gone.
func (e *MenuExecutor) admitLogin(um *user.UserMgr, handle, ip string, nodeNumber int) (authUser *user.User, admitted bool) {
	loginPasswordChecked(ip)
	if e.IPLockoutCheck != nil && !e.IPLockoutCheck.AdmitLogin(ip) {
		logging.Security("login from IP locked out after its password was checked",
			"node", nodeNumber, "handle", handle, "ip", ip)
		return nil, false
	}
	authUser, _ = um.BeginSession(handle)
	return authUser, true
}
