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
	"github.com/gliderlabs/ssh"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/term"
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

// userEditorLayout is the fixed screen geometry of the admin user editor,
// derived once from the (already-resolved) terminal height.
type userEditorLayout struct {
	pageSize       int
	titleRow       int
	sepTopRow      int
	headerRow      int
	listStartRow   int
	sepMidRow      int
	detailStartRow int
	statusRow      int
	actionRow      int
}

// computeUserEditorLayout derives the row layout and page size for the admin
// user editor from a resolved terminal height. Callers are responsible for
// substituting a default before calling this (e.g. when the terminal reports
// height <= 0).
func computeUserEditorLayout(termHeight int) userEditorLayout {
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

	return userEditorLayout{
		pageSize:       pageSize,
		titleRow:       titleRow,
		sepTopRow:      sepTopRow,
		headerRow:      headerRow,
		listStartRow:   listStartRow,
		sepMidRow:      sepMidRow,
		detailStartRow: detailStartRow,
		statusRow:      statusRow,
		actionRow:      actionRow,
	}
}

// visualPipeWidth returns the display width of s, treating |NN pipe color
// codes as zero-width.
func visualPipeWidth(s string) int {
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

// userEditorState holds the dependencies and mutable state for a single
// runUserEditor session. It replaces the closure-captured locals the loop
// body used to mutate directly; users and pendingChanges in particular are
// reassigned (not just mutated) inside the loop, which requires them to live
// as struct fields rather than closed-over locals.
type userEditorState struct {
	e           *MenuExecutor
	s           ssh.Session
	terminal    *term.Terminal
	ih          *editor.InputHandler
	userManager *user.UserMgr
	currentUser *user.User
	nodeNumber  int
	outputMode  ansi.OutputMode
	cfg         userEditorConfig
	layout      userEditorLayout

	users              []*user.User
	selectedIndex      int
	topIndex           int
	pendingChanges     map[string]interface{}
	originalTimestamps map[int]time.Time
	statusMessage      string
	adminCursorHidden  bool
}

// loadEditorUsers loads the full user list, filtered down to users awaiting
// validation when cfg.pendingOnly is set.
func (st *userEditorState) loadEditorUsers() []*user.User {
	all := sortedUsersByID(st.userManager.GetAllUsers())
	if !st.cfg.pendingOnly {
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

// moveUp moves the selection up one row, scrolling topIndex if needed.
func (st *userEditorState) moveUp() {
	if st.selectedIndex > 0 {
		st.selectedIndex--
		if st.selectedIndex < st.topIndex {
			st.topIndex = st.selectedIndex
		}
	}
}

// moveDown moves the selection down one row, scrolling topIndex if needed.
func (st *userEditorState) moveDown() {
	if st.selectedIndex < len(st.users)-1 {
		st.selectedIndex++
		if st.selectedIndex >= st.topIndex+st.layout.pageSize {
			st.topIndex = st.selectedIndex - st.layout.pageSize + 1
		}
	}
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

	st := &userEditorState{
		e:                 e,
		s:                 s,
		terminal:          terminal,
		ih:                getSessionIH(s),
		userManager:       userManager,
		currentUser:       currentUser,
		nodeNumber:        nodeNumber,
		outputMode:        outputMode,
		cfg:               cfg,
		adminCursorHidden: adminCursorHidden,
	}

	st.users = st.loadEditorUsers()
	if len(st.users) == 0 {
		_ = terminalio.WriteProcessedBytes(st.terminal, []byte(ansi.ClearScreen()), st.outputMode)
		_ = terminalio.WriteProcessedBytes(st.terminal, ansi.ReplacePipeCodes([]byte("\r\n"+st.cfg.emptyMessage+"|07")), st.outputMode)
		if pauseErr := e.loginPausePrompt(s, terminal, nodeNumber, outputMode, termWidth, termHeight); pauseErr != nil {
			if errors.Is(pauseErr, io.EOF) {
				return nil, "LOGOFF", io.EOF
			}
			return nil, "", pauseErr
		}
		return nil, "", nil
	}

	if termHeight <= 0 {
		termHeight = 24
		if ptyReq, _, ok := s.Pty(); ok && ptyReq.Window.Height > 0 {
			termHeight = ptyReq.Window.Height
		}
	}
	st.layout = computeUserEditorLayout(termHeight)

	writeAt := func(row, col int, text string) error {
		cmd := fmt.Sprintf("\x1b[%d;%dH\x1b[2K%s", row, col, text)
		return terminalio.WriteProcessedBytes(st.terminal, ansi.ReplacePipeCodes([]byte(cmd)), st.outputMode)
	}

	clearRow := func(row int) error {
		cmd := fmt.Sprintf("\x1b[%d;1H\x1b[2K", row)
		return terminalio.WriteProcessedBytes(st.terminal, []byte(cmd), st.outputMode)
	}

	renderHeader := func() error {
		if err := terminalio.WriteProcessedBytes(st.terminal, []byte(ansi.ClearScreen()), st.outputMode); err != nil {
			return err
		}
		if err := writeAt(st.layout.titleRow, 1, st.cfg.title); err != nil {
			return err
		}
		if err := clearRow(st.layout.sepTopRow); err != nil {
			return err
		}
		// Render column header - aligned with data columns
		// Format: prefix(2) handle(22) space(3) date(10) space(3) ID:(3+4) space(3) L:(2+3) space(2) status(2)
		headerText := fmt.Sprintf("|08  %-22s   %-10s   %-7s   %-5s|07", "Handle", "Created", "ID", "Level")
		if err := writeAt(st.layout.headerRow, 1, headerText); err != nil {
			return err
		}
		if err := clearRow(st.layout.sepMidRow); err != nil {
			return err
		}
		for r := st.layout.detailStartRow; r <= st.layout.statusRow; r++ {
			if err := clearRow(r); err != nil {
				return err
			}
		}
		return nil
	}

	st.pendingChanges = make(map[string]interface{})
	// Track original UpdatedAt timestamps for optimistic locking (indexed by user ID)
	st.originalTimestamps = make(map[int]time.Time)
	for _, u := range st.users {
		if u != nil {
			st.originalTimestamps[u.ID] = u.UpdatedAt
		}
	}

	renderActionBar := func() error {
		var barText string
		if len(st.pendingChanges) > 0 {
			barText = "|08[|15S|08] |14Save Changes  |08[|15X|08] |14Abort  |08[|15Q|08] |14Quit|07"
		} else {
			sel := st.users[st.selectedIndex]
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
		if err := clearRow(st.layout.actionRow); err != nil {
			return err
		}
		// Center the action bar
		textWidth := visualPipeWidth(barText)
		padding := (80 - textWidth) / 2
		if padding < 1 {
			padding = 1
		}
		centeredText := strings.Repeat(" ", padding) + barText
		return writeAt(st.layout.actionRow, 1, centeredText)
	}

	renderList := func() error {
		endIndex := st.topIndex + st.layout.pageSize
		if endIndex > len(st.users) {
			endIndex = len(st.users)
		}
		row := st.layout.listStartRow
		for idx := st.topIndex; idx < endIndex; idx++ {
			u := st.users[idx]
			status := "OK"
			if u.DeletedUser {
				status = "DEL"
			} else if !u.Validated {
				status = "NV"
			}
			// Check if user is currently online (actual session tracking)
			onlineIndicator := " "
			if st.userManager.IsUserOnline(u.ID) {
				onlineIndicator = "*" // Asterisk indicates user is currently online
			}
			prefix := "  "
			lineStart := ""
			lineEnd := ""
			if idx == st.selectedIndex {
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
		for ; row < st.layout.listStartRow+st.layout.pageSize; row++ {
			if err := clearRow(row); err != nil {
				return err
			}
		}
		return nil
	}

	renderDetails := func(message string) error {
		sel := st.users[st.selectedIndex]

		getFieldValue := func(fieldName string, originalValue string) string {
			if val, ok := st.pendingChanges[fieldName]; ok {
				return fmt.Sprintf("|14*|03%s|07", adminTruncate(val.(string), 23))
			}
			return adminTruncate(originalValue, 24)
		}

		getIntFieldValue := func(fieldName string, originalValue int) string {
			if val, ok := st.pendingChanges[fieldName]; ok {
				return fmt.Sprintf("|14*|03%d|07", val.(int))
			}
			return fmt.Sprintf("%d", originalValue)
		}

		getBoolFieldValue := func(fieldName string, originalValue bool) string {
			if val, ok := st.pendingChanges[fieldName]; ok {
				return fmt.Sprintf("|14*|03%t|07", val.(bool))
			}
			return fmt.Sprintf("%t", originalValue)
		}

		lineTwoCol := func(leftLabel, leftValue, rightLabel, rightValue string) string {
			// Calculate padding needed to align second column at position 45
			leftLabelWidth := visualPipeWidth(leftLabel)
			leftValueWidth := visualPipeWidth(leftValue)
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
		if err := writeAt(st.layout.sepMidRow, 1, separator); err != nil {
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
			if err := writeAt(st.layout.detailStartRow+i, 1, line); err != nil {
				return err
			}
		}
		if message != "" {
			if err := writeAt(st.layout.statusRow, 1, message); err != nil {
				return err
			}
		} else {
			if err := clearRow(st.layout.statusRow); err != nil {
				return err
			}
		}
		return renderActionBar()
	}

	readFieldInput := func(fieldLabel string, currentValue string, maxLen int, mask bool) (string, error) {
		if st.adminCursorHidden {
			_ = terminalio.WriteProcessedBytes(st.terminal, []byte("\x1b[?25h"), st.outputMode)
			defer terminalio.WriteProcessedBytes(st.terminal, []byte("\x1b[?25l"), st.outputMode)
		}

		prompt := fmt.Sprintf("|15%s:|07 ", fieldLabel)
		if err := writeAt(st.layout.statusRow, 1, prompt); err != nil {
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
					if err := writeAt(st.layout.statusRow, 1, prompt+display()+"  "); err != nil {
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
					if err := writeAt(st.layout.statusRow, 1, prompt+display()); err != nil {
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
		key, err := st.ih.ReadKey()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil, "LOGOFF", io.EOF
			}
			return nil, "", err
		}

		refresh := false
		st.statusMessage = ""

		switch key {
		case 'k', 'K', 'w', 'W':
			if len(st.pendingChanges) == 0 {
				st.moveUp()
				refresh = true
			}
		case 'j', 'J':
			if len(st.pendingChanges) == 0 {
				st.moveDown()
				refresh = true
			}
		case 's', 'S':
			if len(st.pendingChanges) > 0 {
				target := st.users[st.selectedIndex]
				var saved bool
				st.statusMessage, saved = st.e.applyPendingUserChanges(st.userManager, st.currentUser, target, st.pendingChanges, st.originalTimestamps)
				if saved {
					st.pendingChanges = make(map[string]interface{})
					st.users = st.loadEditorUsers()
					if st.cfg.pendingOnly {
						// Validated users drop out of the queue: handle the now-empty
						// case and clamp the selection back into range.
						if len(st.users) == 0 {
							_ = terminalio.WriteProcessedBytes(st.terminal, []byte(ansi.ClearScreen()), st.outputMode)
							_ = terminalio.WriteProcessedBytes(st.terminal, ansi.ReplacePipeCodes([]byte("\r\n|10All users have been validated!|07")), st.outputMode)
							if pauseErr := e.loginPausePrompt(s, terminal, nodeNumber, outputMode, termWidth, termHeight); pauseErr != nil {
								if errors.Is(pauseErr, io.EOF) {
									return nil, "LOGOFF", io.EOF
								}
								return nil, "", pauseErr
							}
							return nil, "", nil
						}
						if st.selectedIndex >= len(st.users) {
							st.selectedIndex = len(st.users) - 1
						}
						if st.topIndex > st.selectedIndex {
							st.topIndex = st.selectedIndex
						}
						if err := renderHeader(); err != nil {
							return nil, "", err
						}
					}
				}
				refresh = true
			} else {
				st.moveDown()
				refresh = true
			}
		case 'q', 'Q':
			if len(st.pendingChanges) > 0 {
				st.statusMessage = "|11Unsaved changes! Press [S] to save or [X] to abort.|07"
			} else {
				return nil, "", nil
			}
		case 'x', 'X':
			if len(st.pendingChanges) > 0 {
				st.pendingChanges = make(map[string]interface{})
				st.statusMessage = "|11Changes discarded.|07"
				refresh = true
			}
		case 'a', 'A':
			sel := st.users[st.selectedIndex]
			if newVal, editErr := readFieldInput("Handle", sel.Handle, 30, false); editErr == nil {
				trimmedHandle := strings.TrimSpace(newVal)
				if trimmedHandle != sel.Handle {
					st.pendingChanges["handle"] = trimmedHandle
					st.statusMessage = "|10Field marked for update.|07"
				} else {
					delete(st.pendingChanges, "handle")
					st.statusMessage = "|08No change.|07"
				}
				refresh = true
			} else {
				if editErr.Error() != "cancelled" {
					st.statusMessage = fmt.Sprintf("|01Error: %v|07", editErr)
				}
				refresh = true
			}
		case 'b', 'B':
			// Edit Real Name field
			sel := st.users[st.selectedIndex]
			if newVal, editErr := readFieldInput("Real Name", sel.RealName, 50, false); editErr == nil {
				if newVal != sel.RealName {
					st.pendingChanges["realname"] = newVal
					st.statusMessage = "|10Field marked for update.|07"
				} else {
					delete(st.pendingChanges, "realname")
					st.statusMessage = "|08No change.|07"
				}
				refresh = true
			} else {
				if editErr.Error() != "cancelled" {
					st.statusMessage = fmt.Sprintf("|01Error: %v|07", editErr)
				}
				refresh = true
			}
		case 'c', 'C':
			sel := st.users[st.selectedIndex]
			if newVal, editErr := readFieldInput("Group/Location", sel.GroupLocation, 30, false); editErr == nil {
				if newVal != sel.GroupLocation {
					st.pendingChanges["grouploc"] = newVal
					st.statusMessage = "|10Field marked for update.|07"
				} else {
					delete(st.pendingChanges, "grouploc")
					st.statusMessage = "|08No change.|07"
				}
				refresh = true
			} else {
				if editErr.Error() != "cancelled" {
					st.statusMessage = fmt.Sprintf("|01Error: %v|07", editErr)
				}
				refresh = true
			}
		case 'd', 'D':
			sel := st.users[st.selectedIndex]
			if newVal, editErr := readFieldInput("Note", sel.PrivateNote, 50, false); editErr == nil {
				if newVal != sel.PrivateNote {
					st.pendingChanges["note"] = newVal
					st.statusMessage = "|10Field marked for update.|07"
				} else {
					delete(st.pendingChanges, "note")
					st.statusMessage = "|08No change.|07"
				}
				refresh = true
			} else {
				if editErr.Error() != "cancelled" {
					st.statusMessage = fmt.Sprintf("|01Error: %v|07", editErr)
				}
				refresh = true
			}
		case 'e', 'E':
			sel := st.users[st.selectedIndex]
			if newVal, editErr := readFieldInput("Flags", sel.Flags, 20, false); editErr == nil {
				if newVal != sel.Flags {
					st.pendingChanges["flags"] = newVal
					st.statusMessage = "|10Field marked for update.|07"
				} else {
					delete(st.pendingChanges, "flags")
					st.statusMessage = "|08No change.|07"
				}
				refresh = true
			} else {
				if editErr.Error() != "cancelled" {
					st.statusMessage = fmt.Sprintf("|01Error: %v|07", editErr)
				}
				refresh = true
			}
		case 'f', 'F':
			sel := st.users[st.selectedIndex]
			levelStr := fmt.Sprintf("%d", sel.AccessLevel)
			if newVal, editErr := readFieldInput("Level", levelStr, 3, false); editErr == nil {
				if level, parseErr := strconv.Atoi(newVal); parseErr == nil {
					// Protect User #1 from level reduction
					if sel.ID == 1 && level < st.e.ServerCfg.SysOpLevel {
						st.statusMessage = "|01Cannot lower User #1 below SysOp level!|07"
						refresh = true
					} else if level != sel.AccessLevel {
						st.pendingChanges["level"] = level
						st.statusMessage = "|10Field marked for update.|07"
						refresh = true
					} else {
						delete(st.pendingChanges, "level")
						st.statusMessage = "|08No change.|07"
						refresh = true
					}
				} else {
					st.statusMessage = "|01Invalid number.|07"
					refresh = true
				}
			} else {
				if editErr.Error() != "cancelled" {
					st.statusMessage = fmt.Sprintf("|01Error: %v|07", editErr)
				}
				refresh = true
			}
		case 'g', 'G':
			// Toggle validated status
			sel := st.users[st.selectedIndex]
			if sel.ID == 1 && sel.Validated {
				// Don't allow unvalidating User #1
				st.statusMessage = "|01Cannot unvalidate User #1!|07"
				refresh = true
			} else {
				newValidated := !sel.Validated
				if newValidated != sel.Validated {
					st.pendingChanges["validated"] = newValidated
					if newValidated {
						st.statusMessage = "|10Validated status marked for update.|07"
					} else {
						st.statusMessage = "|11Unvalidated status marked for update.|07"
					}
				} else {
					delete(st.pendingChanges, "validated")
					st.statusMessage = "|08No change.|07"
				}
				refresh = true
			}
		case 'p', 'P':
			// Change password
			if newPassword, editErr := readFieldInput("New Password", "", 50, true); editErr == nil {
				if newPassword != "" {
					st.pendingChanges["password"] = newPassword
					st.statusMessage = "|10Password marked for update.|07"
				} else {
					delete(st.pendingChanges, "password")
					st.statusMessage = "|08Password change cancelled.|07"
				}
				refresh = true
			} else {
				if editErr.Error() != "cancelled" {
					st.statusMessage = fmt.Sprintf("|01Error: %v|07", editErr)
				}
				refresh = true
			}
		case '0':
			// Toggle ban user (sets level 0, unvalidated) or unban (restore to regular level)
			sel := st.users[st.selectedIndex]
			if sel.ID == 1 {
				st.statusMessage = "|01Cannot ban User #1!|07"
			} else {
				// Check if user is currently banned
				isBanned := sel.AccessLevel == 0 && !sel.Validated
				if isBanned {
					// Unban: restore to regular user level and validate
					st.pendingChanges["validated"] = true
					st.pendingChanges["level"] = st.e.ServerCfg.RegularUserLevel
					st.statusMessage = fmt.Sprintf("|10Un-ban marked for update (level %d, validated).|07", st.e.ServerCfg.RegularUserLevel)
				} else {
					// Ban: set level 0 and unvalidated
					st.pendingChanges["validated"] = false
					st.pendingChanges["level"] = 0
					st.statusMessage = "|01Ban marked for update (level 0, unvalidated).|07"
				}
			}
			refresh = true
		case '9':
			// Toggle delete user (soft delete)
			sel := st.users[st.selectedIndex]
			if sel.ID == 1 {
				st.statusMessage = "|01Cannot delete User #1!|07"
			} else {
				newDeleted := !sel.DeletedUser
				if newDeleted != sel.DeletedUser {
					st.pendingChanges["deleted"] = newDeleted
					if newDeleted {
						st.statusMessage = "|01Delete marked for update (soft delete).|07"
					} else {
						st.statusMessage = "|10Undelete marked for update (restore user).|07"
					}
				} else {
					delete(st.pendingChanges, "deleted")
					st.statusMessage = "|08No change.|07"
				}
			}
			refresh = true
		case 'i', 'I':
			// View selected user's infoforms - interactive menu
			if len(st.pendingChanges) == 0 {
				sel := st.users[st.selectedIndex]
				infoformsMu.Lock()
				ifCfg, ifErr := loadInfoFormConfig(st.e.RootConfigPath)
				infoformsMu.Unlock()

				if ifErr != nil {
					_ = terminalio.WriteProcessedBytes(st.terminal, []byte(ansi.ClearScreen()), st.outputMode)
					wv(st.terminal, "\r\n|04Error loading infoforms config.\r\n", st.outputMode)
					st.e.holdScreen(s, terminal, outputMode, termWidth, termHeight)
				} else {
					_ = browseInfoForms(st.e, s, terminal, outputMode, sel, ifCfg, termWidth, termHeight)
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
			if len(st.pendingChanges) == 0 {
				st.moveUp()
				refresh = true
			}
		case editor.KeyArrowDown:
			if len(st.pendingChanges) == 0 {
				st.moveDown()
				refresh = true
			}
		case editor.KeyEsc:
			if st.cfg.pendingOnly && len(st.pendingChanges) > 0 {
				st.statusMessage = "|11Unsaved changes! Press [S] to save or [X] to abort.|07"
			} else {
				return nil, "", nil
			}
		}

		if refresh {
			if err := renderList(); err != nil {
				return nil, "", err
			}
			if err := renderDetails(st.statusMessage); err != nil {
				return nil, "", err
			}
		} else if !st.cfg.pendingOnly && st.statusMessage != "" {
			if err := renderDetails(st.statusMessage); err != nil {
				return nil, "", err
			}
		}
	}
}
