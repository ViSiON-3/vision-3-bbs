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

// checkMenuPassword implements the 3-attempt menu password gate. When
// menuRec.Password is empty it is a no-op: ok is true and act is
// loopFallthrough so Run proceeds straight to the ACS check exactly as the
// original inline `if menuPassword != "" { ... }` skipped the whole block.
// Otherwise it prompts up to three times; on a correct match ok is true and
// act is loopFallthrough. On disconnect, interrupted entry, or exhausted
// attempts, act is loopReturn with retErr nil — Run's call site returns
// "LOGOFF" for these, mirroring the original inline `return "LOGOFF", nil,
// nil` statements. On a genuine read error, act is loopReturn with retErr
// set — Run's call site returns "" with that error, mirroring the original
// `return "", nil, fmt.Errorf(...)`.
func (st *runLoopState) checkMenuPassword(menuRec *MenuRecord) (ok bool, act loopAction, retErr error) {
	e := st.e
	s := st.s
	terminal := st.terminal
	outputMode := st.outputMode
	nodeNumber := st.nodeNumber

	// Check Menu Password if required
	menuPassword := menuRec.Password
	if menuPassword != "" {
		slog.Debug("menu requires password", "menu", st.currentMenuName)
		passwordOk := false
		for i := 0; i < 3; i++ { // Allow 3 attempts
			prompt := fmt.Sprintf(e.LoadedStrings.ExecMenuPasswordPrompt, st.currentMenuName, i+1)
			processedPrompt := ansi.ReplacePipeCodes([]byte(prompt))
			wErr := terminalio.WriteProcessedBytes(terminal, processedPrompt, outputMode)
			if wErr != nil {
				slog.Error("failed writing menu password prompt", "node", nodeNumber, "error", wErr)
			}

			// Use our helper for secure input reading (using ssh.Session 's')
			inputPassword, err := readPasswordSecurely(s, terminal, outputMode)
			if err != nil {
				if errors.Is(err, io.EOF) {
					slog.Info("user disconnected during menu password entry", "menu", st.currentMenuName)
					return false, loopReturn, nil // Signal logoff
				}
				if errors.Is(err, errInputAborted) { // Check for specific error
					slog.Info("user interrupted password entry for menu", "menu", st.currentMenuName)
					return false, loopReturn, nil // Signal logoff
				}
				slog.Error("failed to read password input securely", "error", err)
				return false, loopReturn, fmt.Errorf("failed reading password: %w", err)
			}
			if inputPassword == menuPassword {
				passwordOk = true
				// Use new helper for feedback message
				wErr := terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.LoadedStrings.ExecPasswordAccepted)), outputMode)
				if wErr != nil {
					slog.Error("failed writing password accepted message", "error", wErr)
				}
				break
			} else {
				// Use new helper for feedback message
				wErr := terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.LoadedStrings.ExecIncorrectPassword)), outputMode)
				if wErr != nil {
					slog.Error("failed writing incorrect password message", "error", wErr)
				}
			}
		}
		if !passwordOk {
			slog.Warn("user failed password entry for menu", "menu", st.currentMenuName, "user", st.currentUser)
			// Use new helper for feedback message
			wErr := terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.LoadedStrings.ExecTooManyAttempts)), outputMode)
			if wErr != nil {
				slog.Error("failed writing too many attempts message", "error", wErr)
			}
			time.Sleep(1 * time.Second)
			return false, loopReturn, nil // Signal logoff after too many failures
		}
	}
	return true, loopFallthrough, nil
}

// runAutoRunCommands executes the current menu's autorun commands (Keys ==
// "//" or "~~"), honoring "//" run-once semantics via autoRunLog and each
// command's ACS gate. Menu-transition mutations (st.previousMenuName,
// st.currentMenuName, st.currentUser) happen in place here exactly as they
// did inline in Run.
//
// On a command execution error, st.currentUser is set to the userResult
// executeCommandAction returned (which may be nil) before returning
// loopReturn with that error, so that Run's call site — which returns
// st.currentUser as Run's second value — produces exactly what the original
// inline `return "", userResult, err` produced. The LOGOFF path mirrors this
// the same way for `return "LOGOFF", userResult, nil`.
//
// A GOTO command sets autoRunActionTaken and breaks the inner command loop,
// exactly as the original inline `break` did; once the loop over commands
// ends, act is loopContinue if any GOTO fired (mirroring the original
// trailing `if autoRunActionTaken { continue }`), otherwise loopFallthrough
// so Run proceeds to display the menu screen.
func (st *runLoopState) runAutoRunCommands(commands []CommandRecord) (act loopAction, retAction string, retErr error) {
	e := st.e
	s := st.s
	terminal := st.terminal
	userManager := st.userManager
	nodeNumber := st.nodeNumber
	sessionStartTime := st.sessionStartTime
	autoRunLog := st.autoRunLog
	outputMode := st.outputMode
	termWidth := st.termWidth
	termHeight := st.termHeight

	// --- AutoRun Command Execution ---
	autoRunActionTaken := false
	for _, cmd := range commands {
		if cmd.Keys == "//" || cmd.Keys == "~~" {
			autoRunKey := fmt.Sprintf("%s:%s", st.currentMenuName, cmd.Command) // Unique key per menu/command

			if cmd.Keys == "//" && autoRunLog[autoRunKey] {
				slog.Debug("skipping already executed run-once command", "command", autoRunKey)
				continue // Skip if already run
			}
			if checkACS(cmd.ACS, st.currentUser, s, terminal, sessionStartTime) { // Use ssh.Session 's'
				slog.Info("executing autorun command", "keys", cmd.Keys, "command", cmd.Command, "acs", cmd.ACS)

				if cmd.Keys == "//" {
					autoRunLog[autoRunKey] = true
				}
				nextAction, nextMenu, userResult, err := e.executeCommandAction(cmd.Command, s, terminal, userManager, st.currentUser, nodeNumber, sessionStartTime, outputMode, termWidth, termHeight)
				if err != nil {
					st.currentUser = userResult
					return loopReturn, "", err
				}
				if nextAction == "GOTO" {
					st.previousMenuName = st.currentMenuName
					st.currentMenuName = nextMenu
					autoRunActionTaken = true
					break
				} else if nextAction == "LOGOFF" {
					st.currentUser = userResult
					return loopReturn, "LOGOFF", nil
				} else if nextAction == "CONTINUE" {
					if userResult != nil {
						st.currentUser = userResult
					}
				}
			} else {
				slog.Debug("autorun command denied by ACS", "keys", cmd.Keys, "command", cmd.Command, "acs", cmd.ACS)
			}
		}
	}
	if autoRunActionTaken {
		return loopContinue, "", nil
	}
	// --- End AutoRun Command Execution ---

	return loopFallthrough, "", nil
}

// dispatchMatchedAction executes the command matched against user input by
// matchCommand and dispatches the resulting action (GOTO/LOGOFF/CONTINUE, or
// an unrecognized fallthrough action type), exactly as Run's original inline
// "matched" branch did. Every path in the original block ended in either
// `continue` or `return`, so the returned act is always loopContinue or
// loopReturn, never loopFallthrough.
//
// On error and on LOGOFF, st.currentUser is set to executeCommandAction's
// userResult before returning, so that Run's call site (which returns
// st.currentUser as Run's second value) reproduces exactly what the original
// inline `return "", userResult, err` / `return "LOGOFF", userResult, nil`
// produced.
func (st *runLoopState) dispatchMatchedAction(nextAction, nodeActivity, menuDefaultActivity string) (act loopAction, retAction string, retErr error) {
	e := st.e
	s := st.s
	terminal := st.terminal
	userManager := st.userManager
	nodeNumber := st.nodeNumber
	sessionStartTime := st.sessionStartTime
	outputMode := st.outputMode
	termWidth := st.termWidth
	termHeight := st.termHeight

	// Update session activity before executing command
	if nodeActivity != "" {
		if sess := e.SessionRegistry.Get(nodeNumber); sess != nil {
			sess.Mutex.Lock()
			sess.Activity = nodeActivity
			sess.Mutex.Unlock()
		}
	}

	// Execute the determined action here
	nextActionType, nextMenuName, userResult, err := e.executeCommandAction(nextAction, s, terminal, userManager, st.currentUser, nodeNumber, sessionStartTime, outputMode, termWidth, termHeight)
	if err != nil {
		st.currentUser = userResult
		return loopReturn, "", err
	}
	if nextActionType == "GOTO" {
		st.previousMenuName = st.currentMenuName // Store current before going to next
		st.currentMenuName = nextMenuName
		return loopContinue, "", nil // Continue main loop to the new menu
	} else if nextActionType == "LOGOFF" {
		st.currentUser = userResult
		return loopReturn, "LOGOFF", nil // Return specific logoff action
	} else if nextActionType == "CONTINUE" {
		// Reset activity to menu default after command completes
		if sess := e.SessionRegistry.Get(nodeNumber); sess != nil {
			sess.Mutex.Lock()
			sess.Activity = menuDefaultActivity
			sess.Mutex.Unlock()
		}
		if userResult != nil {
			st.currentUser = userResult
		}
		return loopContinue, "", nil // Re-display current menu prompt
	}
	slog.Warn("unhandled action type after executing command", "actionType", nextActionType, "command", nextAction)
	return loopContinue, "", nil
}

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
