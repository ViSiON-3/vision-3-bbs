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
						return "LOGOFF", nil, nil // Signal logoff
					}
					if errors.Is(err, errInputAborted) { // Check for specific error
						slog.Info("user interrupted password entry for menu", "menu", st.currentMenuName)
						return "LOGOFF", nil, nil // Signal logoff
					}
					slog.Error("failed to read password input securely", "error", err)
					return "", nil, fmt.Errorf("failed reading password: %w", err)
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
				return "LOGOFF", nil, nil // Signal logoff after too many failures
			}
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
						return "", userResult, err
					}
					if nextAction == "GOTO" {
						st.previousMenuName = st.currentMenuName
						st.currentMenuName = nextMenu
						autoRunActionTaken = true
						break
					} else if nextAction == "LOGOFF" {
						return "LOGOFF", userResult, nil
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
					// Process keyboard navigation for lightbar.
					// Use the session-scoped InputHandler so the same goroutine that
					// reads from the SSH session is shared with any editor invocations
					// triggered from this menu (e.g. COMPOSEMSG). This prevents the
					// orphaned goroutine from consuming the first keystroke after the
					// editor exits, which caused the "double key press" bug.
					lightbarResult := "" // Use a local variable for the result
					inputLoop := true
					sessionIH := getSessionIH(s)
					for inputLoop {
						key, err := sessionIH.ReadKey()
						if err != nil {
							e.showCursorIfHidden(terminal, outputMode, cursorHidden)
							if errors.Is(err, io.EOF) {
								slog.Info("user disconnected during lightbar input", "menu", st.currentMenuName)
								return "LOGOFF", nil, nil
							}
							if errors.Is(err, editor.ErrIdleTimeout) {
								e.handleIdleTimeout(terminal, outputMode, nodeNumber, termHeight)
								return "LOGOFF", nil, nil
							}
							slog.Error("failed to read lightbar input", "menu", st.currentMenuName, "error", err)
							return "", nil, fmt.Errorf("failed reading lightbar input: %w", err)
						}
						slog.Debug("lightbar input key", "key", key)

						switch key {
						case editor.KeyArrowUp:
							prevIndex := selectedIndex
							selectedIndex--
							if selectedIndex < 0 {
								selectedIndex = len(lightbarOptions) - 1
							}
							if prevIndex != selectedIndex {
								_ = drawLightbarOption(terminal, lightbarOptions[prevIndex], false, outputMode)
								_ = drawLightbarOption(terminal, lightbarOptions[selectedIndex], true, outputMode)
							}
						case editor.KeyArrowDown:
							prevIndex := selectedIndex
							selectedIndex++
							if selectedIndex >= len(lightbarOptions) {
								selectedIndex = 0
							}
							if prevIndex != selectedIndex {
								_ = drawLightbarOption(terminal, lightbarOptions[prevIndex], false, outputMode)
								_ = drawLightbarOption(terminal, lightbarOptions[selectedIndex], true, outputMode)
							}
						case editor.KeyHome:
							if selectedIndex != 0 {
								prevIndex := selectedIndex
								selectedIndex = 0
								_ = drawLightbarOption(terminal, lightbarOptions[prevIndex], false, outputMode)
								_ = drawLightbarOption(terminal, lightbarOptions[selectedIndex], true, outputMode)
							}
						case editor.KeyEnd:
							lastIdx := len(lightbarOptions) - 1
							if selectedIndex != lastIdx {
								prevIndex := selectedIndex
								selectedIndex = lastIdx
								_ = drawLightbarOption(terminal, lightbarOptions[prevIndex], false, outputMode)
								_ = drawLightbarOption(terminal, lightbarOptions[selectedIndex], true, outputMode)
							}
						case int('\r'), int('\n'): // Enter (CR or LF) - select current item
							if selectedIndex >= 0 && selectedIndex < len(lightbarOptions) {
								lightbarResult = lightbarOptions[selectedIndex].HotKey
								inputLoop = false
							}
						case editor.KeyEsc:
							// Bare ESC (InputHandler already consumed any ANSI sequence) — ignore
						default:
							if key >= int('1') && key <= int('9') {
								// Direct selection by number
								numIndex := key - int('1') // Convert 1-9 to 0-8
								if numIndex >= 0 && numIndex < len(lightbarOptions) {
									prevIndex := selectedIndex
									selectedIndex = numIndex
									if prevIndex != selectedIndex {
										_ = drawLightbarOption(terminal, lightbarOptions[prevIndex], false, outputMode)
										_ = drawLightbarOption(terminal, lightbarOptions[selectedIndex], true, outputMode)
									}
									lightbarResult = lightbarOptions[numIndex].HotKey
									inputLoop = false
								}
							} else if key >= 32 && key < 127 {
								// Check if printable key matches any hotkey directly
								keyStr := strings.ToUpper(string(rune(key)))
								for _, opt := range lightbarOptions {
									if keyStr == opt.HotKey {
										lightbarResult = opt.HotKey
										inputLoop = false
										break
									}
								}
							}
							// Control chars and other special codes are ignored
						}
					}
					slog.Debug("processed lightbar input", "result", lightbarResult)
					e.showCursorIfHidden(terminal, outputMode, cursorHidden)
					// Set st.userInput to lightbar result if a selection was made
					if lightbarResult != "" {
						st.userInput = lightbarResult
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
			// --- Standard Menu Input Handling ---
			e.deliverPendingPages(terminal, nodeNumber, outputMode)
			// Display Prompt (Skip if USEPROMPT is false)
			slog.Debug("checking prompt display for menu", "menu", st.currentMenuName, "usePrompt", menuRec.GetUsePrompt())
			if menuRec.GetUsePrompt() { // Condition changed: Only check UsePrompt
				slog.Debug("calling displayPrompt for menu", "menu", st.currentMenuName)
				err = e.displayPrompt(terminal, menuRec, st.currentUser, userManager, nodeNumber, st.currentMenuName, sessionStartTime, outputMode, st.currentAreaName) // Pass st.currentAreaName
				slog.Debug("returned from displayPrompt for menu", "menu", st.currentMenuName, "error", err)
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
				if errors.Is(err, io.EOF) {
					slog.Info("user disconnected during menu input", "menu", st.currentMenuName)
					return "LOGOFF", nil, nil
				}
				if errors.Is(err, editor.ErrIdleTimeout) {
					e.handleIdleTimeout(terminal, outputMode, nodeNumber, termHeight)
					return "LOGOFF", nil, nil
				}
				slog.Error("failed to read input for menu", "menu", st.currentMenuName, "error", err)
				return "", nil, fmt.Errorf("failed reading input: %w", err)
			}
			st.userInput = strings.ToUpper(strings.TrimSpace(input))
			slog.Debug("user input", "input", st.userInput)

			// --- Special Input Handling (^P, ##) ---
			if st.userInput == "\x10" || st.userInput == "^P" { // Ctrl+P is ASCII 16 (\x10)
				if st.previousMenuName != "" {
					slog.Debug("user entered ^P, going back to previous menu", "previous", st.previousMenuName)
					temp := st.currentMenuName
					st.currentMenuName = st.previousMenuName
					st.previousMenuName = temp // Update previous in case they go back again
					continue                   // Go directly to the previous menu loop iteration
				} else {
					slog.Debug("user entered ^P, but no previous menu recorded")
					continue // Re-display current menu prompt
				}
			}

			// --- End Special Input Handling ---
		} // End if st.isLightbarMenu / else

		// 6. Process Input / Find Command Match (st.userInput determined by menu type)
		matched := false
		nextAction := ""          // Store the action determined by the matched command
		matchedNodeActivity := "" // Store matched command's node activity

		// Global hangup shortcut: /G
		if st.userInput == "/G" {
			nextAction = "RUN:IMMEDIATELOGOFF"
			matched = true
		}

		if !matched { // Check keyword matches (relevant for both)
			for _, cmdRec := range commands {
				// Hidden commands are still matched (e.g. % for sponsor menu); HIDDEN only affects display/prompts.

				cmdACS := cmdRec.ACS
				if !checkACS(cmdACS, st.currentUser, s, terminal, sessionStartTime) { // Use ssh.Session 's'
					if st.currentUser != nil {
						slog.Debug("user does not meet ACS for command keys", "handle", st.currentUser.Handle, "acs", cmdACS, "keys", cmdRec.Keys)
					} else {
						slog.Debug("unauthenticated user does not meet ACS for command keys", "acs", cmdACS, "keys", cmdRec.Keys)
					}
					continue // Skip this command if ACS check fails
				}

				keys := strings.Split(cmdRec.Keys, " ") // Use string directly
				for _, key := range keys {
					// ^M matches when user presses Enter with no input (classic BBS default command)
					if key == "^M" && st.userInput == "" {
						nextAction = cmdRec.Command
						matchedNodeActivity = cmdRec.NodeActivity
						slog.Debug("matched ^M (enter/default) to command action", "command", nextAction)
						matched = true
						break
					}
					// Standard exact key match
					if key != "" && st.userInput != "" && st.userInput == key {
						nextAction = cmdRec.Command
						matchedNodeActivity = cmdRec.NodeActivity
						slog.Debug("matched key to command action", "key", key, "command", nextAction)
						matched = true
						break
					}
					// ## matches any numeric input (classic BBS numeric wildcard)
					if key == "##" && st.userInput != "" {
						isNumeric := true
						for _, ch := range st.userInput {
							if ch < '0' || ch > '9' {
								isNumeric = false
								break
							}
						}
						if isNumeric {
							// Append the entered number as args so executeCommandAction
							// forwards it to the RUN: handler via runArgs.
							nextAction = cmdRec.Command + " " + st.userInput
							matchedNodeActivity = cmdRec.NodeActivity
							slog.Debug("matched ## numeric wildcard to command action", "input", st.userInput, "command", nextAction)
							matched = true
							break
						}
					}
				}
				if matched {
					break // Break outer command loop
				}
			}
		}

		// 7. Handle Action or No Match
		if matched {
			// Update session activity before executing command
			if matchedNodeActivity != "" {
				if sess := e.SessionRegistry.Get(nodeNumber); sess != nil {
					sess.Mutex.Lock()
					sess.Activity = matchedNodeActivity
					sess.Mutex.Unlock()
				}
			}

			// Execute the determined action here
			nextActionType, nextMenuName, userResult, err := e.executeCommandAction(nextAction, s, terminal, userManager, st.currentUser, nodeNumber, sessionStartTime, outputMode, termWidth, termHeight)
			if err != nil {
				return "", userResult, err
			}
			if nextActionType == "GOTO" {
				st.previousMenuName = st.currentMenuName // Store current before going to next
				st.currentMenuName = nextMenuName
				continue // Continue main loop to the new menu
			} else if nextActionType == "LOGOFF" {
				return "LOGOFF", userResult, nil // Return specific logoff action
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
				continue // Re-display current menu prompt
			}
			slog.Warn("unhandled action type after executing command", "actionType", nextActionType, "command", nextAction)
			continue
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
