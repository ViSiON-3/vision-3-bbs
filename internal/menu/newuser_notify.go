package menu

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// notifySysopsOfNewUser tells every co-sysop-or-above account that a signup just
// completed. Online sysops are paged immediately; offline sysops get a
// persistent notice queued for their next login, since a sysop is rarely online
// at the moment someone signs up.
//
// It is deliberately independent of AutoValidateNewUsers: an auto-validated
// signup raises nothing to review, but "somebody joined" is still news worth
// having — informational, not a call to action.
//
// Nothing here blocks or fails the signup: a missing registry, an absent string,
// nobody configured, or a write error are all ordinary, non-fatal outcomes.
//
// Returns (paged, queued): how many online sessions were paged and how many
// offline accounts had a notice queued. The tests assert on both.
func (e *MenuExecutor) notifySysopsOfNewUser(userManager *user.UserMgr, newUser *user.User, nodeNumber int) (paged, queued int) {
	if newUser == nil {
		return 0, 0
	}
	if !e.GetServerConfig().NotifySysopNewUser {
		return 0, 0
	}

	format := e.Strings().NewUserSysopPage
	if format == "" {
		// No configured text means no notice, rather than a blank line
		// appearing at a sysop's prompt with no explanation.
		slog.Debug("new-user sysop notice skipped: newUserSysopPage is empty", "node", nodeNumber)
		return 0, 0
	}
	msg := fmt.Sprintf(format, newUser.Handle, nodeNumber)

	// Page online co-sysop+ sessions, and remember which accounts we reached so
	// they are not also queued a login notice for the same event.
	pagedIDs := map[int]bool{}
	if e.SessionRegistry != nil {
		for _, sess := range e.SessionRegistry.ListActive() {
			if sess == nil {
				continue
			}
			sess.Mutex.RLock()
			target := sess.User
			targetNode := sess.NodeID
			sess.Mutex.RUnlock()

			// Skip the node that just signed up: it is still mid-signup and its
			// own arrival is not news to it.
			if targetNode == nodeNumber {
				continue
			}
			if !e.isCoSysOpOrAbove(target) {
				continue
			}
			sess.AddPage(msg)
			paged++
			pagedIDs[target.ID] = true
		}
	}

	// Queue a persistent notice for every co-sysop+ account that was not paged
	// (i.e. offline), so the news survives to their next login.
	if userManager != nil {
		path := sysopNoticesPath(e.GetServerConfig().DataDir)
		for _, u := range userManager.GetAllUsers() {
			if u == nil || u.DeletedUser || u.ID == newUser.ID || pagedIDs[u.ID] {
				continue
			}
			if !e.isCoSysOpOrAbove(u) {
				continue
			}
			// Handle and node travel with the notice so the login step can
			// render it against the clock it is read on: "just signed up" is
			// true of the page above, not of a notice read on the sysop's next
			// call. msg is kept as the fallback text — see sysopNotice.
			notice := sysopNotice{Text: msg, Handle: newUser.Handle, Node: nodeNumber, CreatedAt: time.Now()}
			if err := enqueueSysopNotice(path, u.ID, notice); err != nil {
				slog.Warn("failed to queue new-user notice for offline sysop",
					"node", nodeNumber, "recipient", u.Handle, "error", err)
				continue
			}
			queued++
		}
	}

	if paged > 0 || queued > 0 {
		slog.Info("notified sysops about a new user",
			"node", nodeNumber, "handle", newUser.Handle, "paged", paged, "queued", queued)
	}
	return paged, queued
}
