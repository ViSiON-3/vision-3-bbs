package menu

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/types"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"github.com/gliderlabs/ssh"
	"golang.org/x/term"
)

// loopAction tells the Run loop what to do after a phase completes.
type loopAction int

const (
	loopFallthrough loopAction = iota // proceed to next phase
	loopContinue                      // restart the menu loop (re-enter current/next menu)
	loopReturn                        // leave Run (logoff/goodbye); use with retAction/retErr
)

// runLoopState carries the mutable state of one MenuExecutor.Run invocation.
type runLoopState struct {
	e                *MenuExecutor
	s                ssh.Session
	terminal         *term.Terminal
	userManager      *user.UserMgr
	nodeNumber       int
	sessionStartTime time.Time
	autoRunLog       types.AutoRunTracker
	outputMode       ansi.OutputMode
	termWidth        int
	termHeight       int

	currentMenuName  string
	previousMenuName string
	currentUser      *user.User
	currentAreaName  string
	userInput        string
	isLightbarMenu   bool
}

// Run executes the menu logic for a given starting menu name.
// Reverted s parameter back to ssh.Session
// Added outputMode parameter
// Added currentAreaName parameter
func (e *MenuExecutor) Run(s ssh.Session, terminal *term.Terminal, userManager *user.UserMgr, currentUser *user.User, startMenu string, nodeNumber int, sessionStartTime time.Time, autoRunLog types.AutoRunTracker, outputMode ansi.OutputMode, currentAreaName string, termWidth int, termHeight int) (string, *user.User, error) {
	st := &runLoopState{
		e:                e,
		s:                s,
		terminal:         terminal,
		userManager:      userManager,
		nodeNumber:       nodeNumber,
		sessionStartTime: sessionStartTime,
		autoRunLog:       autoRunLog,
		outputMode:       outputMode,
		termWidth:        termWidth,
		termHeight:       termHeight,
		currentMenuName:  strings.ToUpper(startMenu),
		currentUser:      currentUser,
		currentAreaName:  currentAreaName,
	}
	// previousMenuName starts at its zero value (""); tracked via st.previousMenuName below.
	// var authenticatedUserResult *user.User // Unused

	// Clean up the session-scoped InputHandler when this Run() returns so the
	// goroutine is not reused across re-entrant calls or after the session ends.
	// resetSessionIH calls CloseAndWait() before deleting, which stops the telnet
	// read goroutine via the read-interrupt mechanism before a new one is created.
	// Without this, two goroutines compete on the same bufio.Reader, freezing input.
	defer resetSessionIH(s)
	defer clearSessionIdleTimeout(s)

	if st.currentUser != nil {
		slog.Debug("running menu for user", "handle", st.currentUser.Handle, "level", st.currentUser.AccessLevel)
	} else {
		slog.Debug("running menu for potentially unauthenticated user (login phase)")
	}

	// Apply the session-level idle timeout to the shared InputHandler.
	// Sysops/co-sysops are exempt (idleTimeout returns 0 for them).
	// This covers every ReadKey call in the entire session — menus, prompts,
	// message reader, etc. — without requiring per-call changes, and it
	// survives InputHandler recreation after doors (applySessionIdleTimeout
	// stores the value for getSessionIH to re-apply).
	applySessionIdleTimeout(s, e.idleTimeout(st.currentUser))

	for {
		slog.Info("running menu", "menu", st.currentMenuName, "previous", st.previousMenuName, "node", nodeNumber)

		st.userInput = "" // Reset per iteration (Keep this one)
		// Removed authenticatedUserResult declaration from here
		// Numeric commands must be explicitly defined in KEYS tokens (no positional matching)

		// Load and process the ANSI file for the current menu (conditional
		// regions, pipe/token substitution, CP437/encoding conversion).
		ansiProcessResult, renderErr := st.renderMenuAnsi()
		if renderErr != nil {
			return "", nil, renderErr
		}

		// --- SPECIAL HANDLING FOR LOGIN MENU INTERACTION ---
		if st.currentMenuName == "LOGIN" {
			act, retAction, retErr := st.handleLoginMenu(ansiProcessResult)
			switch act {
			case loopContinue:
				continue
			case loopReturn:
				return retAction, st.currentUser, retErr
			}
		} // --- END SPECIAL LOGIN INTERACTION BLOCK ---

		// --- REGULAR MENU PROCESSING (Common for ALL menus, including LOGIN after interaction) ---
		// 1. Load Menu Definition (.MNU)
		menuMnuPath := filepath.Join(e.MenuSetPath, "mnu") // Use correct path structure for MNU
		menuRec, err := LoadMenu(st.currentMenuName, menuMnuPath)
		if err != nil {
			errMsg := fmt.Sprintf(e.LoadedStrings.ExecMenuLoadError, st.currentMenuName, err)
			processedErrMsg := ansi.ReplacePipeCodes([]byte(errMsg))
			// Use new helper for error message
			wErr := terminalio.WriteProcessedBytes(terminal, processedErrMsg, outputMode)
			if wErr != nil {
				slog.Error("failed writing menu load error message", "error", wErr)
			}
			slog.Error(errMsg)
			return "", nil, fmt.Errorf("failed to load menu %s: %w", st.currentMenuName, err)
		}

		// 2. Load Commands (.CFG) for the *current* menu (which might be LOGIN)
		menuCfgPath := filepath.Join(e.MenuSetPath, "cfg") // Use correct path structure for CFG
		commands, err := LoadCommands(st.currentMenuName, menuCfgPath)
		if err != nil {
			slog.Warn("failed to load commands for menu", "menu", st.currentMenuName, "error", err)
			commands = []CommandRecord{} // Use empty slice
		}

		// Determine default node activity for this menu from autorun entries
		menuDefaultActivity := st.currentMenuName
		for _, cmd := range commands {
			if (cmd.Keys == "//" || cmd.Keys == "~~") && cmd.NodeActivity != "" {
				menuDefaultActivity = cmd.NodeActivity
				break
			}
		}
		// Set default activity on session for Who's Online display
		if sess := e.SessionRegistry.Get(nodeNumber); sess != nil {
			sess.Mutex.Lock()
			sess.Activity = menuDefaultActivity
			sess.Mutex.Unlock()
		}

		// Check Menu Password if required
		if _, act, retErr := st.checkMenuPassword(menuRec); act == loopReturn {
			if retErr != nil {
				return "", nil, retErr
			}
			return "LOGOFF", nil, nil
		}

		// Check Menu ACS before proceeding
		menuACS := menuRec.ACS
		if !checkACS(menuACS, st.currentUser, s, terminal, sessionStartTime) { // Use ssh.Session 's'
			slog.Info("user denied access to menu", "menu", st.currentMenuName, "acs", menuACS, "user", st.currentUser)
			errMsg := e.LoadedStrings.ExecAccessDenied
			processedErrMsg := ansi.ReplacePipeCodes([]byte(errMsg))
			// Use new helper for error message
			wErr := terminalio.WriteProcessedBytes(terminal, processedErrMsg, outputMode)
			if wErr != nil {
				slog.Error("failed writing ACS denied message", "error", wErr)
			}
			time.Sleep(1 * time.Second) // Brief pause
			return "LOGOFF", nil, nil   // Signal logoff
		}

		// --- AutoRun Command Execution ---
		autoRunAct, autoRunRetAction, autoRunRetErr := st.runAutoRunCommands(commands)
		switch autoRunAct {
		case loopReturn:
			return autoRunRetAction, st.currentUser, autoRunRetErr
		case loopContinue:
			continue
		}
		// --- End AutoRun Command Execution ---

		// 3. Display ANSI Screen (Processed Bytes) - Moved display logic here for ALL menus
		// (Avoid double-display for LOGIN which handles its own display before prompt)
		// We still need the raw content for potential lightbar background
		// Note: ansBackgroundBytes is currently unused but will be needed for full lightbar implementation
		// ansBackgroundBytes := ansiProcessResult.DisplayBytes
		if err := st.displayMenuScreen(ansiProcessResult, menuRec); err != nil {
			return "", nil, err
		}

		// --- Check for Lightbar Menu (.BAR) ---
		// Check if a .BAR file exists for this menu in the MENU SET directory
		st.isLightbarMenu = HasBarFile(st.currentMenuName, e.MenuSetPath)

		// Variable declarations for command handling
		// var st.userInput string // REMOVE this redeclaration
		// var numericMatchAction string // Moved declaration up

		// 4. Determine Input Mode / Method
		if st.isLightbarMenu {
			slog.Debug("entering lightbar input mode", "menu", st.currentMenuName)

			// Load lightbar options from the config directory
			lightbarOptions, loadErr := loadLightbarOptions(st.currentMenuName, e)
			if loadErr != nil {
				slog.Error("failed to load lightbar options", "menu", st.currentMenuName, "error", loadErr)
				st.isLightbarMenu = false
			} else if len(lightbarOptions) == 0 {
				slog.Warn("no valid lightbar options loaded", "menu", st.currentMenuName)
				st.isLightbarMenu = false
			}

			if st.isLightbarMenu {
				cursorHidden := e.hideCursorIfNeeded(terminal, outputMode, cursorHideContextDefault)
				ansBackgroundBytes := ansiProcessResult.DisplayBytes

				// Initially draw with first option selected
				selectedIndex := 0
				drawErr := drawLightbarMenu(terminal, ansBackgroundBytes, lightbarOptions, selectedIndex, outputMode, false)
				if drawErr != nil {
					slog.Error("failed to draw lightbar menu", "menu", st.currentMenuName, "error", drawErr)
					e.showCursorIfHidden(terminal, outputMode, cursorHidden)
					st.isLightbarMenu = false
				} else {
					lightbarInput, act, retErr := st.runLightbarInput(lightbarOptions, cursorHidden)
					if act == loopReturn {
						return lightbarInput, nil, retErr
					}
					// Set st.userInput to lightbar result if a selection was made
					if lightbarInput != "" {
						st.userInput = lightbarInput
					}
				}
			}

			if !st.isLightbarMenu || st.userInput == "" {
				// Fallback to standard input if lightbar loading failed or no valid selection made
				e.deliverPendingPages(terminal, nodeNumber, outputMode)
				// Display Prompt (Skip if USEPROMPT is false)
				if menuRec.GetUsePrompt() { // Condition changed: Only check UsePrompt
					err = e.displayPrompt(terminal, menuRec, st.currentUser, userManager, nodeNumber, st.currentMenuName, sessionStartTime, outputMode, st.currentAreaName) // Pass st.currentAreaName
					if err != nil {
						return "", nil, err // Propagate the error
					}
				} else {
					// Log message remains the same, but the condition causing it is now just UsePrompt==false
					slog.Debug("skipping prompt display", "menu", st.currentMenuName, "usePrompt", menuRec.GetUsePrompt(), "prompt1Empty", menuRec.Prompt1 == "")
				}

				// Read User Input Line via shared InputHandler to avoid reader races.
				input, err := readLineFromSessionIH(s, terminal)
				if err != nil {
					if err == io.EOF {
						slog.Info("user disconnected during menu input", "menu", st.currentMenuName)
						return "LOGOFF", nil, nil // Signal logoff
					}
					slog.Error("failed to read input for menu", "menu", st.currentMenuName, "error", err)
					return "", nil, fmt.Errorf("failed reading input: %w", err)
				}
				st.userInput = strings.ToUpper(strings.TrimSpace(input))
				slog.Debug("user input", "input", st.userInput)
			}
		} else {
			input, act, retErr := st.readStandardInput(menuRec)
			switch act {
			case loopReturn:
				return input, nil, retErr
			case loopContinue:
				continue
			}
		} // End if st.isLightbarMenu / else

		// 6. Process Input / Find Command Match (st.userInput determined by menu type)
		hasAccess := func(acs string, keys string) bool {
			ok := checkACS(acs, st.currentUser, s, terminal, sessionStartTime) // Use ssh.Session 's'
			if !ok {
				if st.currentUser != nil {
					slog.Debug("user does not meet ACS for command keys", "handle", st.currentUser.Handle, "acs", acs, "keys", keys)
				} else {
					slog.Debug("unauthenticated user does not meet ACS for command keys", "acs", acs, "keys", keys)
				}
			}
			return ok
		}
		nextAction, matchedNodeActivity, matched := matchCommand(commands, st.userInput, hasAccess)

		// 7. Handle Action or No Match
		if matched {
			dispatchAct, dispatchRetAction, dispatchRetErr := st.dispatchMatchedAction(nextAction, matchedNodeActivity, menuDefaultActivity)
			switch dispatchAct {
			case loopReturn:
				return dispatchRetAction, st.currentUser, dispatchRetErr
			case loopContinue:
				continue
			}
		} else {
			slog.Debug("input did not match any commands in menu", "input", st.userInput, "menu", st.currentMenuName)

			// If it was a lightbar menu and input was ignored (st.userInput == ""), just loop again
			if st.isLightbarMenu {
				continue
			}

			// Empty Enter should just redisplay the current menu, not fall through to fallback
			if st.userInput == "" {
				continue
			}

			fallbackMenu := menuRec.Fallback
			if fallbackMenu != "" {
				slog.Info("no command match, using fallback menu", "menu", fallbackMenu)
				st.previousMenuName = st.currentMenuName // Store current before going to fallback
				st.currentMenuName = strings.ToUpper(fallbackMenu)
				continue
			}
			e.showUndefinedMenuInput(terminal, outputMode, nodeNumber)
			continue // Redisplay current menu
		}
	}
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
