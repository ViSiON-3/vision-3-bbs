package menu

import (
	"fmt"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/session"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// announceTTL is how long a logon, logoff or post announcement waits for a
// caller who cannot be shown it yet, such as one in a door. After that it is
// old news.
const announceTTL = 60 * time.Second

// AnnounceLogon tells other online callers that u has logged on (#206).
func (e *MenuExecutor) AnnounceLogon(sess *session.BbsSession, u *user.User) {
	format := e.Strings().ToastLogon
	if sess == nil || u == nil || format == "" || !e.GetServerConfig().ToastLogons {
		return
	}
	e.announce(sess, u, fmt.Sprintf(e.Strings().ToastLogon, threadEscapePipes(u.Handle), sess.NodeID), nil)
}

// AnnounceLogoff tells other online callers that u has logged off (#206).
func (e *MenuExecutor) AnnounceLogoff(sess *session.BbsSession, u *user.User) {
	format := e.Strings().ToastLogoff
	if sess == nil || u == nil || format == "" || !e.GetServerConfig().ToastLogoffs {
		return
	}
	e.announce(sess, u, fmt.Sprintf(e.Strings().ToastLogoff, threadEscapePipes(u.Handle), sess.NodeID), nil)
}

// AnnouncePost tells the other online callers who may read area that u has
// just posted in it (#1001). Call it only after the post was saved. Private
// mail, netmail and anonymous posts are never announced. Posts that arrive by
// import, upload or script never come through here, so a post from another
// board reaches callers here only as its V3Net Central alert, never also as
// this toast; and a post made here is never shown here as an alert, which the
// leaf drops for its own board.
//
// Who sees it follows the logon toasts: past login, at or above
// toastAnnounceLevel, not the poster's own sessions. On top of that the
// viewer must pass the area's read ACS and its conference's (on their stored
// record when um is given, and their own connection), and their Twit List
// must not hide the poster.
func (e *MenuExecutor) AnnouncePost(um *user.UserMgr, sess *session.BbsSession, u *user.User, area *message.MessageArea, private, anonymous bool) {
	format := e.Strings().ToastPost
	if sess == nil || u == nil || area == nil || format == "" || !e.GetServerConfig().ToastPosts {
		return
	}
	if private || anonymous || isNetmailArea(area) || composeRecipientKindFor(area) == recipientPrivate {
		return
	}
	msg := fmt.Sprintf(format, threadEscapePipes(u.Handle), threadEscapePipes(alertAreaName(area)))
	e.announce(sess, u, msg, func(viewer *session.BbsSession, vu *user.User) bool {
		acct := vu
		if um != nil {
			stored, ok := um.GetUserByID(vu.ID)
			if !ok || stored.DeletedUser {
				return false
			}
			acct = stored
		}
		viewer.Mutex.RLock()
		start := viewer.StartTime
		viewer.Mutex.RUnlock()
		return e.mayReadAlertArea(acct, viewer, start, area) && !twitMatcher(e, um, acct).Hidden(u.Handle, "")
	})
}

// announce sends msg to every other caller allowed to see it, and, when
// allow is given, for whom it also says yes. An invisible caller is never
// announced, and sessions still logging in are skipped: they were not here
// for the news. These are live only and never queued for login.
func (e *MenuExecutor) announce(from *session.BbsSession, u *user.User, msg string, allow func(viewer *session.BbsSession, vu *user.User) bool) {
	if e.SessionRegistry == nil {
		return
	}
	from.Mutex.RLock()
	invisible, node := from.Invisible, from.NodeID
	from.Mutex.RUnlock()
	if invisible {
		return
	}
	level := e.GetServerConfig().ToastAnnounceLevel
	for _, sess := range e.SessionRegistry.ListActive() {
		sess.Mutex.RLock()
		viewer, viewerNode := sess.User, sess.NodeID
		sess.Mutex.RUnlock()
		if viewerNode == node || viewer == nil || viewer.ID == u.ID || !sess.ToastsReady() {
			continue
		}
		if viewerLevel, _, ok := sess.ToastViewer(); !ok || viewerLevel < level {
			continue
		}
		if allow != nil && !allow(sess, viewer) {
			continue
		}
		sess.AddExpiringPage(msg, announceTTL)
	}
}
