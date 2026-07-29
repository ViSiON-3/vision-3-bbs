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
// Returns: actionType (GOTO, LOGOFF, CONTINUE), nextMenu, resultingUser, error
func (e *MenuExecutor) executeCommandAction(action string, s ssh.Session, terminal *term.Terminal, userManager *user.UserMgr, currentUser *user.User, nodeNumber int, sessionStartTime time.Time, outputMode ansi.OutputMode, termWidth int, termHeight int) (actionType string, nextMenu string, userResult *user.User, err error) {
	if strings.HasPrefix(action, "GOTO:") {
		nextMenu = strings.ToUpper(strings.TrimPrefix(action, "GOTO:"))
		return "GOTO", nextMenu, currentUser, nil
	} else if action == "LOGOFF" {
		return "LOGOFF", "", currentUser, nil
	} else if strings.HasPrefix(action, "RUN:") {
		parts := strings.SplitN(strings.TrimPrefix(action, "RUN:"), " ", 2)
		runTarget := strings.ToUpper(parts[0])
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
				if errors.Is(runErr, io.EOF) {
					slog.Info("user disconnected during RUN execution", "node", nodeNumber, "target", runTarget)
					return "LOGOFF", "", nil, nil
				}
				if errors.Is(runErr, editor.ErrIdleTimeout) {
					e.handleIdleTimeout(terminal, outputMode, nodeNumber, termHeight)
					return "LOGOFF", "", nil, nil
				}
				slog.Error("RUN function failed", "target", runTarget, "error", runErr)
				errMsg := fmt.Sprintf(e.LoadedStrings.ExecRunCommandError, runTarget, runErr)
				wErr := terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(errMsg)), outputMode)
				if wErr != nil {
					slog.Error("failed writing RUN command error message", "error", wErr)
				}
				time.Sleep(1 * time.Second)
				// Assign the potentially updated user before returning
				userResult = authUser                     // Capture potential user changes (like from AUTHENTICATE)
				return "CONTINUE", "", userResult, runErr // Continue but report error?
			}
			slog.Debug("RUN function completed", "target", runTarget)

			// Check if the runnable function returned a specific next action
			if strings.HasPrefix(nextActionStr, "GOTO:") {
				nextMenu = strings.ToUpper(strings.TrimPrefix(nextActionStr, "GOTO:"))
				slog.Debug("RUN requested GOTO", "target", runTarget, "menu", nextMenu)
				return "GOTO", nextMenu, authUser, nil
			} else if nextActionStr == "LOGOFF" {
				slog.Debug("RUN requested LOGOFF", "target", runTarget)
				return "LOGOFF", "", authUser, nil
			}

			// Default action for RUN is CONTINUE
			return "CONTINUE", "", authUser, nil
		} else {
			slog.Warn("no internal function registered for RUN", "target", runTarget)
			msg := fmt.Sprintf(e.LoadedStrings.ExecRunCommandNotFound, runTarget)
			wErr := terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
			if wErr != nil {
				slog.Error("failed writing missing RUN command message", "error", wErr)
			}
			time.Sleep(1 * time.Second)
			return "CONTINUE", "", currentUser, nil
		}
	} else if strings.HasPrefix(action, "DOOR:") {
		doorTarget := strings.TrimPrefix(action, "DOOR:")
		slog.Info("executing DOOR action", "door", doorTarget)
		if doorFunc, exists := e.RunRegistry["DOOR:"]; exists {
			// DOOR runnable returns user, "", error
			userResultDoor, nextActionStrDoor, doorErr := doorFunc(&cmdCtx{e: e, s: s, terminal: terminal, userManager: userManager, currentUser: currentUser, nodeNumber: nodeNumber, sessionStartTime: sessionStartTime, outputMode: outputMode, termWidth: termWidth, termHeight: termHeight}, doorTarget)
			if doorErr != nil {
				if errors.Is(doorErr, io.EOF) {
					slog.Info("user disconnected during DOOR execution", "node", nodeNumber, "door", doorTarget)
					return "LOGOFF", "", nil, nil
				}
				slog.Error("DOOR execution failed", "door", doorTarget, "error", doorErr)
				errMsg := fmt.Sprintf(e.LoadedStrings.ExecRunDoorError, doorTarget, doorErr)
				wErr := terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(errMsg)), outputMode)
				if wErr != nil {
					slog.Error("failed writing DOOR command error message", "error", wErr)
				}
				time.Sleep(1 * time.Second)
				// Assign potential user result before returning
				userResult = userResultDoor
				return "CONTINUE", "", userResult, doorErr // Continue after door error?
			}
			// Handle potential LOGOFF request from DOOR runnable (though currently returns "")
			if nextActionStrDoor == "LOGOFF" {
				slog.Debug("DOOR requested LOGOFF", "door", doorTarget)
				return "LOGOFF", "", userResultDoor, nil
			}
			slog.Debug("DOOR completed", "door", doorTarget)
			return "CONTINUE", "", userResultDoor, nil // Default CONTINUE after door
		} else {
			slog.Error("DOOR function not registered")
			return "CONTINUE", "", currentUser, nil
		}
	} else {
		slog.Warn("unhandled command action type in executeCommandAction", "action", action)
		return "CONTINUE", "", currentUser, nil
	}
}
