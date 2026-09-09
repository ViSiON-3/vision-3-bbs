package menu

import (
	"fmt"
	"log/slog"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// notifySysopsOfNewUser queues a page on every online co-sysop-or-above session
// telling them a signup just completed.
//
// It is deliberately independent of AutoValidateNewUsers. An auto-validated
// signup raises nothing to review, but "somebody joined" is still news a sysop
// wants at the time it happens rather than at their next login.
//
// Delivery uses the existing page queue, so the notice appears at the
// recipient's next menu prompt rather than interrupting whatever they are in
// the middle of. Nothing here blocks or fails the signup: a missing registry,
// an absent string, or nobody online are all ordinary outcomes.
//
// Returns the number of sessions paged, which the tests assert on.
func (e *MenuExecutor) notifySysopsOfNewUser(newUser *user.User, nodeNumber int) int {
	if newUser == nil || e.SessionRegistry == nil {
		return 0
	}
	if !e.GetServerConfig().NotifySysopNewUser {
		return 0
	}

	format := e.LoadedStrings.NewUserSysopPage
	if format == "" {
		// No configured text means no notice, rather than a blank line
		// appearing at a sysop's prompt with no explanation.
		slog.Debug("new-user sysop notice skipped: newUserSysopPage is empty", "node", nodeNumber)
		return 0
	}
	msg := fmt.Sprintf(format, newUser.Handle, nodeNumber)

	paged := 0
	for _, sess := range e.SessionRegistry.ListActive() {
		if sess == nil {
			continue
		}
		sess.Mutex.RLock()
		target := sess.User
		targetNode := sess.NodeID
		sess.Mutex.RUnlock()

		// Skip the node that just signed up. The new account cannot be a
		// co-sysop, but the session is still mid-signup and its own arrival is
		// not news to it.
		if targetNode == nodeNumber {
			continue
		}
		if !e.isCoSysOpOrAbove(target) {
			continue
		}
		sess.AddPage(msg)
		paged++
	}

	if paged > 0 {
		slog.Info("paged sysops about a new user", "node", nodeNumber, "handle", newUser.Handle, "recipients", paged)
	}
	return paged
}
