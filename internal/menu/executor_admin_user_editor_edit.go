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
	"github.com/ViSiON-3/vision-3-bbs/internal/perm"
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
	// The value is shown from column cursorPos to the row's last column but
	// one, scrolled sideways to keep the cursor in view, so a long value
	// never wraps onto the action row.
	cursorPos := len(fieldLabel) + 3
	width := st.width
	if width <= 0 {
		width = 80
	}
	room := max(width-cursorPos, 10)

	input := []rune(currentValue)
	cursorIdx := len(input)
	offset := 0

	// draw rewrites the status row: the visible part of the buffer, masked
	// for secret fields, padded to clear what was there, and the cursor.
	draw := func() error {
		if cursorIdx < offset {
			offset = cursorIdx
		}
		if cursorIdx-offset >= room {
			offset = cursorIdx - room + 1
		}
		end := min(len(input), offset+room)
		shown := string(input[offset:end])
		if mask {
			shown = strings.Repeat("*", end-offset)
		}
		shown += strings.Repeat(" ", room-(end-offset))
		if err := st.writeAt(st.layout.statusRow, 1, prompt+shown); err != nil {
			return err
		}
		cmd := fmt.Sprintf("\x1b[%d;%dH", st.layout.statusRow, cursorPos+cursorIdx-offset)
		return terminalio.WriteProcessedBytes(st.terminal, []byte(cmd), st.outputMode)
	}
	if err := draw(); err != nil {
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
				// Step back so the characters before the cursor show again.
				offset = max(0, min(offset, cursorIdx-room/2))
				if err := draw(); err != nil {
					return "", err
				}
			}
		default:
			if key >= 32 && key < 127 && len(input) < maxLen {
				r := rune(key)
				input = append(input[:cursorIdx], append([]rune{r}, input[cursorIdx:]...)...)
				cursorIdx++
				if err := draw(); err != nil {
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

// stageNameList stages an edit of a list of role or group names (field,
// shown as label), checked as typed against configs/roles.json with parse,
// so a name that would never match isn't saved unnoticed.
func (st *userEditorState) stageNameList(field, label string, stored []string, parse func(string, *perm.Roles) ([]string, error)) {
	orig := perm.FormatRoleList(stored)
	cur := orig
	if staged, ok := st.pendingChanges[field].(string); ok {
		cur = staged
	}
	newVal, editErr := st.readFieldInput(label, cur, 60, false)
	if editErr != nil {
		if editErr.Error() != "cancelled" {
			st.statusMessage = fmt.Sprintf("|01Error: %v|07", editErr)
		}
		return
	}
	names, parseErr := parse(newVal, perm.Current())
	switch formatted := perm.FormatRoleList(names); {
	case parseErr != nil:
		st.statusMessage = "|01" + parseErr.Error() + "|07"
	case formatted != orig:
		st.pendingChanges[field] = formatted
		st.statusMessage = "|10Field marked for update.|07"
	default:
		delete(st.pendingChanges, field)
		st.statusMessage = "|08No change.|07"
	}
}

// stageRoles stages an edit of the selected user's admin roles, typed as
// perm.FormatRoleGrants writes them (msgops, validator=2027-01-01) with
// times in the board's zone, checked as typed: an undefined role, or a new
// expiry that has already passed, is refused.
func (st *userEditorState) stageRoles() {
	sel := st.users[st.selectedIndex]
	loc := boardLocation()
	orig := perm.FormatRoleGrants(sel, loc)
	cur := orig
	if staged, ok := st.pendingChanges["roles"].(string); ok {
		cur = staged
	}
	newVal, editErr := st.readFieldInput("Roles", cur, 200, false)
	if editErr != nil {
		if editErr.Error() != "cancelled" {
			st.statusMessage = fmt.Sprintf("|01Error: %v|07", editErr)
		}
		return
	}
	roles, expiring, parseErr := perm.ParseRoleGrants(newVal, perm.Current(), loc, time.Now(), sel)
	next := &user.User{Roles: roles, ExpiringRoles: expiring}
	switch {
	case parseErr != nil:
		st.statusMessage = "|01" + parseErr.Error() + "|07"
	case !perm.SameRoles(next, sel):
		st.pendingChanges["roles"] = perm.FormatRoleGrants(next, loc)
		st.statusMessage = "|10Field marked for update.|07"
	default:
		delete(st.pendingChanges, "roles")
		st.statusMessage = "|08No change.|07"
	}
}

// stageNamedFlags stages an edit of the selected user's named flags, typed
// as perm.FormatFlagList writes them (beta, donor=2027-01-01) with times in
// the board's zone, and checked as typed: an undeclared flag, or a new
// expiry that has already passed, is refused. Leaving a flag out revokes it.
func (st *userEditorState) stageNamedFlags() {
	sel := st.users[st.selectedIndex]
	loc := boardLocation()
	orig := perm.FormatFlagList(sel, loc)
	cur := orig
	if staged, ok := st.pendingChanges["namedflags"].(string); ok {
		cur = staged
	}
	newVal, editErr := st.readFieldInput("Named Flags", cur, 200, false)
	if editErr != nil {
		if editErr.Error() != "cancelled" {
			st.statusMessage = fmt.Sprintf("|01Error: %v|07", editErr)
		}
		return
	}
	flags, parseErr := perm.ParseFlagList(newVal, perm.Current(), loc, time.Now(), sel)
	switch {
	case parseErr != nil:
		st.statusMessage = "|01" + parseErr.Error() + "|07"
	case !perm.SameFlags(flags, sel.NamedFlags):
		st.pendingChanges["namedflags"] = perm.FormatFlagList(&user.User{NamedFlags: flags}, loc)
		st.statusMessage = "|10Field marked for update.|07"
	default:
		delete(st.pendingChanges, "namedflags")
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

	// Who may make these changes is decided here, on the record as stored,
	// whatever screen staged them: a key the validator's view hides can't
	// reach a field by another route, and a permission removed while the
	// editor was open stops this save.
	via, denied := e.authorizeUserChanges(userManager, adminUser, currentUserData, pendingChanges)
	if denied != "" {
		return denied, false
	}

	// A refused or failed save leaves target as it was. Otherwise a check
	// that fails halfway (an undefined role, say) leaves the fields before
	// it changed on the session's copy, and the next save writes them
	// without an audit entry.
	before := *target
	defer func() {
		if !saved {
			*target = before
		}
	}()

	// Protect User ID 1 from critical changes.
	if target.ID == 1 {
		if val, ok := pendingChanges["level"]; ok {
			if val.(int) < e.GetServerConfig().SysOpLevel {
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
		if err := user.ValidateHandleEdit(target.Handle, normalizedHandle); err != nil {
			return "|01" + err.Error() + "|07", false
		}
		target.Handle = normalizedHandle
	}
	if val, ok := pendingChanges["realname"]; ok {
		// Validated like the handle above: a blank real name silently turns off
		// real_name_only for this user in every area that sets it.
		normalizedRealName := strings.TrimSpace(val.(string))
		if err := user.ValidateRealNameEdit(target.RealName, normalizedRealName); err != nil {
			return "|01" + err.Error() + "|07", false
		}
		target.RealName = normalizedRealName
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
	if val, ok := pendingChanges["roles"]; ok {
		// Checked again here: roles.json may have changed since the edit was
		// staged.
		roles, expiring, err := perm.ParseRoleGrants(val.(string), perm.Current(), boardLocation(), time.Now(), target)
		if err != nil {
			return "|01" + err.Error() + "|07", false
		}
		target.Roles, target.ExpiringRoles = roles, expiring
	}
	if val, ok := pendingChanges["groups"]; ok {
		names, err := perm.ParseGroupList(val.(string), perm.Current())
		if err != nil {
			return "|01" + err.Error() + "|07", false
		}
		target.Groups = names
	}
	if val, ok := pendingChanges["namedflags"]; ok {
		// Checked again against the stored record and the clock: an expiry
		// staged a moment ago may have passed since.
		flags, err := perm.ParseFlagList(val.(string), perm.Current(), boardLocation(), time.Now(), target)
		if err != nil {
			return "|01" + err.Error() + "|07", false
		}
		target.NamedFlags = flags
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

	// The network chat bar lives with the consent fields, which an ordinary
	// save keeps as stored, so it goes in the same save by its own path:
	// staged with a deletion, both land or neither does.
	var updateErr error
	if val, ok := pendingChanges["chatBarred"]; ok {
		target.CentralChatBarred = val.(bool)
		updateErr = userManager.UpdateUserByIDAndChatBar(target, val.(bool))
	} else {
		updateErr = userManager.UpdateUserByID(target)
	}
	if updateErr != nil {
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
		case "roles":
			oldValue = perm.FormatRoleGrants(currentUserData, boardLocation())
		case "groups":
			oldValue = perm.FormatRoleList(currentUserData.Groups)
		case "namedflags":
			oldValue = perm.FormatFlagList(currentUserData, boardLocation())
		case "level":
			oldValue = fmt.Sprintf("%d", currentUserData.AccessLevel)
		case "validated":
			oldValue = fmt.Sprintf("%t", currentUserData.Validated)
		case "deleted":
			oldValue = fmt.Sprintf("%t", currentUserData.DeletedUser)
		case "chatBarred":
			oldValue = fmt.Sprintf("%t", currentUserData.CentralChatBarred)
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
		// A change made below sysop level records the grant that allowed it.
		logEntry.Notes = via
		if logErr := userManager.LogAdminActivity(logEntry); logErr != nil {
			// Don't fail the save, but make the audit gap observable.
			slog.Error("admin audit log write failed", "id", target.ID, "field", fieldName, "error", logErr)
		}
	}

	return fmt.Sprintf("|10Changes saved for %s.|07", callerText(target.Handle)), true
}
