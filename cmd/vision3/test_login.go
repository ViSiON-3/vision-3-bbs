package main

import (
	"log/slog"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/session"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// A test account (user.User.TestAccount) is one the sysop set aside for
// automated checks. Its logins write no caller history, so they are not in
// Last Callers, the board's total or the network's last calls; they are not
// counted in the day's calls; and the session is invisible, so the logon is
// not announced to other nodes or the network and only a sysop sees it in
// Who's Online. The server log still records each one.

// setSessionUser makes u the session's caller. When u is a test account
// the session is marked a test login, and invisible, under the same lock,
// so nothing that lists or counts sessions sees the caller without the
// mark; the test login is logged. It reports whether u is a test account.
func setSessionUser(sess *session.BbsSession, u *user.User, node int, log *slog.Logger) bool {
	test := u != nil && u.TestAccount
	sess.Mutex.Lock()
	sess.User = u
	if test {
		sess.Invisible = true
		sess.TestLogin = true
	}
	sess.Mutex.Unlock()
	if !test {
		return false
	}
	log.Info("test account login: not recorded, counted or announced", "node", node, "user", u.Handle, "userID", u.ID, "testLogin", true)
	return true
}

// offersInvisibleLogon reports whether u is asked whether to add this login
// to the last callers: callers at or above invisibleLevel (the co-sysop
// level when it is 0), except a test account, which is never asked.
func offersInvisibleLogon(u *user.User, cfg config.ServerConfig) bool {
	if u.TestAccount {
		return false
	}
	level := cfg.InvisibleLevel
	if level == 0 {
		level = cfg.CoSysOpLevel // backward compat: missing config uses coSysOpLevel
	}
	return u.AccessLevel >= level
}

// callRecorder is the part of user.UserMgr that keeps the caller history.
type callRecorder interface {
	AddCallRecord(user.CallRecord)
}

// recordCall adds rec to the caller history, marked invisible if the
// session was, unless the session is a test login: that call is only
// logged.
func recordCall(um callRecorder, sess *session.BbsSession, rec user.CallRecord, log *slog.Logger) {
	if sess != nil {
		sess.Mutex.RLock()
		rec.Invisible = sess.Invisible
		test := sess.TestLogin
		sess.Mutex.RUnlock()
		if test {
			log.Info("test account logoff: no call record written", "node", rec.NodeID, "user", rec.Handle, "userID", rec.UserID, "testLogin", true)
			return
		}
	}
	um.AddCallRecord(rec)
}
