package menu

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
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

// userHandle returns the current user's handle for logging, or "" when no user
// is authenticated yet. Logging the handle rather than the *user.User keeps the
// password hash and personal fields out of the logs.
func (st *runLoopState) userHandle() string {
	if st.currentUser == nil {
		return ""
	}
	return st.currentUser.Handle
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
			slog.Info("user denied access to menu", "menu", st.currentMenuName, "acs", menuACS, "user", st.userHandle())
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
		input, act, retErr := st.readMenuInput(ansiProcessResult, menuRec)
		switch act {
		case loopReturn:
			// Propagate the user like every other loopReturn site: nil here dropped
			// an authenticated user on the disconnect paths, and main.go reads a nil
			// user as "authentication did not happen".
			return input, st.currentUser, retErr
		case loopContinue:
			continue
		}

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

			// Empty Enter should just redisplay the current menu, not fall through
			// to fallback. This also covers ignored lightbar keypresses, which
			// yield empty input. Non-empty unmatched input gets the fallback /
			// undefined-command treatment even on a lightbar menu: runLightbarInput
			// only ever returns a hotkey defined in the .BAR file, so reaching here
			// with input means that hotkey has no command in the menu's .CFG, and
			// silently redisplaying hid the misconfiguration.
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
