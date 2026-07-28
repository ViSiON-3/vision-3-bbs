package menu

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// runValidateUser presents the pending-validation queue: a filtered view of the
// shared user editor (runUserEditor) that drops users from the list as they are
// validated.
func runValidateUser(c *cmdCtx, args string) (*user.User, string, error) {
	return runUserEditor(c, userEditorConfig{
		title:        "|15ViSiON|03/|113 |08- |14Validate Pending Users|07",
		emptyMessage: "|10No users pending validation.",
		logLabel:     "VALIDATEUSER",
		pendingOnly:  true,
	})
}

// runNewUserValidation checks for unvalidated users and prompts to review them.
func runNewUserValidation(c *cmdCtx, args string) (*user.User, string, error) {
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

	slog.Debug("running NEWUSERVAL", "node", nodeNumber)

	if currentUser == nil {
		return nil, "", nil
	}

	if userManager == nil {
		return nil, "", nil
	}

	// Security level check is handled by login.json sec_level field

	// Get all unvalidated users (skip deleted and banned users)
	allUsers := userManager.GetAllUsers()
	pendingCount := 0
	for _, u := range allUsers {
		if isPendingValidationUser(u) {
			pendingCount++
		}
	}

	if pendingCount == 0 {
		msg := "\r\n|08No new users to validate...|07\r\n"
		_ = terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		time.Sleep(1 * time.Second)
		return nil, "", nil
	}

	// Display count and prompt
	var countText string
	if pendingCount == 1 {
		countText = "1 new user"
	} else {
		countText = fmt.Sprintf("%d new users", pendingCount)
	}

	promptText := fmt.Sprintf("\r\n|15%s.|07 Review?", countText)
	_ = terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(promptText)), outputMode)

	// Use canonical Y/N prompt
	answer, err := e.PromptYesNo(s, terminal, "", outputMode, nodeNumber, termWidth, termHeight, false)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, "LOGOFF", io.EOF
		}
		return nil, "", err
	}

	if answer {
		// User said Yes - launch validate user
		return runValidateUser(&cmdCtx{e: e, s: s, terminal: terminal, userManager: userManager, currentUser: currentUser, nodeNumber: nodeNumber, sessionStartTime: sessionStartTime, outputMode: outputMode, termWidth: termWidth, termHeight: termHeight}, args)
	}

	// User said No - continue
	return nil, "", nil
}

// runUnvalidateUser removes validation status from a user account.
func runUnvalidateUser(c *cmdCtx, args string) (*user.User, string, error) {
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

	slog.Debug("running UNVALIDATEUSER", "node", nodeNumber)

	if currentUser == nil {
		msg := "\r\n|01Error: You must be logged in to modify users.|07\r\n"
		_ = terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		time.Sleep(1 * time.Second)
		return nil, "", nil
	}

	if userManager == nil {
		msg := "\r\n|01Error: User manager is not available.|07\r\n"
		_ = terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		time.Sleep(1 * time.Second)
		return nil, "", nil
	}

	sysOpACS := fmt.Sprintf("S%d", e.ServerCfg.SysOpLevel)
	if !checkACS(sysOpACS, currentUser, s, terminal, sessionStartTime) {
		msg := "\r\n|01Access denied.|07\r\n"
		_ = terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		time.Sleep(1 * time.Second)
		return nil, "", nil
	}

	users := sortedUsersByID(userManager.GetAllUsers())
	if len(users) == 0 {
		_ = terminalio.WriteProcessedBytes(terminal, []byte(ansi.ClearScreen()), outputMode)
		_ = terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n|10No users found.|07")), outputMode)
		if pauseErr := e.loginPausePrompt(s, terminal, nodeNumber, outputMode, termWidth, termHeight); pauseErr != nil {
			if errors.Is(pauseErr, io.EOF) {
				return nil, "LOGOFF", io.EOF
			}
			return nil, "", pauseErr
		}
		return nil, "", nil
	}

	targetUser, selected, pickErr := adminUserLightbarBrowser(s, terminal, users, "Unvalidate User", "Select a user. [Enter] unvalidate, [Q] quit.", outputMode, true)
	if pickErr != nil {
		if errors.Is(pickErr, io.EOF) {
			return nil, "LOGOFF", io.EOF
		}
		return nil, "", pickErr
	}
	if !selected || targetUser == nil {
		return nil, "", nil
	}

	confirmPrompt := fmt.Sprintf("\r\n\r\n|07Set |15%s|07 to unvalidated? @", targetUser.Handle)
	confirm, confirmErr := e.PromptYesNo(s, terminal, confirmPrompt, outputMode, nodeNumber, termWidth, termHeight, false)
	if confirmErr != nil {
		if errors.Is(confirmErr, io.EOF) {
			return nil, "LOGOFF", io.EOF
		}
		return nil, "", confirmErr
	}

	if !confirm {
		msg := "\r\n|07Cancelled.|07"
		_ = terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		if pauseErr := e.loginPausePrompt(s, terminal, nodeNumber, outputMode, termWidth, termHeight); pauseErr != nil {
			if errors.Is(pauseErr, io.EOF) {
				return nil, "LOGOFF", io.EOF
			}
			return nil, "", pauseErr
		}
		return nil, "", nil
	}

	// Route through the shared save path for optimistic locking, audit logging,
	// and User #1 protection.
	orig := map[int]time.Time{targetUser.ID: targetUser.UpdatedAt}
	if statusMsg, saved := e.applyPendingUserChanges(userManager, currentUser, targetUser, map[string]interface{}{"validated": false}, orig); !saved {
		msg := "\r\n\r\n" + statusMsg
		_ = terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		if pauseErr := e.loginPausePrompt(s, terminal, nodeNumber, outputMode, termWidth, termHeight); pauseErr != nil {
			if errors.Is(pauseErr, io.EOF) {
				return nil, "LOGOFF", io.EOF
			}
			return nil, "", pauseErr
		}
		return nil, "", nil
	}

	success := fmt.Sprintf("\r\n\r\n|10User set to unvalidated: |15%s|10.|07", targetUser.Handle)
	_ = terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(success)), outputMode)
	if pauseErr := e.loginPausePrompt(s, terminal, nodeNumber, outputMode, termWidth, termHeight); pauseErr != nil {
		if errors.Is(pauseErr, io.EOF) {
			return nil, "LOGOFF", io.EOF
		}
		return nil, "", pauseErr
	}

	return nil, "", nil
}
