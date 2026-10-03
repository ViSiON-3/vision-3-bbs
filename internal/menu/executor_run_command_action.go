package menu

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"github.com/gliderlabs/ssh"
	"golang.org/x/term"
)

// executeCommandAction handles the logic for executing a command string (GOTO, RUN, DOOR, LOGOFF).
// Returns: actionType (GOTO, LOGOFF, CONTINUE), nextMenu, resultingUser.
//
// A RUN: or DOOR: handler's error never escapes: a session-fatal one becomes
// LOGOFF, and any other is reported to the caller and the menu carries on
// (see commandFailure). resultingUser may be nil when a handler returned no
// user; the Run loop reads that as "unchanged".
func (e *MenuExecutor) executeCommandAction(action string, s ssh.Session, terminal *term.Terminal, userManager *user.UserMgr, currentUser *user.User, nodeNumber int, sessionStartTime time.Time, outputMode ansi.OutputMode, termWidth int, termHeight int) (actionType string, nextMenu string, userResult *user.User) {
	if action == "DOORMENU" || strings.HasPrefix(action, "DOORMENU:") {
		action = "RUN:DOORMENU " + strings.TrimPrefix(strings.TrimPrefix(action, "DOORMENU"), ":")
	}
	if strings.HasPrefix(action, "GOTO:") {
		nextMenu = strings.ToUpper(strings.TrimPrefix(action, "GOTO:"))
		return "GOTO", nextMenu, currentUser
	} else if action == "LOGOFF" {
		return "LOGOFF", "", currentUser
	} else if strings.HasPrefix(action, "RUN:") {
		parts := strings.SplitN(strings.TrimPrefix(action, "RUN:"), " ", 2)
		runTarget := strings.ToUpper(parts[0])
		if strings.HasPrefix(runTarget, "DOORMENU:") {
			parts = []string{"DOORMENU", strings.TrimPrefix(runTarget, "DOORMENU:")}
			runTarget = "DOORMENU"
		}
		var runArgs string
		if len(parts) > 1 {
			runArgs = parts[1]
		}
		slog.Info("executing RUN action", "target", runTarget, "args", runArgs)

		if runnableFunc, exists := e.RunRegistry[runTarget]; exists {
			slog.Debug("calling registered function for RUN", "node", nodeNumber, "target", runTarget)
			// RunnableFunc now returns user, nextActionString, error
			authUser, nextActionStr, runErr := runnableFunc(&cmdCtx{e: e, s: s, terminal: terminal, userManager: userManager, currentUser: currentUser, nodeNumber: nodeNumber, sessionStartTime: sessionStartTime, outputMode: outputMode, termWidth: termWidth, termHeight: termHeight}, runArgs)
			if runErr != nil {
				actionType, userResult = e.commandFailure("RUN", runTarget, fmt.Sprintf(e.Strings().ExecRunCommandError, runTarget, runErr), runErr, nextActionStr, authUser, s, terminal, outputMode, nodeNumber, termWidth, termHeight)
				return actionType, "", userResult
			}
			slog.Debug("RUN function completed", "target", runTarget)

			// Check if the runnable function returned a specific next action
			if strings.HasPrefix(nextActionStr, "GOTO:") {
				nextMenu = strings.ToUpper(strings.TrimPrefix(nextActionStr, "GOTO:"))
				slog.Debug("RUN requested GOTO", "target", runTarget, "menu", nextMenu)
				return "GOTO", nextMenu, authUser
			} else if nextActionStr == "LOGOFF" {
				slog.Debug("RUN requested LOGOFF", "target", runTarget)
				return "LOGOFF", "", authUser
			}

			// Default action for RUN is CONTINUE
			return "CONTINUE", "", authUser
		} else {
			slog.Warn("no internal function registered for RUN", "target", runTarget)
			msg := fmt.Sprintf(e.Strings().ExecRunCommandNotFound, runTarget)
			wErr := terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
			if wErr != nil {
				slog.Error("failed writing missing RUN command message", "error", wErr)
			}
			uiPause(1 * time.Second)
			return "CONTINUE", "", currentUser
		}
	} else if strings.HasPrefix(action, "DOOR:") {
		doorTarget := strings.TrimPrefix(action, "DOOR:")
		slog.Info("executing DOOR action", "door", doorTarget)
		if doorFunc, exists := e.RunRegistry["DOOR:"]; exists {
			// DOOR runnable returns user, "", error
			userResultDoor, nextActionStrDoor, doorErr := doorFunc(&cmdCtx{e: e, s: s, terminal: terminal, userManager: userManager, currentUser: currentUser, nodeNumber: nodeNumber, sessionStartTime: sessionStartTime, outputMode: outputMode, termWidth: termWidth, termHeight: termHeight}, doorTarget)
			if doorErr != nil {
				actionType, userResult = e.commandFailure("DOOR", doorTarget, fmt.Sprintf(e.Strings().ExecRunDoorError, doorTarget, doorErr), doorErr, nextActionStrDoor, userResultDoor, s, terminal, outputMode, nodeNumber, termWidth, termHeight)
				return actionType, "", userResult
			}
			// Handle potential LOGOFF request from DOOR runnable (though currently returns "")
			if nextActionStrDoor == "LOGOFF" {
				slog.Debug("DOOR requested LOGOFF", "door", doorTarget)
				return "LOGOFF", "", userResultDoor
			}
			slog.Debug("DOOR completed", "door", doorTarget)
			return "CONTINUE", "", userResultDoor // Default CONTINUE after door
		} else {
			slog.Error("DOOR function not registered")
			return "CONTINUE", "", currentUser
		}
	} else {
		slog.Warn("unhandled command action type in executeCommandAction", "action", action)
		return "CONTINUE", "", currentUser
	}
}

// isSessionFatal reports whether err, returned by a RUN: or DOOR: handler,
// means the session itself is over rather than just the command: the caller
// hung up (io.EOF, however deeply wrapped) or sat idle past the session idle
// timeout (editor.ErrIdleTimeout). Every other error is the command's own.
// Leaving an error out of this list is the safe mistake: the menu loop
// carries on, and its next read sees the disconnect anyway.
func isSessionFatal(err error) bool {
	return errors.Is(err, io.EOF) || errors.Is(err, editor.ErrIdleTimeout)
}

// commandFailure handles a non-nil error from a RUN: or DOOR: handler and
// returns the action the menu loop should take and the user to carry on
// with. kind ("RUN" or "DOOR") and target name the command in the log, and
// errMsg is the message shown to the caller for it, already formatted at
// the call site so the stringformat call-site check can see the arguments.
//
// A session-fatal error (see isSessionFatal) is LOGOFF, after the idle
// timeout or time limit notice when that was the cause. Any other error is logged and
// shown, and the loop then goes where the handler said: LOGOFF if it asked
// for one, otherwise CONTINUE, which redisplays the menu. One broken door or
// runnable therefore no longer drops the caller out of the menus. u is the
// handler's user, which may be nil for "unchanged".
func (e *MenuExecutor) commandFailure(kind, target, errMsg string, err error, next string, u *user.User, s ssh.Session, terminal *term.Terminal, outputMode ansi.OutputMode, nodeNumber, termWidth, termHeight int) (string, *user.User) {
	if isSessionFatal(err) {
		if errors.Is(err, editor.ErrIdleTimeout) {
			e.handleSessionTimeout(s, terminal, outputMode, nodeNumber, termWidth, termHeight)
		} else {
			slog.Info("user disconnected during command", "node", nodeNumber, "kind", kind, "target", target)
		}
		return "LOGOFF", u
	}

	slog.Error("command failed", "node", nodeNumber, "kind", kind, "target", target, "error", err)
	if wErr := terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(errMsg)), outputMode); wErr != nil {
		slog.Error("failed writing command error message", "kind", kind, "error", wErr)
	}
	uiPause(1 * time.Second)
	if next == "LOGOFF" {
		return "LOGOFF", u
	}
	return "CONTINUE", u
}
