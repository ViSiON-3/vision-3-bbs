package menu

import (
	"log/slog"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// continueAsNewUser decides whether a caller who has just created an account
// should be carried straight into a session as that account, and if so does
// the login bookkeeping for it.
//
// Returns nil when they should not be — either no account was created, or its
// access level is below logonLevel, in which case continuing would only bounce
// them back out. Validation is deliberately not consulted: it does not gate
// login, so letting it gate this would reintroduce the confusion #222 fixed.
//
// The bookkeeping goes through UserMgr.BeginSession, the same call Authenticate
// makes once a password verifies. Stamping lastLogin and timesCalled here by
// hand would work until the two drifted, and a first call that goes unrecorded
// is exactly the kind of gap that stays invisible.
func (e *MenuExecutor) continueAsNewUser(userManager *user.UserMgr, newUser *user.User, nodeNumber int) *user.User {
	if newUser == nil {
		return nil
	}
	if !canLogonAtLevel(e.GetServerConfig(), newUser.AccessLevel) {
		slog.Info("new account cannot log on yet; returning to the login prompt",
			"node", nodeNumber, "handle", newUser.Handle, "level", newUser.AccessLevel)
		return nil
	}

	started, ok := userManager.BeginSession(newUser.Handle)
	if !ok {
		// The account was created a moment ago, so this should not happen;
		// fall back to a normal login rather than proceeding without the
		// bookkeeping done.
		slog.Warn("could not start a session for the new account; falling back to the login prompt",
			"node", nodeNumber, "handle", newUser.Handle)
		return nil
	}

	slog.Info("continuing into the session as the newly created account",
		"node", nodeNumber, "handle", started.Handle, "level", started.AccessLevel)
	return started
}
