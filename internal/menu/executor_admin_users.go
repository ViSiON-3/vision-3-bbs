package menu

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"github.com/gliderlabs/ssh"
	"golang.org/x/term"
)

// runListUsers displays a list of users, sorted alphabetically.
func runListUsers(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	s := c.s
	terminal := c.terminal
	userManager := c.userManager
	nodeNumber := c.nodeNumber
	outputMode := c.outputMode
	termWidth := c.termWidth
	termHeight := c.termHeight

	slog.Debug("running LISTUSERS", "node", nodeNumber)

	// 1. Load Templates (Corrected filenames)
	topTemplatePath := filepath.Join(e.MenuSetPath, "templates", "USERLIST.TOP")
	midTemplatePath := filepath.Join(e.MenuSetPath, "templates", "USERLIST.MID")
	botTemplatePath := filepath.Join(e.MenuSetPath, "templates", "USERLIST.BOT")

	topTemplateBytes, errTop := readTemplateFile(topTemplatePath)
	midTemplateBytes, errMid := readTemplateFile(midTemplatePath)
	botTemplateBytes, errBot := readTemplateFile(botTemplatePath)

	if errTop != nil || errMid != nil || errBot != nil {
		slog.Error("failed to load USERLIST template files", "node", nodeNumber, "top", errTop, "mid", errMid, "bot", errBot)
		msg := e.LoadedStrings.ExecUserlistTemplateErr
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		time.Sleep(1 * time.Second)
		return nil, "", fmt.Errorf("failed loading USERLIST templates")
	}

	// --- Process Pipe Codes in Templates FIRST ---
	processedTopTemplate := ansi.ReplacePipeCodes(topTemplateBytes)
	processedMidTemplate := string(ansi.ReplacePipeCodes(midTemplateBytes)) // Process MID template
	processedBotTemplate := ansi.ReplacePipeCodes(botTemplateBytes)
	// --- END Template Processing ---

	// 2. Get user list data from UserManager
	users := userManager.GetAllUsers() // Corrected method call
	pendingCount := 0
	for _, u := range users {
		if isPendingValidationUser(u) {
			pendingCount++
		}
	}

	// 3. Build the output string using processed templates and processed data
	var outputBuffer bytes.Buffer
	outputBuffer.Write(processedTopTemplate) // Write processed top template
	outputBuffer.WriteString("\r\n")
	outputBuffer.WriteString(string(ansi.ReplacePipeCodes([]byte(fmt.Sprintf(e.LoadedStrings.ExecPendingValidation, pendingCount)))))

	if len(users) == 0 {
		// Optional: Handle empty state. The template might handle this.
		slog.Debug("no users to display", "node", nodeNumber)
		// If templates don't handle empty, add a message here.
	} else {
		// Iterate through user records and format using processed USERLIST.MID
		for _, user := range users {
			// Skip deleted users in public listing
			if user.DeletedUser {
				continue
			}
			line := processedMidTemplate // Start with the pipe-code-processed mid template

			// Format data for substitution
			handle := strings.TrimSpace(string(ansi.ReplacePipeCodes([]byte(user.Handle))))
			level := strings.TrimSpace(strconv.Itoa(user.AccessLevel))
			groupLocation := strings.TrimSpace(string(ansi.ReplacePipeCodes([]byte(user.GroupLocation))))
			if groupLocation == "" {
				groupLocation = "-"
			}
			if !user.Validated {
				handle = handle + " [NV]"
			}

			handle = formatLastCallerATWidth(handle, 30, false)
			groupLocation = formatLastCallerATWidth(groupLocation, 24, false)
			level = formatLastCallerATWidth(level, 3, true)

			// Replace placeholders with *already processed* data
			// Match placeholders found in USERLIST.MID: |UH, |GL, |LV, |AC
			line = strings.ReplaceAll(line, "|UH", handle)        // Use |UH for Handle (Alias)
			line = strings.ReplaceAll(line, "|GL", groupLocation) // Use |GL for Group/Location (Replaces |UN)
			line = strings.ReplaceAll(line, "|LV", level)         // Use |LV for Level

			slog.Debug("about to write line for user", "handle", handle, "line", line)
			outputBuffer.WriteString(line) // Add the fully substituted and processed line
			slog.Debug("wrote line", "bufferSize", outputBuffer.Len())
		}
	}

	slog.Debug("finished user loop", "bufferSize", outputBuffer.Len())
	outputBuffer.Write(processedBotTemplate) // Write processed bottom template
	slog.Debug("added BOT template", "bufferSize", outputBuffer.Len())

	// 4. Clear screen and display the assembled content
	writeErr := terminalio.WriteProcessedBytes(terminal, []byte(ansi.ClearScreen()), outputMode)
	if writeErr != nil {
		slog.Error("failed clearing screen for USERLIST", "node", nodeNumber, "error", writeErr)
		return nil, "", writeErr
	}

	processedContent := outputBuffer.Bytes() // Contains already-processed ANSI bytes
	// For CP437 mode, write raw bytes directly to avoid UTF-8 false positives
	var wErr error
	if outputMode == ansi.OutputModeCP437 {
		_, wErr = terminal.Write(processedContent)
	} else {
		wErr = terminalio.WriteProcessedBytes(terminal, processedContent, outputMode)
	}
	if wErr != nil {
		slog.Error("failed writing USERLIST output", "node", nodeNumber, "error", wErr)
		return nil, "", wErr
	}

	// 5. Wait for Enter using configured PauseString (centered)
	pausePrompt := e.LoadedStrings.PauseString
	if pausePrompt == "" {
		pausePrompt = "\r\n|07Press |15[ENTER]|07 to continue... " // Fallback
	}

	slog.Debug("displaying USERLIST pause prompt", "node", nodeNumber)
	err := writeCenteredPausePrompt(s, terminal, pausePrompt, outputMode, termWidth, termHeight)
	if err != nil {
		if errors.Is(err, io.EOF) {
			slog.Info("user disconnected during USERLIST pause", "node", nodeNumber)
			return nil, "LOGOFF", io.EOF
		}
		slog.Error("failed during USERLIST pause", "node", nodeNumber, "error", err)
		return nil, "", err
	}

	return nil, "", nil // Success
}

// isPendingValidationUser returns true if the user is awaiting validation:
// not nil, not deleted, not banned (AccessLevel > 0), and not yet validated.
func isPendingValidationUser(u *user.User) bool {
	return u != nil && !u.Validated && !u.DeletedUser && u.AccessLevel > 0
}

func pendingValidationCount(userManager *user.UserMgr) int {
	if userManager == nil {
		return 0
	}
	users := userManager.GetAllUsers()
	count := 0
	for _, u := range users {
		if isPendingValidationUser(u) {
			count++
		}
	}
	return count
}

func sortedUsersByID(users []*user.User) []*user.User {
	filtered := make([]*user.User, 0, len(users))
	for _, u := range users {
		if u != nil {
			filtered = append(filtered, u)
		}
	}
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].ID < filtered[j].ID
	})
	return filtered
}

func adminTruncate(input string, max int) string {
	runes := []rune(strings.TrimSpace(input))
	if len(runes) <= max {
		return string(runes)
	}
	if max <= 1 {
		return string(runes[:max])
	}
	return string(runes[:max-1]) + "…"
}

func adminTime(t time.Time) string {
	if t.IsZero() {
		return "N/A"
	}
	return t.Format("2006-01-02 15:04")
}

func adminDate(t time.Time) string {
	if t.IsZero() {
		return "N/A"
	}
	return t.Format("2006-01-02")
}

func adminUserLightbarBrowser(s ssh.Session, terminal *term.Terminal, users []*user.User, title string, instruction string, outputMode ansi.OutputMode, selectOnEnter bool) (*user.User, bool, error) {
	if len(users) == 0 {
		return nil, false, nil
	}

	selectedIndex := 0
	topIndex := 0
	pageSize := 10
	ih := getSessionIH(s)

	render := func() error {
		if err := terminalio.WriteProcessedBytes(terminal, []byte(ansi.ClearScreen()), outputMode); err != nil {
			return err
		}

		var b strings.Builder
		b.WriteString("\r\n|15" + title + "|07\r\n")
		b.WriteString("|07" + instruction + "\r\n")
		b.WriteString("|08--------------------------------------------------------------------------------|07\r\n")

		endIndex := topIndex + pageSize
		if endIndex > len(users) {
			endIndex = len(users)
		}

		for idx := topIndex; idx < endIndex; idx++ {
			u := users[idx]
			status := "OK"
			if u.DeletedUser {
				status = "DEL"
			} else if !u.Validated {
				status = "NV"
			}
			prefix := "  "
			if idx == selectedIndex {
				prefix = "» "
			}
			line := fmt.Sprintf("%s%-22s  ID:%-4d L:%-3d %-2s", prefix, adminTruncate(u.Handle, 22), u.ID, u.AccessLevel, status)
			if idx == selectedIndex {
				b.WriteString("\x1b[7m" + line + "\x1b[0m\r\n")
			} else {
				b.WriteString("|07" + line + "|07\r\n")
			}
		}

		for idx := endIndex; idx < topIndex+pageSize; idx++ {
			b.WriteString("\r\n")
		}

		sel := users[selectedIndex]
		b.WriteString("|08--------------------------------------------------------------------------------|07\r\n")
		b.WriteString(fmt.Sprintf("|15Handle        :|07 %s\r\n", adminTruncate(sel.Handle, 56)))
		b.WriteString(fmt.Sprintf("|15Real Name     :|07 %-21s |15Flags         :|07 %s\r\n", adminTruncate(sel.RealName, 21), adminTruncate(sel.Flags, 8)))
		b.WriteString(fmt.Sprintf("|15Group/Location:|07 %s\r\n", adminTruncate(sel.GroupLocation, 40)))
		b.WriteString(fmt.Sprintf("|15Validated     :|07 %-5t |15Level         :|07 %-3d |15TimeLimit     :|07 %-4d\r\n", sel.Validated, sel.AccessLevel, sel.TimeLimit))
		b.WriteString(fmt.Sprintf("|15Created       :|07 %-16s |15Last Login    :|07 %s\r\n", adminTime(sel.CreatedAt), adminTime(sel.LastLogin)))
		b.WriteString(fmt.Sprintf("|15Calls         :|07 %-5d |15Uploads       :|07 %-5d |15FilePoints    :|07 %-6d\r\n", sel.TimesCalled, sel.NumUploads, sel.FilePoints))
		b.WriteString(fmt.Sprintf("|15Msg Area      :|07 %-16s |15File Area     :|07 %s\r\n", adminTruncate(sel.CurrentMessageAreaTag, 16), adminTruncate(sel.CurrentFileAreaTag, 24)))
		b.WriteString(fmt.Sprintf("|15Note          :|07 %s\r\n", adminTruncate(sel.PrivateNote, 70)))

		return terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(b.String())), outputMode)
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

	for {
		if err := render(); err != nil {
			return nil, false, err
		}

		key, err := ih.ReadKey()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil, false, io.EOF
			}
			return nil, false, err
		}

		switch key {
		case int('k'), int('K'), int('w'), int('W'):
			moveUp()
		case int('j'), int('J'), int('s'), int('S'):
			moveDown()
		case int('q'), int('Q'):
			return nil, false, nil
		case int('\r'), int('\n'):
			if selectOnEnter {
				return users[selectedIndex], true, nil
			}
		case editor.KeyArrowUp:
			moveUp()
		case editor.KeyArrowDown:
			moveDown()
		case editor.KeyEsc:
			return nil, false, nil
		}
	}
}

// runAdminListUsers presents the full interactive user editor (USEREDITOR / UE).
func runAdminListUsers(c *cmdCtx, args string) (*user.User, string, error) {
	return runUserEditor(c, userEditorConfig{
		title:        "|15ViSiON|03/|153 |08- |14User Editor|07",
		emptyMessage: "|10No users found.",
	})
}

// runPendingValidationNotice notifies SysOps when users are awaiting validation.
func runPendingValidationNotice(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	s := c.s
	terminal := c.terminal
	userManager := c.userManager
	currentUser := c.currentUser
	sessionStartTime := c.sessionStartTime
	outputMode := c.outputMode

	if currentUser == nil || userManager == nil {
		return nil, "", nil
	}

	sysOpACS := fmt.Sprintf("S%d", e.ServerCfg.SysOpLevel)
	if !checkACS(sysOpACS, currentUser, s, terminal, sessionStartTime) {
		return nil, "", nil
	}

	pendingCount := pendingValidationCount(userManager)

	if pendingCount == 0 {
		return nil, "", nil
	}

	notice := fmt.Sprintf("\r\n|11Admin: |15[V]|11 Validate user account [|15%d|11]. Press |15X|11 for Admin menu.|07\r\n", pendingCount)
	if err := terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(notice)), outputMode); err != nil {
		return nil, "", err
	}

	return nil, "", nil
}

// runAdminToggleAllowNewUsers toggles the allowNewUsers config flag and persists it to config.json.
func runAdminToggleAllowNewUsers(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	s := c.s
	terminal := c.terminal
	currentUser := c.currentUser
	nodeNumber := c.nodeNumber
	sessionStartTime := c.sessionStartTime
	outputMode := c.outputMode

	if currentUser == nil {
		return nil, "", nil
	}
	sysOpACS := fmt.Sprintf("S%d", e.ServerCfg.SysOpLevel)
	if !checkACS(sysOpACS, currentUser, s, terminal, sessionStartTime) {
		_ = terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n|01Access denied.|07\r\n")), outputMode)
		time.Sleep(1 * time.Second)
		return nil, "", nil
	}

	cfg := e.GetServerConfig()
	cfg.AllowNewUsers = !cfg.AllowNewUsers

	if err := config.SaveServerConfig(e.RootConfigPath, cfg); err != nil {
		slog.Error("failed to save config after toggling allowNewUsers", "node", nodeNumber, "error", err)
		_ = terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n|01Error saving config.|07\r\n")), outputMode)
		time.Sleep(1 * time.Second)
		return nil, "", nil
	}

	e.SetServerConfig(cfg)

	stateStr := "|12CLOSED|07"
	if cfg.AllowNewUsers {
		stateStr = "|10OPEN|07"
	}
	msg := fmt.Sprintf("\r\n|07New user registrations: %s\r\n", stateStr)
	_ = terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
	time.Sleep(1 * time.Second)
	return nil, "", nil
}
