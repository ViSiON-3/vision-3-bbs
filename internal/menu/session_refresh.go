package menu

import (
	"log/slog"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
)

// refreshCurrentUser pulls any sysop edit made on disk into the session's user
// record, and reports whether the call should continue.
//
// Menu entry is the checkpoint. The session holds its user by value, so there
// is no single place a change could be pushed into; re-reading where the loop
// already restarts means an edit lands within a keystroke or two, which is as
// close to immediate as matters for a sysop watching someone's session.
//
// A false return means the account is gone or has been deleted and the call
// has to end. Everything else — a demotion, a validation, a new message header
// style — takes effect silently on the next screen.
func (st *runLoopState) refreshCurrentUser() bool {
	if st.currentUser == nil || st.userManager == nil {
		return true // login phase: there is no session record to refresh yet
	}

	before := st.currentUser
	refreshed, stillValid := st.userManager.RefreshSessionUser(before)
	if !stillValid {
		slog.Info("account removed or deleted while online; ending the call",
			"handle", before.Handle, "node", st.nodeNumber)
		if msg := st.e.Strings().ExecAccessDenied; msg != "" {
			_ = terminalio.WriteProcessedBytes(st.terminal,
				ansi.ReplacePipeCodes([]byte(msg)), st.outputMode)
		}
		return false
	}

	if refreshed.AccessLevel != before.AccessLevel {
		slog.Info("access level changed underneath a live session",
			"handle", before.Handle, "node", st.nodeNumber,
			"from", before.AccessLevel, "to", refreshed.AccessLevel)
		// The idle timeout is derived from access level -- sysops are exempt --
		// so a demotion has to re-arm it, or someone demoted out of sysop keeps
		// an unlimited session for the rest of the call.
		applySessionIdleTimeout(st.s, st.e.idleTimeout(refreshed))
	}

	st.currentUser = refreshed
	return true
}
