package menu

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"golang.org/x/crypto/bcrypt"
)

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

// userEditorConfig parameterizes runUserEditor for its two entry points: the
// full user editor (runAdminListUsers) and the pending-validation queue
// (runValidateUser). Those two flows were previously ~800 lines of near-identical
// duplicated code; pendingOnly captures every behavioral difference between them.
type userEditorConfig struct {
	title        string // header title line (pipe-coded)
	emptyMessage string // shown when no users match (pipe-coded, no trailing reset)
	logLabel     string // optional startup debug label ("" = no log line)
	pendingOnly  bool   // restrict to users awaiting validation + queue behavior
}

// runUserEditor implements the shared interactive user editor used by both the
// admin user browser and the pending-validation queue. See userEditorConfig.
func runUserEditor(c *cmdCtx, cfg userEditorConfig) (*user.User, string, error) {
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

	if cfg.logLabel != "" {
		slog.Debug("running command", "node", nodeNumber, "label", cfg.logLabel)
	}

	adminCursorHidden := e.hideCursorIfNeeded(terminal, outputMode, cursorHideContextDefault)
	if adminCursorHidden {
		defer e.showCursorIfHidden(terminal, outputMode, adminCursorHidden)
	}

	if currentUser == nil || userManager == nil {
		return nil, "", nil
	}
	sysOpACS := fmt.Sprintf("S%d", e.ServerCfg.SysOpLevel)
	if !checkACS(sysOpACS, currentUser, s, terminal, sessionStartTime) {
		_ = terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n|01Access denied.|07\r\n")), outputMode)
		time.Sleep(1 * time.Second)
		return nil, "", nil
	}

	loadEditorUsers := func() []*user.User {
		all := sortedUsersByID(userManager.GetAllUsers())
		if !cfg.pendingOnly {
			return all
		}
		pending := make([]*user.User, 0)
		for _, u := range all {
			if isPendingValidationUser(u) {
				pending = append(pending, u)
			}
		}
		return pending
	}

	users := loadEditorUsers()
	if len(users) == 0 {
		_ = terminalio.WriteProcessedBytes(terminal, []byte(ansi.ClearScreen()), outputMode)
		_ = terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n"+cfg.emptyMessage+"|07")), outputMode)
		if pauseErr := e.loginPausePrompt(s, terminal, nodeNumber, outputMode, termWidth, termHeight); pauseErr != nil {
			if errors.Is(pauseErr, io.EOF) {
				return nil, "LOGOFF", io.EOF
			}
			return nil, "", pauseErr
		}
		return nil, "", nil
	}

	ih := getSessionIH(s)
	selectedIndex := 0
	topIndex := 0
	if termHeight <= 0 {
		termHeight = 24
		if ptyReq, _, ok := s.Pty(); ok && ptyReq.Window.Height > 0 {
			termHeight = ptyReq.Window.Height
		}
	}
	pageSize := termHeight - 14 // Reduced by 1 to account for header row
	if pageSize < 3 {
		pageSize = 3
	}
	if pageSize > 12 {
		pageSize = 12
	}

	titleRow := 1
	sepTopRow := 2
	headerRow := 3    // Column header labels
	listStartRow := 4 // First user row (after header)
	sepMidRow := listStartRow + pageSize
	detailStartRow := sepMidRow + 1
	statusRow := termHeight - 1
	actionRow := termHeight

	writeAt := func(row, col int, text string) error {
		cmd := fmt.Sprintf("\x1b[%d;%dH\x1b[2K%s", row, col, text)
		return terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(cmd)), outputMode)
	}

	clearRow := func(row int) error {
		cmd := fmt.Sprintf("\x1b[%d;1H\x1b[2K", row)
		return terminalio.WriteProcessedBytes(terminal, []byte(cmd), outputMode)
	}

	renderHeader := func() error {
		if err := terminalio.WriteProcessedBytes(terminal, []byte(ansi.ClearScreen()), outputMode); err != nil {
			return err
		}
		if err := writeAt(titleRow, 1, cfg.title); err != nil {
			return err
		}
		if err := clearRow(sepTopRow); err != nil {
			return err
		}
		// Render column header - aligned with data columns
		// Format: prefix(2) handle(22) space(3) date(10) space(3) ID:(3+4) space(3) L:(2+3) space(2) status(2)
		headerText := fmt.Sprintf("|08  %-22s   %-10s   %-7s   %-5s|07", "Handle", "Created", "ID", "Level")
		if err := writeAt(headerRow, 1, headerText); err != nil {
			return err
		}
		if err := clearRow(sepMidRow); err != nil {
			return err
		}
		for r := detailStartRow; r <= statusRow; r++ {
			if err := clearRow(r); err != nil {
				return err
			}
		}
		return nil
	}

	// Calculate visual display width (excluding pipe codes)
	visualWidth := func(s string) int {
		width := 0
		i := 0
		for i < len(s) {
			if s[i] == '|' && i+2 < len(s) && s[i+1] >= '0' && s[i+1] <= '9' && s[i+2] >= '0' && s[i+2] <= '9' {
				i += 3 // Skip pipe code
			} else {
				width++
				i++
			}
		}
		return width
	}

	pendingChanges := make(map[string]interface{})
	// Track original UpdatedAt timestamps for optimistic locking (indexed by user ID)
	originalTimestamps := make(map[int]time.Time)
	for _, u := range users {
		if u != nil {
			originalTimestamps[u.ID] = u.UpdatedAt
		}
	}

	renderActionBar := func() error {
		var barText string
		if len(pendingChanges) > 0 {
			barText = "|08[|15S|08] |14Save Changes  |08[|15X|08] |14Abort  |08[|15Q|08] |14Quit|07"
		} else {
			sel := users[selectedIndex]
			// Dynamic labels based on user state
			validateLabel := "Validate"
			validateColor := "|10" // Green for validate
			if sel.Validated {
				validateLabel = "Un-Validate"
				validateColor = "|11" // Yellow for un-validate
			}
			banLabel := "Ban"
			banColor := "|12" // Red for ban
			if sel.AccessLevel == 0 && !sel.Validated {
				banLabel = "Un-Ban"
				banColor = "|10" // Green for un-ban
			}
			deleteLabel := "Delete"
			deleteColor := "|12" // Red for delete
			if sel.DeletedUser {
				deleteLabel = "Un-Delete"
				deleteColor = "|10" // Green for un-delete
			}
			barText = fmt.Sprintf("|08[|15G|08] %s%s |08[|15I|08] |14Info |08[|15P|08] |14Passwd |08[|150|08] %s%s |08[|159|08] %s%s |08[|15Q|08] |11Quit|07", validateColor, validateLabel, banColor, banLabel, deleteColor, deleteLabel)
		}
		if err := clearRow(actionRow); err != nil {
			return err
		}
		// Center the action bar
		textWidth := visualWidth(barText)
		padding := (80 - textWidth) / 2
		if padding < 1 {
			padding = 1
		}
		centeredText := strings.Repeat(" ", padding) + barText
		return writeAt(actionRow, 1, centeredText)
	}

	renderList := func() error {
		endIndex := topIndex + pageSize
		if endIndex > len(users) {
			endIndex = len(users)
		}
		row := listStartRow
		for idx := topIndex; idx < endIndex; idx++ {
			u := users[idx]
			status := "OK"
			if u.DeletedUser {
				status = "DEL"
			} else if !u.Validated {
				status = "NV"
			}
			// Check if user is currently online (actual session tracking)
			onlineIndicator := " "
			if userManager.IsUserOnline(u.ID) {
				onlineIndicator = "*" // Asterisk indicates user is currently online
			}
			prefix := "  "
			lineStart := ""
			lineEnd := ""
			if idx == selectedIndex {
				prefix = "» "
				lineStart = "\x1b[46;30m" // Dark cyan background, black foreground
				lineEnd = "\x1b[0m"       // Reset colors
			}
			line := fmt.Sprintf("%s%s%-22s   %-10s   ID:%-4d   L:%-3d  %-2s%s%s", lineStart, prefix, adminTruncate(u.Handle, 22), adminDate(u.CreatedAt), u.ID, u.AccessLevel, status, onlineIndicator, lineEnd)
			if err := writeAt(row, 1, line); err != nil {
				return err
			}
			row++
		}
		for ; row < listStartRow+pageSize; row++ {
			if err := clearRow(row); err != nil {
				return err
			}
		}
		return nil
	}

	renderDetails := func(message string) error {
		sel := users[selectedIndex]

		getFieldValue := func(fieldName string, originalValue string) string {
			if val, ok := pendingChanges[fieldName]; ok {
				return fmt.Sprintf("|14*|03%s|07", adminTruncate(val.(string), 23))
			}
			return adminTruncate(originalValue, 24)
		}

		getIntFieldValue := func(fieldName string, originalValue int) string {
			if val, ok := pendingChanges[fieldName]; ok {
				return fmt.Sprintf("|14*|03%d|07", val.(int))
			}
			return fmt.Sprintf("%d", originalValue)
		}

		getBoolFieldValue := func(fieldName string, originalValue bool) string {
			if val, ok := pendingChanges[fieldName]; ok {
				return fmt.Sprintf("|14*|03%t|07", val.(bool))
			}
			return fmt.Sprintf("%t", originalValue)
		}

		// Calculate visual display width (excluding pipe codes)
		visualWidth := func(s string) int {
			width := 0
			i := 0
			for i < len(s) {
				if s[i] == '|' && i+2 < len(s) && s[i+1] >= '0' && s[i+1] <= '9' && s[i+2] >= '0' && s[i+2] <= '9' {
					i += 3 // Skip pipe code
				} else {
					width++
					i++
				}
			}
			return width
		}

		lineTwoCol := func(leftLabel, leftValue, rightLabel, rightValue string) string {
			// Calculate padding needed to align second column at position 45
			leftLabelWidth := visualWidth(leftLabel)
			leftValueWidth := visualWidth(leftValue)
			totalLeft := leftLabelWidth + 2 + leftValueWidth // label + ": " + value
			paddingNeeded := 45 - totalLeft
			if paddingNeeded < 2 {
				paddingNeeded = 2 // Minimum 2 spaces
			}
			padding := ""
			for i := 0; i < paddingNeeded; i++ {
				padding += " "
			}
			return fmt.Sprintf("%s|08: |03%s%s%s|08: |03%s|07", leftLabel, leftValue, padding, rightLabel, rightValue)
		}

		deletedStatus := "No"
		if sel.DeletedUser {
			deletedStatus = "Yes"
		}

		// Draw separator line above edit area
		separator := "|08" + strings.Repeat("-", 79) + "|07"
		if err := writeAt(sepMidRow, 1, separator); err != nil {
			return err
		}

		lines := []string{
			// Editable fields (A-G) in LEFT column, read-only stats in RIGHT column
			lineTwoCol("|08[|14A|08]|11 Handle", getFieldValue("handle", sel.Handle), "|11Calls", fmt.Sprintf("%d", sel.TimesCalled)),
			lineTwoCol("|08[|14B|08]|11 Real Name", getFieldValue("realname", sel.RealName), "|11Uploads", fmt.Sprintf("%d", sel.NumUploads)),
			lineTwoCol("|08[|14C|08]|11 Group/Loc", getFieldValue("grouploc", sel.GroupLocation), "|11FilePoints", fmt.Sprintf("%d", sel.FilePoints)),
			lineTwoCol("|08[|14D|08]|11 Note", getFieldValue("note", sel.PrivateNote), "|11Posts", fmt.Sprintf("%d", sel.MessagesPosted)),
			lineTwoCol("|08[|14E|08]|11 Flags", getFieldValue("flags", sel.Flags), "|11Created", adminTime(sel.CreatedAt)),
			lineTwoCol("|08[|14F|08]|11 Level", getIntFieldValue("level", sel.AccessLevel), "|11Last Login", adminTime(sel.LastLogin)),
			lineTwoCol("|08[|14G|08]|11 Validated", getBoolFieldValue("validated", sel.Validated), "|11Deleted", deletedStatus),
		}
		for i, line := range lines {
			if err := writeAt(detailStartRow+i, 1, line); err != nil {
				return err
			}
		}
		if message != "" {
			if err := writeAt(statusRow, 1, message); err != nil {
				return err
			}
		} else {
			if err := clearRow(statusRow); err != nil {
				return err
			}
		}
		return renderActionBar()
	}

	readFieldInput := func(fieldLabel string, currentValue string, maxLen int, mask bool) (string, error) {
		if adminCursorHidden {
			_ = terminalio.WriteProcessedBytes(terminal, []byte("\x1b[?25h"), outputMode)
			defer terminalio.WriteProcessedBytes(terminal, []byte("\x1b[?25l"), outputMode)
		}

		prompt := fmt.Sprintf("|15%s:|07 ", fieldLabel)
		if err := writeAt(statusRow, 1, prompt); err != nil {
			return "", err
		}

		// Position cursor after prompt
		cursorPos := len(fieldLabel) + 3
		cmd := fmt.Sprintf("\x1b[%d;%dH", statusRow, cursorPos)
		if err := terminalio.WriteProcessedBytes(terminal, []byte(cmd), outputMode); err != nil {
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
		if err := terminalio.WriteProcessedBytes(terminal, []byte(display()), outputMode); err != nil {
			return "", err
		}

		for {
			key, readErr := ih.ReadKey()
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
					if err := writeAt(statusRow, 1, prompt+display()+"  "); err != nil {
						return "", err
					}
					cmd := fmt.Sprintf("\x1b[%d;%dH", statusRow, cursorPos+cursorIdx)
					if err := terminalio.WriteProcessedBytes(terminal, []byte(cmd), outputMode); err != nil {
						return "", err
					}
				}
			default:
				if key >= 32 && key < 127 && len(input) < maxLen {
					r := rune(key)
					input = append(input[:cursorIdx], append([]rune{r}, input[cursorIdx:]...)...)
					cursorIdx++
					if err := writeAt(statusRow, 1, prompt+display()); err != nil {
						return "", err
					}
					cmd := fmt.Sprintf("\x1b[%d;%dH", statusRow, cursorPos+cursorIdx)
					if err := terminalio.WriteProcessedBytes(terminal, []byte(cmd), outputMode); err != nil {
						return "", err
					}
				}
			}
		}
	}

	moveUp := func() {
		if selectedIndex > 0 {
			selectedIndex--
			if selectedIndex < topIndex {
				topIndex = selectedIndex
			}
		}
	}
	moveDown := func() {
		if selectedIndex < len(users)-1 {
			selectedIndex++
			if selectedIndex >= topIndex+pageSize {
				topIndex = selectedIndex - pageSize + 1
			}
		}
	}

	if err := renderHeader(); err != nil {
		return nil, "", err
	}
	if err := renderList(); err != nil {
		return nil, "", err
	}
	if err := renderActionBar(); err != nil {
		return nil, "", err
	}
	if err := renderDetails(""); err != nil {
		return nil, "", err
	}

	for {
		key, err := ih.ReadKey()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil, "LOGOFF", io.EOF
			}
			return nil, "", err
		}

		refresh := false
		statusMessage := ""

		switch key {
		case 'k', 'K', 'w', 'W':
			if len(pendingChanges) == 0 {
				moveUp()
				refresh = true
			}
		case 'j', 'J':
			if len(pendingChanges) == 0 {
				moveDown()
				refresh = true
			}
		case 's', 'S':
			if len(pendingChanges) > 0 {
				target := users[selectedIndex]
				var saved bool
				statusMessage, saved = e.applyPendingUserChanges(userManager, currentUser, target, pendingChanges, originalTimestamps)
				if saved {
					pendingChanges = make(map[string]interface{})
					users = loadEditorUsers()
					if cfg.pendingOnly {
						// Validated users drop out of the queue: handle the now-empty
						// case and clamp the selection back into range.
						if len(users) == 0 {
							_ = terminalio.WriteProcessedBytes(terminal, []byte(ansi.ClearScreen()), outputMode)
							_ = terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n|10All users have been validated!|07")), outputMode)
							if pauseErr := e.loginPausePrompt(s, terminal, nodeNumber, outputMode, termWidth, termHeight); pauseErr != nil {
								if errors.Is(pauseErr, io.EOF) {
									return nil, "LOGOFF", io.EOF
								}
								return nil, "", pauseErr
							}
							return nil, "", nil
						}
						if selectedIndex >= len(users) {
							selectedIndex = len(users) - 1
						}
						if topIndex > selectedIndex {
							topIndex = selectedIndex
						}
						if err := renderHeader(); err != nil {
							return nil, "", err
						}
					}
				}
				refresh = true
			} else {
				moveDown()
				refresh = true
			}
		case 'q', 'Q':
			if len(pendingChanges) > 0 {
				statusMessage = "|11Unsaved changes! Press [S] to save or [X] to abort.|07"
			} else {
				return nil, "", nil
			}
		case 'x', 'X':
			if len(pendingChanges) > 0 {
				pendingChanges = make(map[string]interface{})
				statusMessage = "|11Changes discarded.|07"
				refresh = true
			}
		case 'a', 'A':
			sel := users[selectedIndex]
			if newVal, editErr := readFieldInput("Handle", sel.Handle, 30, false); editErr == nil {
				trimmedHandle := strings.TrimSpace(newVal)
				if trimmedHandle != sel.Handle {
					pendingChanges["handle"] = trimmedHandle
					statusMessage = "|10Field marked for update.|07"
				} else {
					delete(pendingChanges, "handle")
					statusMessage = "|08No change.|07"
				}
				refresh = true
			} else {
				if editErr.Error() != "cancelled" {
					statusMessage = fmt.Sprintf("|01Error: %v|07", editErr)
				}
				refresh = true
			}
		case 'b', 'B':
			// Edit Real Name field
			sel := users[selectedIndex]
			if newVal, editErr := readFieldInput("Real Name", sel.RealName, 50, false); editErr == nil {
				if newVal != sel.RealName {
					pendingChanges["realname"] = newVal
					statusMessage = "|10Field marked for update.|07"
				} else {
					delete(pendingChanges, "realname")
					statusMessage = "|08No change.|07"
				}
				refresh = true
			} else {
				if editErr.Error() != "cancelled" {
					statusMessage = fmt.Sprintf("|01Error: %v|07", editErr)
				}
				refresh = true
			}
		case 'c', 'C':
			sel := users[selectedIndex]
			if newVal, editErr := readFieldInput("Group/Location", sel.GroupLocation, 30, false); editErr == nil {
				if newVal != sel.GroupLocation {
					pendingChanges["grouploc"] = newVal
					statusMessage = "|10Field marked for update.|07"
				} else {
					delete(pendingChanges, "grouploc")
					statusMessage = "|08No change.|07"
				}
				refresh = true
			} else {
				if editErr.Error() != "cancelled" {
					statusMessage = fmt.Sprintf("|01Error: %v|07", editErr)
				}
				refresh = true
			}
		case 'd', 'D':
			sel := users[selectedIndex]
			if newVal, editErr := readFieldInput("Note", sel.PrivateNote, 50, false); editErr == nil {
				if newVal != sel.PrivateNote {
					pendingChanges["note"] = newVal
					statusMessage = "|10Field marked for update.|07"
				} else {
					delete(pendingChanges, "note")
					statusMessage = "|08No change.|07"
				}
				refresh = true
			} else {
				if editErr.Error() != "cancelled" {
					statusMessage = fmt.Sprintf("|01Error: %v|07", editErr)
				}
				refresh = true
			}
		case 'e', 'E':
			sel := users[selectedIndex]
			if newVal, editErr := readFieldInput("Flags", sel.Flags, 20, false); editErr == nil {
				if newVal != sel.Flags {
					pendingChanges["flags"] = newVal
					statusMessage = "|10Field marked for update.|07"
				} else {
					delete(pendingChanges, "flags")
					statusMessage = "|08No change.|07"
				}
				refresh = true
			} else {
				if editErr.Error() != "cancelled" {
					statusMessage = fmt.Sprintf("|01Error: %v|07", editErr)
				}
				refresh = true
			}
		case 'f', 'F':
			sel := users[selectedIndex]
			levelStr := fmt.Sprintf("%d", sel.AccessLevel)
			if newVal, editErr := readFieldInput("Level", levelStr, 3, false); editErr == nil {
				if level, parseErr := strconv.Atoi(newVal); parseErr == nil {
					// Protect User #1 from level reduction
					if sel.ID == 1 && level < e.ServerCfg.SysOpLevel {
						statusMessage = "|01Cannot lower User #1 below SysOp level!|07"
						refresh = true
					} else if level != sel.AccessLevel {
						pendingChanges["level"] = level
						statusMessage = "|10Field marked for update.|07"
						refresh = true
					} else {
						delete(pendingChanges, "level")
						statusMessage = "|08No change.|07"
						refresh = true
					}
				} else {
					statusMessage = "|01Invalid number.|07"
					refresh = true
				}
			} else {
				if editErr.Error() != "cancelled" {
					statusMessage = fmt.Sprintf("|01Error: %v|07", editErr)
				}
				refresh = true
			}
		case 'g', 'G':
			// Toggle validated status
			sel := users[selectedIndex]
			if sel.ID == 1 && sel.Validated {
				// Don't allow unvalidating User #1
				statusMessage = "|01Cannot unvalidate User #1!|07"
				refresh = true
			} else {
				newValidated := !sel.Validated
				if newValidated != sel.Validated {
					pendingChanges["validated"] = newValidated
					if newValidated {
						statusMessage = "|10Validated status marked for update.|07"
					} else {
						statusMessage = "|11Unvalidated status marked for update.|07"
					}
				} else {
					delete(pendingChanges, "validated")
					statusMessage = "|08No change.|07"
				}
				refresh = true
			}
		case 'p', 'P':
			// Change password
			if newPassword, editErr := readFieldInput("New Password", "", 50, true); editErr == nil {
				if newPassword != "" {
					pendingChanges["password"] = newPassword
					statusMessage = "|10Password marked for update.|07"
				} else {
					delete(pendingChanges, "password")
					statusMessage = "|08Password change cancelled.|07"
				}
				refresh = true
			} else {
				if editErr.Error() != "cancelled" {
					statusMessage = fmt.Sprintf("|01Error: %v|07", editErr)
				}
				refresh = true
			}
		case '0':
			// Toggle ban user (sets level 0, unvalidated) or unban (restore to regular level)
			sel := users[selectedIndex]
			if sel.ID == 1 {
				statusMessage = "|01Cannot ban User #1!|07"
			} else {
				// Check if user is currently banned
				isBanned := sel.AccessLevel == 0 && !sel.Validated
				if isBanned {
					// Unban: restore to regular user level and validate
					pendingChanges["validated"] = true
					pendingChanges["level"] = e.ServerCfg.RegularUserLevel
					statusMessage = fmt.Sprintf("|10Un-ban marked for update (level %d, validated).|07", e.ServerCfg.RegularUserLevel)
				} else {
					// Ban: set level 0 and unvalidated
					pendingChanges["validated"] = false
					pendingChanges["level"] = 0
					statusMessage = "|01Ban marked for update (level 0, unvalidated).|07"
				}
			}
			refresh = true
		case '9':
			// Toggle delete user (soft delete)
			sel := users[selectedIndex]
			if sel.ID == 1 {
				statusMessage = "|01Cannot delete User #1!|07"
			} else {
				newDeleted := !sel.DeletedUser
				if newDeleted != sel.DeletedUser {
					pendingChanges["deleted"] = newDeleted
					if newDeleted {
						statusMessage = "|01Delete marked for update (soft delete).|07"
					} else {
						statusMessage = "|10Undelete marked for update (restore user).|07"
					}
				} else {
					delete(pendingChanges, "deleted")
					statusMessage = "|08No change.|07"
				}
			}
			refresh = true
		case 'i', 'I':
			// View selected user's infoforms - interactive menu
			if len(pendingChanges) == 0 {
				sel := users[selectedIndex]
				infoformsMu.Lock()
				ifCfg, ifErr := loadInfoFormConfig(e.RootConfigPath)
				infoformsMu.Unlock()

				if ifErr != nil {
					_ = terminalio.WriteProcessedBytes(terminal, []byte(ansi.ClearScreen()), outputMode)
					wv(terminal, "\r\n|04Error loading infoforms config.\r\n", outputMode)
					e.holdScreen(s, terminal, outputMode, termWidth, termHeight)
				} else {
					_ = browseInfoForms(e, s, terminal, outputMode, sel, ifCfg, termWidth, termHeight)
				}
				// Restore full screen layout after infoform viewer cleared the screen
				if err := renderHeader(); err != nil {
					return nil, "", err
				}
				refresh = true
			}
		case '\r', '\n':
			// Enter/Return pressed - do nothing (removed help text display)
		case editor.KeyArrowUp:
			// Block navigation while edits are staged (matches j/k handling) so
			// a subsequent Save can't apply pendingChanges to a different user.
			if len(pendingChanges) == 0 {
				moveUp()
				refresh = true
			}
		case editor.KeyArrowDown:
			if len(pendingChanges) == 0 {
				moveDown()
				refresh = true
			}
		case editor.KeyEsc:
			if cfg.pendingOnly && len(pendingChanges) > 0 {
				statusMessage = "|11Unsaved changes! Press [S] to save or [X] to abort.|07"
			} else {
				return nil, "", nil
			}
		}

		if refresh {
			if err := renderList(); err != nil {
				return nil, "", err
			}
			if err := renderDetails(statusMessage); err != nil {
				return nil, "", err
			}
		} else if !cfg.pendingOnly && statusMessage != "" {
			if err := renderDetails(statusMessage); err != nil {
				return nil, "", err
			}
		}
	}
}
