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

// runPurgeUsers permanently removes soft-deleted users that have exceeded the
// configured retention period (deletedUserRetentionDays in config.json).
func runPurgeUsers(c *cmdCtx, args string) (*user.User, string, error) {
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

	slog.Debug("running PURGEUSERS", "node", nodeNumber)

	if currentUser == nil || userManager == nil {
		msg := "\r\n|01Error: You must be logged in to purge users.|07\r\n"
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

	retentionDays := e.ServerCfg.DeletedUserRetentionDays
	if retentionDays < 0 {
		msg := "\r\n|14User purge is disabled (deletedUserRetentionDays = -1).|07\r\n"
		_ = terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		if pauseErr := e.loginPausePrompt(s, terminal, nodeNumber, outputMode, termWidth, termHeight); pauseErr != nil {
			if errors.Is(pauseErr, io.EOF) {
				return nil, "LOGOFF", io.EOF
			}
			return nil, "", pauseErr
		}
		return nil, "", nil
	}

	// Count eligible users before committing
	cutoff := time.Now().AddDate(0, 0, -retentionDays)
	allUsers := userManager.GetAllUsers()
	var eligible []*user.User
	for _, u := range allUsers {
		if !u.DeletedUser {
			continue
		}
		if u.DeletedAt == nil || u.DeletedAt.Before(cutoff) {
			eligible = append(eligible, u)
		}
	}

	_ = terminalio.WriteProcessedBytes(terminal, []byte(ansi.ClearScreen()), outputMode)

	if len(eligible) == 0 {
		msg := fmt.Sprintf("\r\n|10No users eligible for purge.|07 (retention: |15%d|07 days)\r\n", retentionDays)
		_ = terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		if pauseErr := e.loginPausePrompt(s, terminal, nodeNumber, outputMode, termWidth, termHeight); pauseErr != nil {
			if errors.Is(pauseErr, io.EOF) {
				return nil, "LOGOFF", io.EOF
			}
			return nil, "", pauseErr
		}
		return nil, "", nil
	}

	// Show eligible users
	header := fmt.Sprintf("\r\n|11Purge Deleted Users|07 (retention: |15%d|07 days, cutoff: |15%s|07)\r\n\r\n",
		retentionDays, cutoff.Format("2006-01-02"))
	_ = terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(header)), outputMode)

	for _, u := range eligible {
		deletedOn := "(no timestamp)"
		if u.DeletedAt != nil {
			deletedOn = u.DeletedAt.Format("2006-01-02")
		}
		line := fmt.Sprintf("  |15#%-4d|07  %-20s  deleted |14%s|07\r\n",
			u.ID, u.Handle, deletedOn)
		_ = terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(line)), outputMode)
	}

	confirmPrompt := fmt.Sprintf("\r\n|07Permanently delete |01%d|07 account(s)? This cannot be undone. @", len(eligible))
	confirm, confirmErr := e.PromptYesNo(s, terminal, confirmPrompt, outputMode, nodeNumber, termWidth, termHeight, false)
	if confirmErr != nil {
		if errors.Is(confirmErr, io.EOF) {
			return nil, "LOGOFF", io.EOF
		}
		return nil, "", confirmErr
	}

	if !confirm {
		msg := "\r\n|07Cancelled.|07\r\n"
		_ = terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		if pauseErr := e.loginPausePrompt(s, terminal, nodeNumber, outputMode, termWidth, termHeight); pauseErr != nil {
			if errors.Is(pauseErr, io.EOF) {
				return nil, "LOGOFF", io.EOF
			}
			return nil, "", pauseErr
		}
		return nil, "", nil
	}

	purged, err := userManager.PurgeDeletedUsers(retentionDays)
	if err != nil {
		msg := fmt.Sprintf("\r\n\r\n|01Purge failed: %v|07\r\n", err)
		_ = terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		if pauseErr := e.loginPausePrompt(s, terminal, nodeNumber, outputMode, termWidth, termHeight); pauseErr != nil {
			if errors.Is(pauseErr, io.EOF) {
				return nil, "LOGOFF", io.EOF
			}
			return nil, "", pauseErr
		}
		return nil, "", err
	}

	// Log each purged account to admin activity log
	for _, p := range purged {
		logEntry := user.AdminActivityLog{
			AdminHandle:  currentUser.Handle,
			AdminID:      currentUser.ID,
			TargetUserID: p.ID,
			TargetHandle: p.Handle,
			Action:       "PURGE_USER",
			Notes:        fmt.Sprintf("Permanently purged after %d-day retention period", retentionDays),
		}
		_ = userManager.LogAdminActivity(logEntry)
	}

	result := fmt.Sprintf("\r\n\r\n|10Purged %d user account(s).|07\r\n", len(purged))
	_ = terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(result)), outputMode)

	if pauseErr := e.loginPausePrompt(s, terminal, nodeNumber, outputMode, termWidth, termHeight); pauseErr != nil {
		if errors.Is(pauseErr, io.EOF) {
			return nil, "LOGOFF", io.EOF
		}
		return nil, "", pauseErr
	}
	return nil, "", nil
}
