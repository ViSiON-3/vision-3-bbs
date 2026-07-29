package menu

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/logging"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"github.com/gliderlabs/ssh"
	"golang.org/x/term"
)

// handleLoginPrompt manages the interactive username/password entry using coordinates.
// Added outputMode parameter.
func (e *MenuExecutor) handleLoginPrompt(s ssh.Session, terminal *term.Terminal, userManager *user.UserMgr, nodeNumber int, coords map[string]struct{ X, Y int }, colors map[string]string, outputMode ansi.OutputMode, termWidth int, termHeight int) (*user.User, error) {
	// Get coordinates for username and password fields from the map
	userCoord, userOk := coords["P"] // Use 'P' for Handle/Name field based on LOGIN.ANS
	passCoord, passOk := coords["O"] // Use 'O' for Password field based on LOGIN.ANS

	slog.Debug("login coords received", "p", userCoord, "pOk", userOk, "o", passCoord, "oOk", passOk)

	if !userOk || !passOk {
		slog.Error("LOGIN.ANS is missing required coordinate codes P or O")
		if wErr := terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.LoadedStrings.ExecLoginCriticalError)), outputMode); wErr != nil {
			slog.Error("failed writing critical login configuration message", "error", wErr)
		}
		time.Sleep(2 * time.Second)
		return nil, fmt.Errorf("missing login coordinates P/O in LOGIN.ANS")
	}

	// No Y offset needed — ANSI display is truncated to termHeight rows,
	// preventing scrolling, so extracted coordinates are accurate as-is
	slog.Debug("login prompt coords", "node", nodeNumber, "pX", userCoord.X, "pY", userCoord.Y, "oX", passCoord.X, "oY", passCoord.Y, "termHeight", termHeight)

	errorRow := passCoord.Y + 2 // Error message row below password
	if errorRow <= userCoord.Y || errorRow <= passCoord.Y {
		errorRow = userCoord.Y + 2 // Adjust if overlapping
	}

	// Move to Username position (coordinates are accurate since display is truncated to fit)
	terminalio.WriteProcessedBytes(terminal, []byte(ansi.MoveCursor(userCoord.Y, userCoord.X)), outputMode)
	// Apply the color that was at the |P position in the ANSI file
	if userColor, ok := colors["P"]; ok && userColor != "" {
		terminalio.WriteProcessedBytes(terminal, []byte(userColor), outputMode)
	}
	usernameInput, err := readLineFromSessionIHAllowAbort(s, terminal)
	// Reset color attributes after input (required for bright colors)
	terminalio.WriteProcessedBytes(terminal, []byte("\x1b[0m"), outputMode)
	if err != nil {
		if err == io.EOF {
			return nil, io.EOF // Signal disconnection
		}
		if errors.Is(err, errInputAborted) {
			abort, confirmErr := e.confirmAbortLogin(s, terminal, outputMode, nodeNumber, termWidth, termHeight)
			if confirmErr != nil {
				return nil, confirmErr
			}
			if abort {
				return nil, io.EOF
			}
			return nil, nil
		}
		slog.Error("failed to read username input", "node", nodeNumber, "error", err)
		return nil, fmt.Errorf("failed reading username: %w", err)
	}
	username := strings.TrimSpace(usernameInput)
	if username == "" {
		slog.Debug("empty username entered", "node", nodeNumber)
		return nil, nil // Return nil user, nil error to signal retry LOGIN
	}

	// Check if user wants to apply as a new user
	if strings.EqualFold(username, "new") {
		slog.Info("user typed 'new' - starting new user application", "node", nodeNumber)
		err := e.handleNewUserApplication(s, terminal, userManager, nodeNumber, outputMode, termWidth, termHeight)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil, io.EOF
			}
			slog.Error("new user application error", "node", nodeNumber, "error", err)
		}
		return nil, nil // Return to LOGIN screen after signup
	}

	// Move to Password position (coordinates are accurate since display is truncated to fit)
	terminalio.WriteProcessedBytes(terminal, []byte(ansi.MoveCursor(passCoord.Y, passCoord.X)), outputMode)
	// Apply the color that was at the |O position in the ANSI file
	if passColor, ok := colors["O"]; ok && passColor != "" {
		terminalio.WriteProcessedBytes(terminal, []byte(passColor), outputMode)
	}
	password, err := readPasswordSecurely(s, terminal, outputMode) // Use ssh.Session 's'
	// Reset color attributes after input (required for bright colors)
	terminalio.WriteProcessedBytes(terminal, []byte("\x1b[0m"), outputMode)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, io.EOF // Signal disconnection
		}
		if errors.Is(err, errInputAborted) { // ESC/Ctrl+C
			slog.Info("user pressed ESC during password entry", "node", nodeNumber)
			abort, confirmErr := e.confirmAbortLogin(s, terminal, outputMode, nodeNumber, termWidth, termHeight)
			if confirmErr != nil {
				return nil, confirmErr
			}
			if abort {
				return nil, io.EOF
			}
			return nil, nil
		}
		slog.Error("failed to read password securely", "node", nodeNumber, "error", err)
		return nil, fmt.Errorf("failed reading password: %w", err)
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
			return nil, nil
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

		terminalio.WriteProcessedBytes(terminal, []byte(ansi.MoveCursor(errorRow, 1)), outputMode) // Move cursor for message
		errMsg := e.LoadedStrings.ExecLoginIncorrect
		// Use WriteProcessedBytes with the passed outputMode
		wErr := terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(errMsg)), outputMode)
		if wErr != nil {
			slog.Error("failed writing login incorrect message", "error", wErr)
		}
		time.Sleep(1 * time.Second) // Pause after failed attempt
		return nil, nil             // Failed auth, but not a critical error. Let LOGIN menu handle retries.
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
		return nil, nil // Insufficient level, treat as failed login
	}

	slog.Info("user authenticated successfully via LOGIN prompt", "node", nodeNumber, "handle", authUser.Handle)

	// Clear failed login attempts for this IP
	if e.IPLockoutCheck != nil {
		e.IPLockoutCheck.ClearFailedLoginAttempts(remoteIP)
		slog.Debug("cleared failed login attempts", "node", nodeNumber, "ip", remoteIP)
	}

	return authUser, nil // Success!
}
