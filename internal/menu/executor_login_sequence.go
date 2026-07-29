package menu

import (
	"errors"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"github.com/gliderlabs/ssh"
	"golang.org/x/term"
)

// loginPausePrompt displays the configured pause prompt (centered) and waits for Enter.
func (e *MenuExecutor) loginPausePrompt(s ssh.Session, terminal *term.Terminal, _ int, outputMode ansi.OutputMode, termWidth int, termHeight int) error {
	pausePrompt := e.LoadedStrings.PauseString
	if pausePrompt == "" {
		pausePrompt = "\r\n|07Press |15[ENTER]|07 to continue... "
	}

	return writeCenteredPausePrompt(s, terminal, pausePrompt, outputMode, termWidth, termHeight)
}

// RunLoginSequence is the exported entry point for running the login sequence from main.go.
// Returns the next menu name to enter (e.g., "MAIN") or "LOGOFF".
func (e *MenuExecutor) RunLoginSequence(s ssh.Session, terminal *term.Terminal, userManager *user.UserMgr, currentUser *user.User, nodeNumber int, sessionStartTime time.Time, outputMode ansi.OutputMode, termWidth int, termHeight int) (string, error) {
	_, nextAction, err := runFullLoginSequence(&cmdCtx{e: e, s: s, terminal: terminal, userManager: userManager, currentUser: currentUser, nodeNumber: nodeNumber, sessionStartTime: sessionStartTime, outputMode: outputMode, termWidth: termWidth, termHeight: termHeight}, "")
	if err != nil {
		return "LOGOFF", err
	}
	// Parse "GOTO:MAIN" -> "MAIN", pass through "LOGOFF" as-is
	if strings.HasPrefix(nextAction, "GOTO:") {
		return strings.ToUpper(strings.TrimPrefix(nextAction, "GOTO:")), nil
	}
	if nextAction == "LOGOFF" {
		return "LOGOFF", nil
	}
	return "MAIN", nil
}

// runFullLoginSequence executes the configurable login sequence from login.json.
func runFullLoginSequence(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	s := c.s
	terminal := c.terminal
	userManager := c.userManager
	currentUser := c.currentUser
	nodeNumber := c.nodeNumber
	sessionStartTime := c.sessionStartTime
	outputMode := c.outputMode
	termWidth := c.termWidth
	termHeight := c.termHeight

	loginSequence := e.GetLoginSequence()
	slog.Info("running FULL_LOGIN_SEQUENCE", "node", nodeNumber, "handle", currentUser.Handle, "count", len(loginSequence))

	// Offer newly flagged newscan-default areas before the login items run.
	if updated, njErr := runNewscanAutoJoin(&cmdCtx{e: e, s: s, terminal: terminal, userManager: userManager, currentUser: currentUser, nodeNumber: nodeNumber, sessionStartTime: sessionStartTime, outputMode: outputMode, termWidth: termWidth, termHeight: termHeight}); njErr != nil {
		if errors.Is(njErr, io.EOF) {
			return nil, "LOGOFF", io.EOF
		}
		slog.Error("newscan auto-join failed", "node", nodeNumber, "error", njErr)
	} else if updated != nil {
		currentUser = updated
	}

	// Build dispatch map for login item commands
	type loginHandler func(c *cmdCtx, args string) (*user.User, string, error)

	handlers := map[string]loginHandler{
		"LASTCALLS":        runLastCallers,
		"ONELINERS":        runOneliners,
		"USERSTATS":        runShowStats,
		"NMAILSCAN":        runNewMailScan,
		"DISPLAYFILE":      runLoginDisplayFile,
		"RUNDOOR":          runLoginDoor,
		"FASTLOGIN":        runFastLogin,
		"NEWUSERVAL":       runNewUserValidation,
		"WHOISONLINE":      runLoginWhosOnline,
		"PRINTNEWS":        runPrintNews,
		"VOTEMANDATORY":    runVoteOnMandatory,
		"CHECKNUV":         runCheckNUV,
		"RANDOMRUMOR":      runRandomRumor,
		"INFOFORMREQUIRED": runInfoFormRequired,
	}

	for i, item := range loginSequence {
		// Check security level requirement
		if item.SecLevel > 0 && currentUser.AccessLevel < item.SecLevel {
			slog.Debug("skipping login item - insufficient user level", "node", nodeNumber, "item", i+1, "command", item.Command, "level", currentUser.AccessLevel, "required", item.SecLevel)
			continue
		}

		slog.Debug("executing login item", "node", nodeNumber, "item", i+1, "count", len(loginSequence), "command", item.Command)

		// Clear screen if requested
		if item.ClearScreen {
			terminalio.WriteProcessedBytes(terminal, []byte("\x1b[2J\x1b[H"), outputMode)
		}

		// Check if this is a DOOR: command
		var nextAction string
		var err error
		var updatedUser *user.User
		if strings.HasPrefix(item.Command, "DOOR:") {
			// Extract door name and execute via DOOR: handler
			doorName := strings.TrimPrefix(item.Command, "DOOR:")
			slog.Info("executing door from login sequence", "node", nodeNumber, "door", doorName)

			// Call the DOOR: handler from RunRegistry
			if doorFunc, exists := e.RunRegistry["DOOR:"]; exists {
				updatedUser, nextAction, err = doorFunc(&cmdCtx{e: e, s: s, terminal: terminal, userManager: userManager, currentUser: currentUser, nodeNumber: nodeNumber, sessionStartTime: sessionStartTime, outputMode: outputMode, termWidth: termWidth, termHeight: termHeight}, doorName)
				if updatedUser != nil {
					currentUser = updatedUser
				}
			} else {
				slog.Error("DOOR: handler not registered", "node", nodeNumber)
				continue
			}
		} else {
			// Look up and execute the handler from the local handlers map
			handler, exists := handlers[item.Command]
			if !exists {
				slog.Warn("unknown login sequence command", "node", nodeNumber, "command", item.Command)
				continue
			}

			// Pass item.Data as the args parameter for commands that need it
			itemArgs := args
			if item.Data != "" {
				itemArgs = item.Data
			}

			updatedUser, nextAction, err = handler(&cmdCtx{e: e, s: s, terminal: terminal, userManager: userManager, currentUser: currentUser, nodeNumber: nodeNumber, sessionStartTime: sessionStartTime, outputMode: outputMode, termWidth: termWidth, termHeight: termHeight}, itemArgs)
			if updatedUser != nil {
				currentUser = updatedUser
			}
		}
		if err != nil {
			slog.Error("error during login item", "node", nodeNumber, "command", item.Command, "error", err)
			if errors.Is(err, io.EOF) {
				return nil, "LOGOFF", io.EOF
			}
			// Non-fatal errors - continue with next item
		}

		// Check if the handler requested navigation (GOTO/LOGOFF)
		if nextAction == "LOGOFF" {
			return nil, "LOGOFF", nil
		}
		if strings.HasPrefix(nextAction, "GOTO:") {
			slog.Debug("login sequence interrupted", "node", nodeNumber, "command", item.Command, "action", nextAction)
			return currentUser, nextAction, nil
		}

		// Pause after if requested
		if item.PauseAfter {
			if pauseErr := e.loginPausePrompt(s, terminal, nodeNumber, outputMode, termWidth, termHeight); pauseErr != nil {
				if errors.Is(pauseErr, io.EOF) {
					return nil, "LOGOFF", io.EOF
				}
			}
		}
	}

	// Sequence completed - transition to MAIN menu
	slog.Debug("FULL_LOGIN_SEQUENCE completed, transitioning to MAIN", "node", nodeNumber)
	return currentUser, "GOTO:MAIN", nil
}

// confirmAbortLogin shows "Abort Login? Yes|No" and returns true if confirmed.
func (e *MenuExecutor) confirmAbortLogin(s ssh.Session, terminal *term.Terminal, outputMode ansi.OutputMode, nodeNumber, termWidth, termHeight int) (bool, error) {
	// Keep login abort confirmation below the ANSI menu art instead of inline
	// at the username/password field position.
	if termHeight > 0 {
		terminalio.WriteProcessedBytes(terminal, []byte(ansi.MoveCursor(termHeight, 1)), outputMode)
	} else {
		terminalio.WriteProcessedBytes(terminal, []byte("\r\n"), outputMode)
	}

	prompt := e.LoadedStrings.ExecAbortLoginPrompt
	if prompt == "" {
		prompt = "|07Abort Login? @"
	}
	abort, err := e.PromptYesNo(s, terminal, prompt, outputMode, nodeNumber, termWidth, termHeight, false)
	if err != nil {
		return false, err
	}
	return abort, nil
}

// confirmAbortPost shows "Abort Post? Yes|No" lightbar.
// Returns true (and prints "Post aborted.") if user confirmed, false to retry.
func (e *MenuExecutor) confirmAbortPost(s ssh.Session, terminal *term.Terminal, outputMode ansi.OutputMode, nodeNumber, termWidth, termHeight int) (bool, error) {
	abort, err := e.PromptYesNo(s, terminal, "|07Abort Post? @", outputMode, nodeNumber, termWidth, termHeight, false)
	if err != nil {
		return false, err
	}
	if abort {
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n|07Post aborted.|07\r\n")), outputMode)
		time.Sleep(500 * time.Millisecond)
	}
	return abort, nil
}
