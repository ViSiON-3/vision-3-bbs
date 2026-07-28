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

// runBanUser quickly bans a user by setting access level to 0 and validation to false.
func runBanUser(c *cmdCtx, args string) (*user.User, string, error) {
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

	slog.Debug("running BANUSER", "node", nodeNumber)

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

	targetUser, selected, pickErr := adminUserLightbarBrowser(s, terminal, users, "Ban User", "Select a user. [Enter] ban, [Q] quit.", outputMode, true)
	if pickErr != nil {
		if errors.Is(pickErr, io.EOF) {
			return nil, "LOGOFF", io.EOF
		}
		return nil, "", pickErr
	}
	if !selected || targetUser == nil {
		return nil, "", nil
	}

	// Protect User #1
	if targetUser.ID == 1 {
		msg := "\r\n\r\n|01Cannot ban User #1!|07"
		_ = terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		if pauseErr := e.loginPausePrompt(s, terminal, nodeNumber, outputMode, termWidth, termHeight); pauseErr != nil {
			if errors.Is(pauseErr, io.EOF) {
				return nil, "LOGOFF", io.EOF
			}
			return nil, "", pauseErr
		}
		return nil, "", nil
	}

	confirmPrompt := fmt.Sprintf("\r\n\r\n|07Ban |15%s|07 (set level 0 + unvalidated)? @", targetUser.Handle)
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

	// Route through the shared save path for optimistic locking and audit logging.
	orig := map[int]time.Time{targetUser.ID: targetUser.UpdatedAt}
	if statusMsg, saved := e.applyPendingUserChanges(userManager, currentUser, targetUser, map[string]interface{}{"validated": false, "level": 0}, orig); !saved {
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

	success := fmt.Sprintf("\r\n\r\n|10User banned: |15%s|10 (level 0, unvalidated).|07", targetUser.Handle)
	_ = terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(success)), outputMode)
	if pauseErr := e.loginPausePrompt(s, terminal, nodeNumber, outputMode, termWidth, termHeight); pauseErr != nil {
		if errors.Is(pauseErr, io.EOF) {
			return nil, "LOGOFF", io.EOF
		}
		return nil, "", pauseErr
	}

	return nil, "", nil
}

// runDeleteUser soft-deletes a user by setting DeletedUser=true and recording the deletion timestamp.
func runDeleteUser(c *cmdCtx, args string) (*user.User, string, error) {
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

	slog.Debug("running DELETEUSER", "node", nodeNumber)

	if currentUser == nil {
		msg := "\r\n|01Error: You must be logged in to delete users.|07\r\n"
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

	targetUser, selected, pickErr := adminUserLightbarBrowser(s, terminal, users, "Delete User", "Select a user. [Enter] delete, [Q] quit.", outputMode, true)
	if pickErr != nil {
		if errors.Is(pickErr, io.EOF) {
			return nil, "LOGOFF", io.EOF
		}
		return nil, "", pickErr
	}
	if !selected || targetUser == nil {
		return nil, "", nil
	}

	// Protect User #1
	if targetUser.ID == 1 {
		msg := "\r\n\r\n|01Cannot delete User #1!|07"
		_ = terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		if pauseErr := e.loginPausePrompt(s, terminal, nodeNumber, outputMode, termWidth, termHeight); pauseErr != nil {
			if errors.Is(pauseErr, io.EOF) {
				return nil, "LOGOFF", io.EOF
			}
			return nil, "", pauseErr
		}
		return nil, "", nil
	}

	confirmPrompt := fmt.Sprintf("\r\n\r\n|07Delete |15%s|07 (soft delete - data preserved)? @", targetUser.Handle)
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

	// Route through the shared save path for optimistic locking and audit logging
	// (applyPendingUserChanges sets DeletedAt when "deleted" is true).
	orig := map[int]time.Time{targetUser.ID: targetUser.UpdatedAt}
	if statusMsg, saved := e.applyPendingUserChanges(userManager, currentUser, targetUser, map[string]interface{}{"deleted": true}, orig); !saved {
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

	success := fmt.Sprintf("\r\n\r\n|10User deleted: |15%s|10 (soft delete - data preserved).|07", targetUser.Handle)
	_ = terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(success)), outputMode)
	if pauseErr := e.loginPausePrompt(s, terminal, nodeNumber, outputMode, termWidth, termHeight); pauseErr != nil {
		if errors.Is(pauseErr, io.EOF) {
			return nil, "LOGOFF", io.EOF
		}
		return nil, "", pauseErr
	}

	return nil, "", nil
}
