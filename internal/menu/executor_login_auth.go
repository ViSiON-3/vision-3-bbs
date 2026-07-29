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
	"github.com/ViSiON-3/vision-3-bbs/internal/logging"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"github.com/gliderlabs/ssh"
	"golang.org/x/term"
)

// runAuthenticate handles the RUN:AUTHENTICATE command.
func runAuthenticate(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	s := c.s
	terminal := c.terminal
	userManager := c.userManager
	currentUser := c.currentUser
	nodeNumber := c.nodeNumber
	outputMode := c.outputMode
	termWidth := c.termWidth
	termHeight := c.termHeight

	// If already logged in, maybe show an error or just return?
	if currentUser != nil {
		slog.Warn("user tried to run AUTHENTICATE while already logged in", "node", nodeNumber, "handle", currentUser.Handle)
		msg := e.LoadedStrings.ExecAlreadyLoggedIn
		// Use WriteProcessedBytes
		wErr := terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		if wErr != nil {
			slog.Error("failed writing already logged in message", "error", wErr)
		}
		time.Sleep(1 * time.Second) // Pause after failed attempt
		return nil, "", nil         // No user change, no error
	}

	// Define approximate coordinates (MODIFY THESE based on LOGIN.ANS)
	userRow, userCol := 18, 20
	passRow, passCol := 19, 20
	errorRow := passRow + 2 // Row for error messages

	// Move to Username position, display prompt, and read input
	terminalio.WriteProcessedBytes(terminal, []byte(ansi.MoveCursor(userRow, userCol)), outputMode)
	usernamePrompt := e.LoadedStrings.ExecUsernamePrompt
	// Use WriteProcessedBytes for prompt
	wErr := terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(usernamePrompt)), outputMode)
	if wErr != nil {
		slog.Error("failed writing username prompt", "node", nodeNumber, "error", wErr)
		// Continue anyway?
	}
	usernameInput, err := readLineFromSessionIHAllowAbort(s, terminal)
	if err != nil {
		if err == io.EOF {
			slog.Info("user disconnected during username input", "node", nodeNumber)
			// Return an error that signals disconnection to the main loop
			return nil, "LOGOFF", io.EOF // Signal logoff
		}
		if errors.Is(err, errInputAborted) {
			abort, confirmErr := e.confirmAbortLogin(s, terminal, outputMode, nodeNumber, termWidth, termHeight)
			if confirmErr != nil {
				return nil, "", confirmErr
			}
			if abort {
				return nil, "LOGOFF", io.EOF
			}
			return nil, "", nil
		}
		slog.Error("failed to read username input", "node", nodeNumber, "error", err)
		return nil, "", fmt.Errorf("failed reading username: %w", err) // Critical error
	}
	username := strings.TrimSpace(usernameInput)
	if username == "" {
		return nil, "", nil // Empty username, just redisplay login menu
	}

	// Check if user wants to apply as a new user
	if strings.EqualFold(username, "new") {
		slog.Info("user typed 'new' in AUTHENTICATE - starting new user application", "node", nodeNumber)
		newUserErr := e.handleNewUserApplication(s, terminal, userManager, nodeNumber, outputMode, termWidth, termHeight)
		if newUserErr != nil {
			if errors.Is(newUserErr, io.EOF) {
				return nil, "LOGOFF", io.EOF
			}
			slog.Error("new user application error", "node", nodeNumber, "error", newUserErr)
		}
		return nil, "", nil // Return to LOGIN screen after signup
	}

	// Move to Password position, display prompt, and read input securely
	terminalio.WriteProcessedBytes(terminal, []byte(ansi.MoveCursor(passRow, passCol)), outputMode)
	passwordPrompt := e.LoadedStrings.ExecPasswordPrompt
	wErr = terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(passwordPrompt)), outputMode)
	if wErr != nil {
		slog.Error("failed writing password prompt", "node", nodeNumber, "error", wErr)
	}
	password, err := readPasswordSecurely(s, terminal, outputMode)
	if err != nil {
		if errors.Is(err, io.EOF) {
			slog.Info("user disconnected during password input", "node", nodeNumber)
			return nil, "LOGOFF", io.EOF // Signal logoff
		}
		if errors.Is(err, errInputAborted) {
			slog.Info("user pressed ESC during password entry", "node", nodeNumber)
			abort, confirmErr := e.confirmAbortLogin(s, terminal, outputMode, nodeNumber, termWidth, termHeight)
			if confirmErr != nil {
				return nil, "", confirmErr
			}
			if abort {
				return nil, "LOGOFF", io.EOF
			}
			return nil, "", nil
		}
		slog.Error("failed to read password securely", "node", nodeNumber, "error", err)
		return nil, "", fmt.Errorf("failed reading password: %w", err) // Critical error
	}

	// Get remote IP address for lockout checking
	remoteIP := remoteIPFromSession(s)

	// Check if this IP is currently locked out
	if e.IPLockoutCheck != nil {
		isLocked, lockedUntil, attempts := e.IPLockoutCheck.IsIPLockedOut(remoteIP)
		if isLocked {
			logging.Security("login attempt from locked IP",
				"node", nodeNumber, "ip", remoteIP, "locked_until", lockedUntil.Format("2006-01-02 15:04:05"), "attempts", attempts)
			terminalio.WriteProcessedBytes(terminal, []byte(ansi.MoveCursor(errorRow, 1)), outputMode)
			minutesLeft := int(time.Until(lockedUntil).Minutes()) + 1
			errMsg := fmt.Sprintf(e.LoadedStrings.ExecIPLockout, minutesLeft)
			wErr := terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(errMsg)), outputMode)
			if wErr != nil {
				slog.Error("failed writing IP lockout message", "error", wErr)
			}
			time.Sleep(2 * time.Second)
			return nil, "", nil
		}
	}

	// Attempt Authentication via UserManager
	slog.Debug("attempting authentication", "node", nodeNumber, "handle", username, "ip", remoteIP)
	authUser, authenticated := userManager.Authenticate(username, password)
	if !authenticated {
		slog.Warn("failed authentication attempt", "node", nodeNumber, "handle", username, "ip", remoteIP)

		// Record failed login attempt for this IP
		if e.IPLockoutCheck != nil {
			wasLocked := e.IPLockoutCheck.RecordFailedLoginAttempt(remoteIP)
			if wasLocked {
				logging.Security("IP locked out after too many failed attempts", "node", nodeNumber, "ip", remoteIP)
			}
		}

		// Display error message to user
		terminalio.WriteProcessedBytes(terminal, []byte(ansi.MoveCursor(errorRow, 1)), outputMode) // Move cursor for message
		errMsg := e.LoadedStrings.ExecLoginIncorrect
		// Use WriteProcessedBytes
		wErr := terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(errMsg)), outputMode)
		if wErr != nil {
			slog.Error("failed writing login incorrect message", "error", wErr)
		}
		time.Sleep(1 * time.Second) // Pause after failed attempt
		return nil, "", nil         // Failed auth, but not a critical error. Let LOGIN menu handle retries.
	}

	// Check if user meets minimum logon level (if LogonLevel > 0)
	// Note: We rely on accessLevel for access control, not the validated flag.
	// The validated flag is used for auto-upgrading to regularUserLevel and showing validation status.
	// Get thread-safe config snapshot
	cfg := e.GetServerConfig()
	if cfg.LogonLevel > 0 && authUser.AccessLevel < cfg.LogonLevel {
		slog.Info("login denied - insufficient access level", "node", nodeNumber, "handle", username, "has", authUser.AccessLevel, "needs", cfg.LogonLevel)
		terminalio.WriteProcessedBytes(terminal, []byte(ansi.MoveCursor(errorRow, 1)), outputMode)
		errMsg := e.LoadedStrings.ExecAccessDenied
		wErr := terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(errMsg)), outputMode)
		if wErr != nil {
			slog.Error("failed writing access denied message", "error", wErr)
		}
		time.Sleep(1 * time.Second)
		return nil, "", nil // Insufficient level, treat as failed login
	}

	// Authentication Successful!
	slog.Info("user authenticated successfully via RUN:AUTHENTICATE", "node", nodeNumber, "handle", authUser.Handle)

	// Clear failed login attempts for this IP
	if e.IPLockoutCheck != nil {
		e.IPLockoutCheck.ClearFailedLoginAttempts(remoteIP)
		slog.Debug("cleared failed login attempts", "node", nodeNumber, "ip", remoteIP)
	}

	// Display success message (optional) - Move cursor first
	terminalio.WriteProcessedBytes(terminal, []byte(ansi.MoveCursor(errorRow, 1)), outputMode)
	// successMsg := "\r\n|10Login successful!|07\r\n"
	// terminal.Write(ansi.ReplacePipeCodes([]byte(successMsg)))
	// time.Sleep(500 * time.Millisecond)

	// Return the authenticated user object!
	return authUser, "", nil
}

// readPasswordSecurely reads a password from the terminal without echoing characters.
// Uses the session-scoped InputHandler to avoid racing with other menu input readers.
// Returns errInputAborted on ESC or Ctrl+C, io.EOF on disconnect.
func readPasswordSecurely(s ssh.Session, terminal *term.Terminal, outputMode ansi.OutputMode) (string, error) {
	var password []rune
	var byteBuf [1]byte
	ih := getSessionIH(s)

	for {
		key, err := ih.ReadKey()
		if err != nil {
			if errors.Is(err, io.EOF) {
				slog.Debug("EOF received during secure password read")
			}
			return "", err
		}

		switch key {
		case editor.KeyEnter:
			terminalio.WriteProcessedBytes(terminal, []byte("\r\n"), outputMode)
			return string(password), nil
		case editor.KeyBackspace:
			if len(password) > 0 {
				password = password[:len(password)-1]
				if err := terminalio.WriteProcessedBytes(terminal, []byte("\b \b"), outputMode); err != nil {
					slog.Warn("failed to write backspace sequence", "error", err)
				}
			}
		case 3: // Ctrl+C
			terminalio.WriteProcessedBytes(terminal, []byte("^C\r\n"), outputMode)
			return "", errInputAborted
		case editor.KeyEsc:
			terminalio.WriteProcessedBytes(terminal, []byte("\r\n"), outputMode)
			return "", errInputAborted
		default:
			if key >= 32 && key <= 126 {
				password = append(password, rune(key))
				byteBuf[0] = '*'
				if err := terminalio.WriteProcessedBytes(terminal, byteBuf[:], outputMode); err != nil {
					slog.Warn("failed to write asterisk", "error", err)
				}
			}
		}
	}
}
