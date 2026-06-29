package menu

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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

// runNewMailScan checks for new private mail and displays a count to the user.
func runNewMailScan(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	terminal := c.terminal
	currentUser := c.currentUser
	nodeNumber := c.nodeNumber
	outputMode := c.outputMode

	if currentUser == nil {
		return nil, "", nil
	}

	slog.Debug("running NMAILSCAN", "node", nodeNumber, "handle", currentUser.Handle)

	if e.MessageMgr == nil {
		slog.Warn("MessageMgr not available for NMAILSCAN", "node", nodeNumber)
		return currentUser, "", nil
	}

	// Get PRIVMAIL area
	privmailArea, exists := e.MessageMgr.GetAreaByTag("PRIVMAIL")
	if !exists {
		slog.Debug("PRIVMAIL area not configured, skipping mail scan", "node", nodeNumber)
		return currentUser, "", nil
	}

	// Get JAM base for PRIVMAIL area
	base, err := e.MessageMgr.GetBase(privmailArea.ID)
	if err != nil {
		slog.Warn("JAM base not open for PRIVMAIL area", "node", nodeNumber, "error", err)
		return currentUser, "", nil
	}
	defer base.Close()

	// Get total message count
	totalMessages, err := e.MessageMgr.GetMessageCountForArea(privmailArea.ID)
	if err != nil {
		slog.Warn("failed to get message count for PRIVMAIL", "node", nodeNumber, "error", err)
		return currentUser, "", nil
	}

	if totalMessages == 0 {
		msg := e.LoadedStrings.ExecNoNewMail
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		return currentUser, "", nil
	}

	// Get lastread pointer for this user
	lastRead, err := e.MessageMgr.GetLastRead(privmailArea.ID, currentUser.Handle)
	if err != nil {
		slog.Warn("failed to get lastread for PRIVMAIL", "node", nodeNumber, "error", err)
		lastRead = 0
	}

	// Count unread private messages addressed to this user
	newMailCount := 0
	for msgNum := lastRead + 1; msgNum <= totalMessages; msgNum++ {
		msg, readErr := base.ReadMessage(msgNum)
		if readErr != nil {
			continue
		}
		if msg.IsDeleted() {
			continue
		}
		if msg.IsPrivate() && strings.EqualFold(msg.To, currentUser.Handle) {
			newMailCount++
		}
	}

	if newMailCount > 0 {
		mailMsg := fmt.Sprintf(e.LoadedStrings.ExecNewMailCount, newMailCount)
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(mailMsg)), outputMode)
	} else {
		msg := e.LoadedStrings.ExecNoNewMail
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
	}

	return currentUser, "", nil
}

// runLoginDisplayFile displays an ANSI file during the login sequence.
// The filename is passed via the args parameter (from LoginItem.Data).
func runLoginDisplayFile(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	terminal := c.terminal
	currentUser := c.currentUser
	nodeNumber := c.nodeNumber
	outputMode := c.outputMode

	filename := strings.TrimSpace(args)
	if filename == "" {
		slog.Warn("DISPLAYFILE called with no filename", "node", nodeNumber)
		return currentUser, "", nil
	}

	slog.Debug("running DISPLAYFILE", "node", nodeNumber, "file", filename)

	err := e.displayFile(terminal, filename, outputMode)
	if err != nil {
		slog.Warn("failed to display file", "node", nodeNumber, "file", filename, "error", err)
		// Non-fatal - continue login sequence even if file is missing
	}

	return currentUser, "", nil
}

// runLoginDoor executes a script/program during the login sequence.
// The script path is passed via the args parameter (from LoginItem.Data).
// The node number is passed as the first argument to the script.
func runLoginDoor(c *cmdCtx, args string) (*user.User, string, error) {
	s := c.s
	currentUser := c.currentUser
	nodeNumber := c.nodeNumber

	scriptPath := strings.TrimSpace(args)
	if scriptPath == "" {
		slog.Warn("RUNDOOR called with no script path", "node", nodeNumber)
		return currentUser, "", nil
	}

	slog.Info("running login door script", "node", nodeNumber, "path", scriptPath)

	// Verify script exists
	if _, err := os.Stat(scriptPath); os.IsNotExist(err) {
		slog.Warn("login door script not found", "node", nodeNumber, "path", scriptPath)
		return currentUser, "", nil
	}

	// Execute the script with node number as argument
	cmd := exec.Command(scriptPath, strconv.Itoa(nodeNumber))
	cmd.Stdin = s
	cmd.Stdout = s
	cmd.Stderr = s.Stderr()

	if err := cmd.Run(); err != nil {
		slog.Warn("login door script exited with error", "node", nodeNumber, "path", scriptPath, "error", err)
		// Non-fatal - continue login sequence
	}

	return currentUser, "", nil
}

// runFastLogin presents the FASTLOGN menu inline during the login sequence.
// Returns a GOTO action if the user chooses to skip/jump, or empty string to continue.
func runFastLogin(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	s := c.s
	terminal := c.terminal
	currentUser := c.currentUser
	nodeNumber := c.nodeNumber
	sessionStartTime := c.sessionStartTime
	outputMode := c.outputMode
	termHeight := c.termHeight

	slog.Debug("running FASTLOGIN inline", "node", nodeNumber, "handle", currentUser.Handle)

	// Load FASTLOGN menu definition (.MNU) for CLR/CLS + prompt behavior
	var fastlognMenu *MenuRecord
	menuMnuPath := filepath.Join(e.MenuSetPath, "mnu")
	loadedMenu, menuErr := LoadMenu("FASTLOGN", menuMnuPath)
	if menuErr != nil {
		slog.Warn("failed to load FASTLOGN.MNU", "node", nodeNumber, "error", menuErr)
	} else {
		fastlognMenu = loadedMenu
	}

	renderFastLoginScreen := func() {
		clearFirst := fastlognMenu != nil && fastlognMenu.GetClrScrBefore()
		if displayErr := e.displayFile(terminal, "FASTLOGN.ANS", outputMode, clearFirst); displayErr != nil {
			slog.Warn("failed to display FASTLOGN.ANS", "node", nodeNumber, "error", displayErr)
		}

		if fastlognMenu != nil && fastlognMenu.GetUsePrompt() {
			promptParts := make([]string, 0, 2)
			if strings.TrimSpace(fastlognMenu.Prompt1) != "" {
				promptParts = append(promptParts, fastlognMenu.Prompt1)
			}
			if strings.TrimSpace(fastlognMenu.Prompt2) != "" {
				promptParts = append(promptParts, fastlognMenu.Prompt2)
			}
			if len(promptParts) > 0 {
				prompt := "\r\n" + strings.Join(promptParts, "\r\n")
				terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(prompt)), outputMode)
			}
		}
	}

	// Load FASTLOGN commands
	cfgPath := filepath.Join(e.MenuSetPath, "cfg")
	commands, err := LoadCommands("FASTLOGN", cfgPath)
	if err != nil {
		slog.Warn("failed to load FASTLOGN.CFG", "node", nodeNumber, "error", err)
		return currentUser, "", nil
	}

	renderFastLoginScreen()

	// Check for lightbar BAR file.
	barPath := filepath.Join(e.MenuSetPath, "bar", "FASTLOGN.BAR")
	lightbarOptions, barLoadErr := loadLightbarOptions("FASTLOGN", e)
	isLightbar := barLoadErr == nil && len(lightbarOptions) > 0
	if barLoadErr != nil {
		if _, statErr := os.Stat(barPath); statErr == nil {
			slog.Warn("BAR file exists but failed to load", "node", nodeNumber, "error", barLoadErr)
		}
	}

	// Dispatch command by key string against CFG commands.
	dispatchCommand := func(keyStr string) (*user.User, string, error, bool) {
		for _, cmd := range commands {
			keys := strings.Fields(strings.ToUpper(cmd.Keys))
			matched := false
			for _, key := range keys {
				if key == keyStr {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
			if cmd.ACS != "" && cmd.ACS != "*" {
				if !checkACS(cmd.ACS, currentUser, s, terminal, sessionStartTime) {
					continue
				}
			}
			if cmd.Command == "RUN:FULL_LOGIN_SEQUENCE" {
				slog.Debug("FASTLOGIN - user chose to continue full sequence", "node", nodeNumber)
				terminalio.WriteProcessedBytes(terminal, []byte("\r\n"), outputMode)
				return currentUser, "", nil, true
			}
			if strings.HasPrefix(cmd.Command, "GOTO:") {
				nextMenu := strings.ToUpper(strings.TrimPrefix(cmd.Command, "GOTO:"))
				slog.Debug("FASTLOGIN - user chose GOTO", "node", nodeNumber, "menu", nextMenu)
				terminalio.WriteProcessedBytes(terminal, []byte("\r\n"), outputMode)
				return currentUser, "GOTO:" + nextMenu, nil, true
			}
			if cmd.Command == "RUN:MAINLOGOFF" || cmd.Command == "LOGOFF" {
				return currentUser, "LOGOFF", nil, true
			}
			if cmd.Command == "RUN:IMMEDIATELOGOFF" {
				return nil, "LOGOFF", io.EOF, true
			}
		}
		return nil, "", nil, false
	}

	// Use session-scoped InputHandler so we share the single goroutine reading
	// from the SSH session (prevents "double key press" race with other menus).
	ih := getSessionIH(s)

	if isLightbar {
		slog.Debug("FASTLOGIN using lightbar mode", "node", nodeNumber, "count", len(lightbarOptions))
		selectedIndex := 0
		_ = drawLightbarMenu(terminal, nil, lightbarOptions, selectedIndex, outputMode, false)

		for {
			key, readErr := ih.ReadKey()
			if readErr != nil {
				if errors.Is(readErr, io.EOF) {
					return nil, "LOGOFF", io.EOF
				}
				if errors.Is(readErr, editor.ErrIdleTimeout) {
					e.handleIdleTimeout(terminal, outputMode, nodeNumber, termHeight)
					return currentUser, "LOGOFF", nil
				}
				return currentUser, "", readErr
			}

			switch key {
			case editor.KeyArrowUp:
				if selectedIndex > 0 {
					prev := selectedIndex
					selectedIndex--
					_ = drawLightbarOption(terminal, lightbarOptions[prev], false, outputMode)
					_ = drawLightbarOption(terminal, lightbarOptions[selectedIndex], true, outputMode)
				}
			case editor.KeyArrowDown:
				if selectedIndex < len(lightbarOptions)-1 {
					prev := selectedIndex
					selectedIndex++
					_ = drawLightbarOption(terminal, lightbarOptions[prev], false, outputMode)
					_ = drawLightbarOption(terminal, lightbarOptions[selectedIndex], true, outputMode)
				}
			case int('\r'), int('\n'): // Enter — select current
				if selectedIndex >= 0 && selectedIndex < len(lightbarOptions) {
					keyStr := lightbarOptions[selectedIndex].HotKey
					if u, action, err, matched := dispatchCommand(keyStr); matched {
						return u, action, err
					}
				}
				return currentUser, "", nil
			case editor.KeyEsc:
				// Bare ESC (InputHandler consumed any ANSI sequence) — ignore
			default:
				if key >= 32 && key < 127 {
					keyStr := strings.ToUpper(string(rune(key)))
					if key == int('/') {
						// Read second key for two-character CFG commands like /G,
						// matching the non-lightbar fallback path below.
						if nextKey, nextErr := ih.ReadKey(); nextErr == nil && nextKey >= 32 && nextKey < 127 {
							keyStr = "/" + strings.ToUpper(string(rune(nextKey)))
						}
					}
					for i, opt := range lightbarOptions {
						if keyStr == opt.HotKey {
							prev := selectedIndex
							selectedIndex = i
							if prev != selectedIndex {
								_ = drawLightbarOption(terminal, lightbarOptions[prev], false, outputMode)
								_ = drawLightbarOption(terminal, lightbarOptions[selectedIndex], true, outputMode)
							}
							if u, action, err, matched := dispatchCommand(keyStr); matched {
								return u, action, err
							}
						}
					}
					// Also check non-lightbar commands (like G, /G)
					if u, action, err, matched := dispatchCommand(keyStr); matched {
						return u, action, err
					}
				}
			}
		}
	}

	// Fallback: standard keystroke input (no lightbar).
	for {
		key, readErr := ih.ReadKey()
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return nil, "LOGOFF", io.EOF
			}
			if errors.Is(readErr, editor.ErrIdleTimeout) {
				e.handleIdleTimeout(terminal, outputMode, nodeNumber, termHeight)
				return currentUser, "LOGOFF", nil
			}
			return currentUser, "", readErr
		}

		if key == editor.KeyEsc || key < 32 || key == 127 {
			continue
		}

		keyStr := strings.ToUpper(string(rune(key)))
		if key == int('/') {
			// Read second key for two-character commands like /G
			nextKey, nextErr := ih.ReadKey()
			if nextErr == nil && nextKey >= 32 && nextKey < 127 {
				keyStr = "/" + strings.ToUpper(string(rune(nextKey)))
			}
		}

		if u, action, err, matched := dispatchCommand(keyStr); matched {
			return u, action, err
		}

		e.showUndefinedMenuInput(terminal, outputMode, nodeNumber)
		renderFastLoginScreen()
	}
}

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
