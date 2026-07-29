package menu

import (
	"fmt"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
)

// writeAt positions the cursor at row,col, clears the row, and writes text.
func (st *userEditorState) writeAt(row, col int, text string) error {
	cmd := fmt.Sprintf("\x1b[%d;%dH\x1b[2K%s", row, col, text)
	return terminalio.WriteProcessedBytes(st.terminal, ansi.ReplacePipeCodes([]byte(cmd)), st.outputMode)
}

// clearRow clears the given terminal row.
func (st *userEditorState) clearRow(row int) error {
	cmd := fmt.Sprintf("\x1b[%d;1H\x1b[2K", row)
	return terminalio.WriteProcessedBytes(st.terminal, []byte(cmd), st.outputMode)
}

// renderHeader draws the static title, column header, and separator rows,
// and clears the detail/status area below the list.
func (st *userEditorState) renderHeader() error {
	if err := terminalio.WriteProcessedBytes(st.terminal, []byte(ansi.ClearScreen()), st.outputMode); err != nil {
		return err
	}
	if err := st.writeAt(st.layout.titleRow, 1, st.cfg.title); err != nil {
		return err
	}
	if err := st.clearRow(st.layout.sepTopRow); err != nil {
		return err
	}
	// Render column header - aligned with data columns
	// Format: prefix(2) handle(22) space(3) date(10) space(3) ID:(3+4) space(3) L:(2+3) space(2) status(2)
	headerText := fmt.Sprintf("|08  %-22s   %-10s   %-7s   %-5s|07", "Handle", "Created", "ID", "Level")
	if err := st.writeAt(st.layout.headerRow, 1, headerText); err != nil {
		return err
	}
	if err := st.clearRow(st.layout.sepMidRow); err != nil {
		return err
	}
	for r := st.layout.detailStartRow; r <= st.layout.statusRow; r++ {
		if err := st.clearRow(r); err != nil {
			return err
		}
	}
	return nil
}

// renderActionBar draws the centered, context-sensitive key hint bar on the
// action row.
func (st *userEditorState) renderActionBar() error {
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
	if err := st.clearRow(st.layout.actionRow); err != nil {
		return err
	}
	// Center the action bar
	textWidth := visualPipeWidth(barText)
	padding := (80 - textWidth) / 2
	if padding < 1 {
		padding = 1
	}
	centeredText := strings.Repeat(" ", padding) + barText
	return st.writeAt(st.layout.actionRow, 1, centeredText)
}

// renderList redraws the visible page of the user list.
func (st *userEditorState) renderList() error {
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
		if err := st.writeAt(row, 1, line); err != nil {
			return err
		}
		row++
	}
	for ; row < st.layout.listStartRow+st.layout.pageSize; row++ {
		if err := st.clearRow(row); err != nil {
			return err
		}
	}
	return nil
}

// renderDetails redraws the detail/edit fields for the selected user, the
// status row (message, or cleared when empty), and the action bar.
func (st *userEditorState) renderDetails(message string) error {
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
	if err := st.writeAt(st.layout.sepMidRow, 1, separator); err != nil {
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
		if err := st.writeAt(st.layout.detailStartRow+i, 1, line); err != nil {
			return err
		}
	}
	if message != "" {
		if err := st.writeAt(st.layout.statusRow, 1, message); err != nil {
			return err
		}
	} else {
		if err := st.clearRow(st.layout.statusRow); err != nil {
			return err
		}
	}
	return st.renderActionBar()
}
