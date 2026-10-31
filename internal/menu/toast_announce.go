package menu

import (
	"fmt"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/session"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// announceTTL is how long a logon or logoff announcement waits for a caller who
// cannot be shown it yet, such as one in a door. After that it is old news.
const announceTTL = 60 * time.Second

// AnnounceLogon tells other online callers that u has logged on (#206).
func (e *MenuExecutor) AnnounceLogon(sess *session.BbsSession, u *user.User) {
	format := e.Strings().ToastLogon
	if sess == nil || u == nil || format == "" || !e.GetServerConfig().ToastLogons {
		return
	}
	e.announce(sess, u, fmt.Sprintf(e.Strings().ToastLogon, threadEscapePipes(u.Handle), sess.NodeID))
}

// AnnounceLogoff tells other online callers that u has logged off (#206).
func (e *MenuExecutor) AnnounceLogoff(sess *session.BbsSession, u *user.User) {
	format := e.Strings().ToastLogoff
	if sess == nil || u == nil || format == "" || !e.GetServerConfig().ToastLogoffs {
		return
	}
	e.announce(sess, u, fmt.Sprintf(e.Strings().ToastLogoff, threadEscapePipes(u.Handle), sess.NodeID))
}

// announce sends msg to every other caller allowed to see it. An invisible
// caller is never announced, and sessions still logging in are skipped: they
// were not here for the news. These are live only and never queued for login.
func (e *MenuExecutor) announce(from *session.BbsSession, u *user.User, msg string) {
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
		sess.AddExpiringPage(msg, announceTTL)
	}
}
