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
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"golang.org/x/crypto/bcrypt"
)

// readFieldInput prompts on the status row for a single-line text value,
// pre-filled with currentValue, and runs a minimal line editor (backspace,
// printable insertion, Enter to accept, Esc to cancel) until the caller
// presses Enter or Esc. mask renders the buffer as asterisks (for password
// entry) without affecting the returned value.
func (st *userEditorState) readFieldInput(fieldLabel string, currentValue string, maxLen int, mask bool) (string, error) {
	if st.adminCursorHidden {
		_ = terminalio.WriteProcessedBytes(st.terminal, []byte("\x1b[?25h"), st.outputMode)
		defer terminalio.WriteProcessedBytes(st.terminal, []byte("\x1b[?25l"), st.outputMode)
	}

	prompt := fmt.Sprintf("|15%s:|07 ", fieldLabel)
	if err := st.writeAt(st.layout.statusRow, 1, prompt); err != nil {
		return "", err
	}

	// Position cursor after prompt
	cursorPos := len(fieldLabel) + 3
	cmd := fmt.Sprintf("\x1b[%d;%dH", st.layout.statusRow, cursorPos)
	if err := terminalio.WriteProcessedBytes(st.terminal, []byte(cmd), st.outputMode); err != nil {
		return "", err
	}

	input := []rune(currentValue)
	cursorIdx := len(input)

	// display renders the editable buffer, masking it for secret fields.
	display := func() string {
		if mask {
			return strings.Repeat("*", len(input))
		}
		return string(input)
	}

	// Show current value
	if err := terminalio.WriteProcessedBytes(st.terminal, []byte(display()), st.outputMode); err != nil {
		return "", err
	}

	for {
		key, readErr := st.ih.ReadKey()
		if readErr != nil {
			return "", readErr
		}

		switch key {
		case int('\r'), int('\n'):
			return string(input), nil
		case editor.KeyEsc:
			return "", fmt.Errorf("cancelled")
		case editor.KeyBackspace, editor.KeyDelete: // Backspace / DEL
			if cursorIdx > 0 {
				input = append(input[:cursorIdx-1], input[cursorIdx:]...)
				cursorIdx--
				if err := st.writeAt(st.layout.statusRow, 1, prompt+display()+"  "); err != nil {
					return "", err
				}
				cmd := fmt.Sprintf("\x1b[%d;%dH", st.layout.statusRow, cursorPos+cursorIdx)
				if err := terminalio.WriteProcessedBytes(st.terminal, []byte(cmd), st.outputMode); err != nil {
					return "", err
				}
			}
		default:
			if key >= 32 && key < 127 && len(input) < maxLen {
				r := rune(key)
				input = append(input[:cursorIdx], append([]rune{r}, input[cursorIdx:]...)...)
				cursorIdx++
				if err := st.writeAt(st.layout.statusRow, 1, prompt+display()); err != nil {
					return "", err
				}
				cmd := fmt.Sprintf("\x1b[%d;%dH", st.layout.statusRow, cursorPos+cursorIdx)
				if err := terminalio.WriteProcessedBytes(st.terminal, []byte(cmd), st.outputMode); err != nil {
					return "", err
				}
			}
		}
	}
}

// stageFieldEdit reads a new value for a text field via readFieldInput and
// stages the change in st.pendingChanges (keyed by fieldKey) when it differs
// from orig, clearing any previously staged change when it matches. It
// always sets st.statusMessage. A cancelled edit (Esc) leaves pendingChanges
// untouched and the status message blank; other read errors are surfaced as
// an error status message. This is the shared body of the six near-identical
// plain-text field cases ('a'-'e' and the Real Name case in particular);
// cases with extra validation or formatting (Handle's whitespace trim, Level's
// numeric parsing) keep that deviation at their call site instead of being
// forced through this helper.
func (st *userEditorState) stageFieldEdit(fieldKey, label, orig string, maxLen int) {
	newVal, editErr := st.readFieldInput(label, orig, maxLen, false)
	if editErr != nil {
		if editErr.Error() != "cancelled" {
			st.statusMessage = fmt.Sprintf("|01Error: %v|07", editErr)
		}
		return
	}
	if newVal != orig {
		st.pendingChanges[fieldKey] = newVal
		st.statusMessage = "|10Field marked for update.|07"
	} else {
		delete(st.pendingChanges, fieldKey)
		st.statusMessage = "|08No change.|07"
	}
}

// postSaveReload reloads the user list after a successful save. For the
// pending-validation queue it also handles the queue-emptied terminal case
// (the "all users validated" screen and pause prompt) and clamps the
// selection back into range for the next render. exit reports that
// runUserEditor should return immediately with (nil, msg, err) — either
// because the validation queue emptied out, or because the pause-prompt read
// failed; the parent loop performs the actual return.
func (st *userEditorState) postSaveReload(termWidth, termHeight int) (exit bool, msg string, err error) {
	st.users = st.loadEditorUsers()
	if !st.cfg.pendingOnly {
		return false, "", nil
	}
	// Validated users drop out of the queue: handle the now-empty case and
	// clamp the selection back into range.
	if len(st.users) == 0 {
		_ = terminalio.WriteProcessedBytes(st.terminal, []byte(ansi.ClearScreen()), st.outputMode)
		_ = terminalio.WriteProcessedBytes(st.terminal, ansi.ReplacePipeCodes([]byte("\r\n|10All users have been validated!|07")), st.outputMode)
		if pauseErr := st.e.loginPausePrompt(st.s, st.terminal, st.nodeNumber, st.outputMode, termWidth, termHeight); pauseErr != nil {
			if errors.Is(pauseErr, io.EOF) {
				return true, "LOGOFF", io.EOF
			}
			return true, "", pauseErr
		}
		return true, "", nil
	}
	if st.selectedIndex >= len(st.users) {
		st.selectedIndex = len(st.users) - 1
	}
	if st.topIndex > st.selectedIndex {
		st.topIndex = st.selectedIndex
	}
	if renderErr := st.renderHeader(); renderErr != nil {
		return true, "", renderErr
	}
	return false, "", nil
}

// applyPendingUserChanges validates and persists the staged edits for target.
// It returns a status message and whether the save succeeded; saved == false
// means a validation failure or persistence error (the message explains why,
// and any User #1-protected change is dropped from pendingChanges). On success
// it stamps target.UpdatedAt, refreshes originalTimestamps, and writes the admin
// audit log. It performs no terminal I/O, so it is unit-testable in isolation.
func (e *MenuExecutor) applyPendingUserChanges(userManager *user.UserMgr, adminUser, target *user.User, pendingChanges map[string]interface{}, originalTimestamps map[int]time.Time) (statusMessage string, saved bool) {
	// Optimistic locking: verify the user has not changed since editing began.
	currentUserData, found := userManager.GetUserByID(target.ID)
	if !found {
		return "|01Failed to verify user data - user not found!|07", false
	}
	if !currentUserData.UpdatedAt.Equal(originalTimestamps[target.ID]) {
		return "|01User data changed by another admin! Please refresh (X) and try again.|07", false
	}

	// Protect User ID 1 from critical changes.
	if target.ID == 1 {
		if val, ok := pendingChanges["level"]; ok {
			if val.(int) < e.ServerCfg.SysOpLevel {
				delete(pendingChanges, "level")
				return "|01Cannot lower User #1 below SysOp level!|07", false
			}
		}
		if val, ok := pendingChanges["validated"]; ok {
			if !val.(bool) {
				delete(pendingChanges, "validated")
				return "|01Cannot unvalidate User #1!|07", false
			}
		}
		if val, ok := pendingChanges["deleted"]; ok {
			if val.(bool) {
				delete(pendingChanges, "deleted")
				return "|01Cannot delete User #1!|07", false
			}
		}
	}

	if val, ok := pendingChanges["handle"]; ok {
		normalizedHandle := strings.TrimSpace(val.(string))
		if normalizedHandle == "" {
			return "|01Handle cannot be blank.|07", false
		}
		target.Handle = normalizedHandle
	}
	if val, ok := pendingChanges["realname"]; ok {
		target.RealName = val.(string)
	}
	if val, ok := pendingChanges["grouploc"]; ok {
		target.GroupLocation = val.(string)
	}
	if val, ok := pendingChanges["note"]; ok {
		target.PrivateNote = val.(string)
	}
	if val, ok := pendingChanges["flags"]; ok {
		target.Flags = val.(string)
	}
	if val, ok := pendingChanges["level"]; ok {
		target.AccessLevel = val.(int)
	}
	if val, ok := pendingChanges["validated"]; ok {
		target.Validated = val.(bool)
		// When validating, upgrade to regular user level if below it.
		if target.Validated {
			cfg := e.GetServerConfig()
			desiredLevel := cfg.RegularUserLevel
			if desiredLevel <= 0 {
				desiredLevel = 10
			}
			if target.AccessLevel < desiredLevel {
				target.AccessLevel = desiredLevel
			}
		}
	}
	if val, ok := pendingChanges["deleted"]; ok {
		target.DeletedUser = val.(bool)
		if target.DeletedUser {
			now := time.Now()
			target.DeletedAt = &now
		} else {
			target.DeletedAt = nil
		}
	}
	if val, ok := pendingChanges["password"]; ok {
		newPassword := val.(string)
		hashedPassword, hashErr := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
		if hashErr != nil {
			return fmt.Sprintf("|01Failed to hash password: %v|07", hashErr), false
		}
		target.PasswordHash = string(hashedPassword)
	}

	// Update timestamp for optimistic locking.
	target.UpdatedAt = time.Now()

	if updateErr := userManager.UpdateUserByID(target); updateErr != nil {
		return fmt.Sprintf("|01Save failed: %v|07", updateErr), false
	}
	originalTimestamps[target.ID] = target.UpdatedAt

	// Log all admin changes for audit trail.
	for fieldName, newValue := range pendingChanges {
		oldValue := ""
		switch fieldName {
		case "handle":
			oldValue = currentUserData.Handle
		case "realname":
			oldValue = currentUserData.RealName
		case "grouploc":
			oldValue = currentUserData.GroupLocation
		case "note":
			oldValue = currentUserData.PrivateNote
		case "flags":
			oldValue = currentUserData.Flags
		case "level":
			oldValue = fmt.Sprintf("%d", currentUserData.AccessLevel)
		case "validated":
			oldValue = fmt.Sprintf("%t", currentUserData.Validated)
		case "deleted":
			oldValue = fmt.Sprintf("%t", currentUserData.DeletedUser)
		case "password":
			// Don't log actual password values for security.
			oldValue = "********"
			newValue = "********"
		}
		logEntry := user.AdminActivityLogEntry(
			adminUser.Handle,
			adminUser.ID,
			target.ID,
			target.Handle,
			fieldName,
			oldValue,
			fmt.Sprintf("%v", newValue),
		)
		if logErr := userManager.LogAdminActivity(logEntry); logErr != nil {
			// Don't fail the save, but make the audit gap observable.
			slog.Error("admin audit log write failed", "id", target.ID, "field", fieldName, "error", logErr)
		}
	}

	return fmt.Sprintf("|10Changes saved for %s.|07", target.Handle), true
}
