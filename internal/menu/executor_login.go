package menu

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
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
		log.Printf("WARN: Node %d: User %s tried to run AUTHENTICATE while already logged in.", nodeNumber, currentUser.Handle)
		msg := e.LoadedStrings.ExecAlreadyLoggedIn
		// Use WriteProcessedBytes
		wErr := terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		if wErr != nil {
			log.Printf("ERROR: Failed writing already logged in message: %v", wErr)
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
		log.Printf("ERROR: Node %d: Failed writing username prompt: %v", nodeNumber, wErr)
		// Continue anyway?
	}
	usernameInput, err := readLineFromSessionIHAllowAbort(s, terminal)
	if err != nil {
		if err == io.EOF {
			log.Printf("INFO: Node %d: User disconnected during username input.", nodeNumber)
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
		log.Printf("ERROR: Node %d: Failed to read username input: %v", nodeNumber, err)
		return nil, "", fmt.Errorf("failed reading username: %w", err) // Critical error
	}
	username := strings.TrimSpace(usernameInput)
	if username == "" {
		return nil, "", nil // Empty username, just redisplay login menu
	}

	// Check if user wants to apply as a new user
	if strings.EqualFold(username, "new") {
		log.Printf("INFO: Node %d: User typed 'new' in AUTHENTICATE - starting new user application", nodeNumber)
		newUserErr := e.handleNewUserApplication(s, terminal, userManager, nodeNumber, outputMode, termWidth, termHeight)
		if newUserErr != nil {
			if errors.Is(newUserErr, io.EOF) {
				return nil, "LOGOFF", io.EOF
			}
			log.Printf("ERROR: Node %d: New user application error: %v", nodeNumber, newUserErr)
		}
		return nil, "", nil // Return to LOGIN screen after signup
	}

	// Move to Password position, display prompt, and read input securely
	terminalio.WriteProcessedBytes(terminal, []byte(ansi.MoveCursor(passRow, passCol)), outputMode)
	passwordPrompt := e.LoadedStrings.ExecPasswordPrompt
	wErr = terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(passwordPrompt)), outputMode)
	if wErr != nil {
		log.Printf("ERROR: Node %d: Failed writing password prompt: %v", nodeNumber, wErr)
	}
	password, err := readPasswordSecurely(s, terminal, outputMode)
	if err != nil {
		if errors.Is(err, io.EOF) {
			log.Printf("INFO: Node %d: User disconnected during password input.", nodeNumber)
			return nil, "LOGOFF", io.EOF // Signal logoff
		}
		if errors.Is(err, errInputAborted) {
			log.Printf("INFO: Node %d: User pressed ESC during password entry.", nodeNumber)
			abort, confirmErr := e.confirmAbortLogin(s, terminal, outputMode, nodeNumber, termWidth, termHeight)
			if confirmErr != nil {
				return nil, "", confirmErr
			}
			if abort {
				return nil, "LOGOFF", io.EOF
			}
			return nil, "", nil
		}
		log.Printf("ERROR: Node %d: Failed to read password securely: %v", nodeNumber, err)
		return nil, "", fmt.Errorf("failed reading password: %w", err) // Critical error
	}

	// Get remote IP address for lockout checking
	remoteIP := remoteIPFromSession(s)

	// Check if this IP is currently locked out
	if e.IPLockoutCheck != nil {
		isLocked, lockedUntil, attempts := e.IPLockoutCheck.IsIPLockedOut(remoteIP)
		if isLocked {
			log.Printf("SECURITY: Node %d: Login attempt from locked IP %s (locked until %s, %d attempts)",
				nodeNumber, remoteIP, lockedUntil.Format("2006-01-02 15:04:05"), attempts)
			terminalio.WriteProcessedBytes(terminal, []byte(ansi.MoveCursor(errorRow, 1)), outputMode)
			minutesLeft := int(time.Until(lockedUntil).Minutes()) + 1
			errMsg := fmt.Sprintf(e.LoadedStrings.ExecIPLockout, minutesLeft)
			wErr := terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(errMsg)), outputMode)
			if wErr != nil {
				log.Printf("ERROR: Failed writing IP lockout message: %v", wErr)
			}
			time.Sleep(2 * time.Second)
			return nil, "", nil
		}
	}

	// Attempt Authentication via UserManager
	log.Printf("DEBUG: Node %d: Attempting authentication for user: %s from IP: %s", nodeNumber, username, remoteIP)
	authUser, authenticated := userManager.Authenticate(username, password)
	if !authenticated {
		log.Printf("WARN: Node %d: Failed authentication attempt for user: %s from IP: %s", nodeNumber, username, remoteIP)

		// Record failed login attempt for this IP
		if e.IPLockoutCheck != nil {
			wasLocked := e.IPLockoutCheck.RecordFailedLoginAttempt(remoteIP)
			if wasLocked {
				log.Printf("SECURITY: Node %d: IP %s has been locked out after too many failed attempts", nodeNumber, remoteIP)
			}
		}

		// Display error message to user
		terminalio.WriteProcessedBytes(terminal, []byte(ansi.MoveCursor(errorRow, 1)), outputMode) // Move cursor for message
		errMsg := e.LoadedStrings.ExecLoginIncorrect
		// Use WriteProcessedBytes
		wErr := terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(errMsg)), outputMode)
		if wErr != nil {
			log.Printf("ERROR: Failed writing login incorrect message: %v", wErr)
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
		log.Printf("INFO: Node %d: Login denied for user '%s' - insufficient access level (has %d, needs %d)",
			nodeNumber, username, authUser.AccessLevel, cfg.LogonLevel)
		terminalio.WriteProcessedBytes(terminal, []byte(ansi.MoveCursor(errorRow, 1)), outputMode)
		errMsg := e.LoadedStrings.ExecAccessDenied
		wErr := terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(errMsg)), outputMode)
		if wErr != nil {
			log.Printf("ERROR: Failed writing access denied message: %v", wErr)
		}
		time.Sleep(1 * time.Second)
		return nil, "", nil // Insufficient level, treat as failed login
	}

	// Authentication Successful!
	log.Printf("INFO: Node %d: User '%s' authenticated successfully via RUN:AUTHENTICATE", nodeNumber, authUser.Handle)

	// Clear failed login attempts for this IP
	if e.IPLockoutCheck != nil {
		e.IPLockoutCheck.ClearFailedLoginAttempts(remoteIP)
		log.Printf("DEBUG: Node %d: Cleared failed login attempts for IP %s", nodeNumber, remoteIP)
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

	log.Printf("DEBUG: LOGIN Coords Received - P: %+v (Ok: %t), O: %+v (Ok: %t)", userCoord, userOk, passCoord, passOk)

	if !userOk || !passOk {
		log.Printf("CRITICAL: LOGIN.ANS is missing required coordinate codes P or O.")
		if wErr := terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.LoadedStrings.ExecLoginCriticalError)), outputMode); wErr != nil {
			log.Printf("ERROR: Failed writing critical login configuration message: %v", wErr)
		}
		time.Sleep(2 * time.Second)
		return nil, fmt.Errorf("missing login coordinates P/O in LOGIN.ANS")
	}

	// No Y offset needed — ANSI display is truncated to termHeight rows,
	// preventing scrolling, so extracted coordinates are accurate as-is
	log.Printf("DEBUG: Node %d: Login prompt coords P=(%d,%d) O=(%d,%d) termHeight=%d", nodeNumber, userCoord.X, userCoord.Y, passCoord.X, passCoord.Y, termHeight)

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
		log.Printf("ERROR: Node %d: Failed to read username input: %v", nodeNumber, err)
		return nil, fmt.Errorf("failed reading username: %w", err)
	}
	username := strings.TrimSpace(usernameInput)
	if username == "" {
		log.Printf("DEBUG: Node %d: Empty username entered.", nodeNumber)
		return nil, nil // Return nil user, nil error to signal retry LOGIN
	}

	// Check if user wants to apply as a new user
	if strings.EqualFold(username, "new") {
		log.Printf("INFO: Node %d: User typed 'new' - starting new user application", nodeNumber)
		err := e.handleNewUserApplication(s, terminal, userManager, nodeNumber, outputMode, termWidth, termHeight)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil, io.EOF
			}
			log.Printf("ERROR: Node %d: New user application error: %v", nodeNumber, err)
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
			log.Printf("INFO: Node %d: User pressed ESC during password entry.", nodeNumber)
			abort, confirmErr := e.confirmAbortLogin(s, terminal, outputMode, nodeNumber, termWidth, termHeight)
			if confirmErr != nil {
				return nil, confirmErr
			}
			if abort {
				return nil, io.EOF
			}
			return nil, nil
		}
		log.Printf("ERROR: Node %d: Failed to read password securely: %v", nodeNumber, err)
		return nil, fmt.Errorf("failed reading password: %w", err)
	}

	// Get remote IP address for lockout checking
	remoteIP := remoteIPFromSession(s)

	// Check if this IP is currently locked out
	if e.IPLockoutCheck != nil {
		isLocked, lockedUntil, attempts := e.IPLockoutCheck.IsIPLockedOut(remoteIP)
		if isLocked {
			log.Printf("SECURITY: Node %d: Login attempt from locked IP %s (locked until %s, %d attempts)",
				nodeNumber, remoteIP, lockedUntil.Format("2006-01-02 15:04:05"), attempts)
			terminalio.WriteProcessedBytes(terminal, []byte(ansi.MoveCursor(errorRow, 1)), outputMode)
			minutesLeft := int(time.Until(lockedUntil).Minutes()) + 1
			errMsg := fmt.Sprintf(e.LoadedStrings.ExecIPLockout, minutesLeft)
			wErr := terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(errMsg)), outputMode)
			if wErr != nil {
				log.Printf("ERROR: Failed writing IP lockout message: %v", wErr)
			}
			time.Sleep(2 * time.Second)
			return nil, nil
		}
	}

	// Attempt Authentication via UserManager
	log.Printf("DEBUG: Node %d: Attempting authentication for user: %s from IP: %s", nodeNumber, username, remoteIP)
	authUser, authenticated := userManager.Authenticate(username, password)
	if !authenticated {
		log.Printf("WARN: Node %d: Failed authentication attempt for user: %s from IP: %s", nodeNumber, username, remoteIP)

		// Record failed login attempt for this IP
		if e.IPLockoutCheck != nil {
			wasLocked := e.IPLockoutCheck.RecordFailedLoginAttempt(remoteIP)
			if wasLocked {
				log.Printf("SECURITY: Node %d: IP %s has been locked out after too many failed attempts", nodeNumber, remoteIP)
			}
		}

		terminalio.WriteProcessedBytes(terminal, []byte(ansi.MoveCursor(errorRow, 1)), outputMode) // Move cursor for message
		errMsg := e.LoadedStrings.ExecLoginIncorrect
		// Use WriteProcessedBytes with the passed outputMode
		wErr := terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(errMsg)), outputMode)
		if wErr != nil {
			log.Printf("ERROR: Failed writing login incorrect message: %v", wErr)
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
		log.Printf("INFO: Node %d: Login denied for user '%s' - insufficient access level (has %d, needs %d)",
			nodeNumber, username, authUser.AccessLevel, cfg.LogonLevel)
		terminalio.WriteProcessedBytes(terminal, []byte(ansi.MoveCursor(errorRow, 1)), outputMode)
		errMsg := e.LoadedStrings.ExecAccessDenied
		wErr := terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(errMsg)), outputMode)
		if wErr != nil {
			log.Printf("ERROR: Failed writing access denied message: %v", wErr)
		}
		time.Sleep(1 * time.Second)
		return nil, nil // Insufficient level, treat as failed login
	}

	log.Printf("INFO: Node %d: User '%s' authenticated successfully via LOGIN prompt", nodeNumber, authUser.Handle)

	// Clear failed login attempts for this IP
	if e.IPLockoutCheck != nil {
		e.IPLockoutCheck.ClearFailedLoginAttempts(remoteIP)
		log.Printf("DEBUG: Node %d: Cleared failed login attempts for IP %s", nodeNumber, remoteIP)
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
				log.Println("DEBUG: EOF received during secure password read.")
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
					log.Printf("WARN: Failed to write backspace sequence: %v", err)
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
					log.Printf("WARN: Failed to write asterisk: %v", err)
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

	log.Printf("DEBUG: Node %d: Running NMAILSCAN for user %s", nodeNumber, currentUser.Handle)

	if e.MessageMgr == nil {
		log.Printf("WARN: Node %d: MessageMgr not available for NMAILSCAN", nodeNumber)
		return currentUser, "", nil
	}

	// Get PRIVMAIL area
	privmailArea, exists := e.MessageMgr.GetAreaByTag("PRIVMAIL")
	if !exists {
		log.Printf("DEBUG: Node %d: PRIVMAIL area not configured, skipping mail scan", nodeNumber)
		return currentUser, "", nil
	}

	// Get JAM base for PRIVMAIL area
	base, err := e.MessageMgr.GetBase(privmailArea.ID)
	if err != nil {
		log.Printf("WARN: Node %d: JAM base not open for PRIVMAIL area: %v", nodeNumber, err)
		return currentUser, "", nil
	}
	defer base.Close()

	// Get total message count
	totalMessages, err := e.MessageMgr.GetMessageCountForArea(privmailArea.ID)
	if err != nil {
		log.Printf("WARN: Node %d: Failed to get message count for PRIVMAIL: %v", nodeNumber, err)
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
		log.Printf("WARN: Node %d: Failed to get lastread for PRIVMAIL: %v", nodeNumber, err)
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
		log.Printf("WARN: Node %d: DISPLAYFILE called with no filename", nodeNumber)
		return currentUser, "", nil
	}

	log.Printf("DEBUG: Node %d: Running DISPLAYFILE for %s", nodeNumber, filename)

	err := e.displayFile(terminal, filename, outputMode)
	if err != nil {
		log.Printf("WARN: Node %d: Failed to display file %s: %v", nodeNumber, filename, err)
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
		log.Printf("WARN: Node %d: RUNDOOR called with no script path", nodeNumber)
		return currentUser, "", nil
	}

	log.Printf("INFO: Node %d: Running login door script: %s", nodeNumber, scriptPath)

	// Verify script exists
	if _, err := os.Stat(scriptPath); os.IsNotExist(err) {
		log.Printf("WARN: Node %d: Login door script not found: %s", nodeNumber, scriptPath)
		return currentUser, "", nil
	}

	// Execute the script with node number as argument
	cmd := exec.Command(scriptPath, strconv.Itoa(nodeNumber))
	cmd.Stdin = s
	cmd.Stdout = s
	cmd.Stderr = s.Stderr()

	if err := cmd.Run(); err != nil {
		log.Printf("WARN: Node %d: Login door script %s exited with error: %v", nodeNumber, scriptPath, err)
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

	log.Printf("DEBUG: Node %d: Running FASTLOGIN inline for user %s", nodeNumber, currentUser.Handle)

	// Load FASTLOGN menu definition (.MNU) for CLR/CLS + prompt behavior
	var fastlognMenu *MenuRecord
	menuMnuPath := filepath.Join(e.MenuSetPath, "mnu")
	loadedMenu, menuErr := LoadMenu("FASTLOGN", menuMnuPath)
	if menuErr != nil {
		log.Printf("WARN: Node %d: Failed to load FASTLOGN.MNU: %v", nodeNumber, menuErr)
	} else {
		fastlognMenu = loadedMenu
	}

	renderFastLoginScreen := func() {
		clearFirst := fastlognMenu != nil && fastlognMenu.GetClrScrBefore()
		if displayErr := e.displayFile(terminal, "FASTLOGN.ANS", outputMode, clearFirst); displayErr != nil {
			log.Printf("WARN: Node %d: Failed to display FASTLOGN.ANS: %v", nodeNumber, displayErr)
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
		log.Printf("WARN: Node %d: Failed to load FASTLOGN.CFG: %v", nodeNumber, err)
		return currentUser, "", nil
	}

	renderFastLoginScreen()

	// Check for lightbar BAR file.
	barPath := filepath.Join(e.MenuSetPath, "bar", "FASTLOGN.BAR")
	lightbarOptions, barLoadErr := loadLightbarOptions("FASTLOGN", e)
	isLightbar := barLoadErr == nil && len(lightbarOptions) > 0
	if barLoadErr != nil {
		if _, statErr := os.Stat(barPath); statErr == nil {
			log.Printf("WARN: Node %d: BAR file exists but failed to load: %v", nodeNumber, barLoadErr)
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
				log.Printf("DEBUG: Node %d: FASTLOGIN - user chose to continue full sequence", nodeNumber)
				terminalio.WriteProcessedBytes(terminal, []byte("\r\n"), outputMode)
				return currentUser, "", nil, true
			}
			if strings.HasPrefix(cmd.Command, "GOTO:") {
				nextMenu := strings.ToUpper(strings.TrimPrefix(cmd.Command, "GOTO:"))
				log.Printf("DEBUG: Node %d: FASTLOGIN - user chose GOTO:%s", nodeNumber, nextMenu)
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
		log.Printf("DEBUG: Node %d: FASTLOGIN using lightbar mode (%d options)", nodeNumber, len(lightbarOptions))
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
	log.Printf("INFO: Node %d: Running FULL_LOGIN_SEQUENCE for user %s (%d items configured)", nodeNumber, currentUser.Handle, len(loginSequence))

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
			log.Printf("DEBUG: Node %d: Skipping login item %d (%s) - user level %d < required %d",
				nodeNumber, i+1, item.Command, currentUser.AccessLevel, item.SecLevel)
			continue
		}

		log.Printf("DEBUG: Node %d: Executing login item %d/%d: %s", nodeNumber, i+1, len(loginSequence), item.Command)

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
			log.Printf("INFO: Node %d: Executing door '%s' from login sequence", nodeNumber, doorName)

			// Call the DOOR: handler from RunRegistry
			if doorFunc, exists := e.RunRegistry["DOOR:"]; exists {
				updatedUser, nextAction, err = doorFunc(&cmdCtx{e: e, s: s, terminal: terminal, userManager: userManager, currentUser: currentUser, nodeNumber: nodeNumber, sessionStartTime: sessionStartTime, outputMode: outputMode, termWidth: termWidth, termHeight: termHeight}, doorName)
				if updatedUser != nil {
					currentUser = updatedUser
				}
			} else {
				log.Printf("ERROR: Node %d: DOOR: handler not registered", nodeNumber)
				continue
			}
		} else {
			// Look up and execute the handler from the local handlers map
			handler, exists := handlers[item.Command]
			if !exists {
				log.Printf("WARN: Node %d: Unknown login sequence command: %s", nodeNumber, item.Command)
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
			log.Printf("ERROR: Node %d: Error during login item %s: %v", nodeNumber, item.Command, err)
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
			log.Printf("DEBUG: Node %d: Login sequence interrupted by %s -> %s", nodeNumber, item.Command, nextAction)
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
	log.Printf("DEBUG: Node %d: FULL_LOGIN_SEQUENCE completed. Transitioning to MAIN.", nodeNumber)
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
