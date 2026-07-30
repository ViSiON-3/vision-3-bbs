package menu

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// runInfoFormView lets a user view their own completed forms.
// Maps to V2's ShowInfoForms call pattern.
func runInfoFormView(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	s := c.s
	terminal := c.terminal
	currentUser := c.currentUser
	outputMode := c.outputMode
	termWidth := c.termWidth
	termHeight := c.termHeight

	if currentUser == nil {
		return currentUser, "", nil
	}

	viewPrompt := e.LoadedStrings.ViewWhichForm
	if viewPrompt == "" {
		viewPrompt = "|09View which |08F|07o|15rm? (|07#|15) |09:"
	}
	wv(terminal, viewPrompt, outputMode)
	input, err := readLineFromSessionIH(s, terminal)
	if err != nil {
		return currentUser, "", nil
	}
	formNum, nerr := strconv.Atoi(strings.TrimSpace(input))
	if nerr != nil || formNum < 1 || formNum > 5 {
		wv(terminal, "\r\n|04Invalid form number.\r\n", outputMode)
		return currentUser, "", nil
	}

	showInfoForm(e, s, terminal, outputMode, currentUser.ID, formNum, termHeight)
	e.holdScreen(s, terminal, outputMode, termWidth, termHeight)
	return currentUser, "", nil
}

// runInfoFormHunt lets sysops browse all users' completed forms.
// Maps to V2's InfoFormHunt in MAINMENU.PAS:1161.
func runInfoFormHunt(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	s := c.s
	terminal := c.terminal
	userManager := c.userManager
	currentUser := c.currentUser
	outputMode := c.outputMode
	termWidth := c.termWidth
	termHeight := c.termHeight

	if currentUser == nil {
		return currentUser, "", nil
	}

	isSysop := currentUser.AccessLevel >= 255
	if !isSysop {
		wv(terminal, "\r\n|04Access denied.\r\n", outputMode)
		return currentUser, "", nil
	}

	wv(terminal, "\r\n|07Show which infoform? |15(1-5)|07: ", outputMode)
	input, err := readLineFromSessionIH(s, terminal)
	if err != nil {
		return currentUser, "", nil
	}
	formNum, nerr := strconv.Atoi(strings.TrimSpace(input))
	if nerr != nil || formNum < 1 || formNum > 5 {
		wv(terminal, "\r\n|04Invalid form number.\r\n", outputMode)
		return currentUser, "", nil
	}

	if !templateExists(e.RootConfigPath, formNum) {
		wv(terminal, "\r\n|04That form template doesn't exist.\r\n", outputMode)
		return currentUser, "", nil
	}

	wv(terminal, "\r\n|15Showing All Forms #"+strconv.Itoa(formNum)+"\r\n", outputMode)
	wv(terminal, "|08"+strings.Repeat("\xc4", 50)+"\r\n", outputMode)

	// Scan responses directory for this form number
	respDir := filepath.Join(infoformsDataDir(e.RootConfigPath), "responses")
	entries, err := os.ReadDir(respDir)
	if err != nil {
		if os.IsNotExist(err) {
			wv(terminal, "|07No responses found.\r\n", outputMode)
			return currentUser, "", nil
		}
		wv(terminal, "\r\n|04Error reading responses.\r\n", outputMode)
		return currentUser, "", nil
	}

	suffix := fmt.Sprintf("_%d.json", formNum)
	found := 0
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), suffix) {
			continue
		}
		// Parse userID from filename
		parts := strings.SplitN(entry.Name(), "_", 2)
		if len(parts) < 2 {
			continue
		}
		uid, uerr := strconv.Atoi(parts[0])
		if uerr != nil {
			continue
		}

		resp, rerr := loadInfoFormResponse(e.RootConfigPath, uid, formNum)
		if rerr != nil || resp == nil {
			continue
		}

		found++
		displayHandle := resp.Handle
		if u, ok := userManager.GetUserByID(uid); ok && u.Handle != "" {
			displayHandle = u.Handle
		}
		wv(terminal, fmt.Sprintf("\r\n|11%s\r\n", displayHandle), outputMode)
		showInfoForm(e, s, terminal, outputMode, uid, formNum, termHeight)
	}

	if found == 0 {
		wv(terminal, "|07No responses found.\r\n", outputMode)
	}

	e.holdScreen(s, terminal, outputMode, termWidth, termHeight)
	return currentUser, "", nil
}

// runInfoFormRequired checks and forces required forms during login sequence.
// Maps to V2's GETLOGIN.PAS:1592 required forms enforcement.
func runInfoFormRequired(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	s := c.s
	terminal := c.terminal
	currentUser := c.currentUser
	nodeNumber := c.nodeNumber
	outputMode := c.outputMode
	termWidth := c.termWidth
	termHeight := c.termHeight

	if currentUser == nil {
		return currentUser, "", nil
	}

	// Only force required infoforms on new (unvalidated) users.
	// Existing validated users should not be prompted.
	if currentUser.Validated {
		return currentUser, "", nil
	}

	infoformsMu.Lock()
	cfg, err := loadInfoFormConfig(e.RootConfigPath)
	infoformsMu.Unlock()
	if err != nil {
		slog.Error("failed to load infoforms config in required check", "node", nodeNumber, "error", err)
		return currentUser, "", nil
	}

	for i := 0; i < 5; i++ {
		formNum := i + 1
		if !isFormRequired(cfg, formNum) {
			continue
		}
		if !templateExists(e.RootConfigPath, formNum) {
			continue
		}
		if hasCompletedForm(e.RootConfigPath, currentUser.ID, formNum) {
			continue
		}
		// Force fill out this required form
		fillInfoForm(e, s, terminal, outputMode, nodeNumber, currentUser, formNum, termWidth, termHeight)
		// Re-check: if form still not completed (save failed, user disconnected, etc.), block login
		if !hasCompletedForm(e.RootConfigPath, currentUser.ID, formNum) {
			slog.Warn("required infoform not completed, blocking login",
				"node", nodeNumber, "form", formNum, "handle", currentUser.Handle)
			wv(terminal, fmt.Sprintf("\r\n|04Required form #%d was not completed. Disconnecting.\r\n", formNum), outputMode)
			return currentUser, "LOGOFF", nil
		}
	}

	return currentUser, "", nil
}

// runInfoFormNuke lets sysops delete all form responses for a specific user.
// Maps to V2's nuke all infoforms (MAINMENU.PAS:1580-1592).
func runInfoFormNuke(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	s := c.s
	terminal := c.terminal
	userManager := c.userManager
	currentUser := c.currentUser
	nodeNumber := c.nodeNumber
	outputMode := c.outputMode
	termWidth := c.termWidth
	termHeight := c.termHeight

	if currentUser == nil {
		return currentUser, "", nil
	}

	isSysop := currentUser.AccessLevel >= 255
	if !isSysop {
		wv(terminal, "\r\n|04Access denied.\r\n", outputMode)
		return currentUser, "", nil
	}

	wv(terminal, "\r\n|07Handle to nuke infoforms for: ", outputMode)
	input, err := readLineFromSessionIH(s, terminal)
	if err != nil {
		return currentUser, "", nil
	}
	handle := strings.TrimSpace(input)
	if handle == "" {
		return currentUser, "", nil
	}

	// Look up user by handle
	targetUser, found := userManager.GetUser(handle)
	if !found {
		wv(terminal, "\r\n|04User not found.\r\n", outputMode)
		return currentUser, "", nil
	}

	nukeYes, err := e.PromptYesNo(s, terminal,
		fmt.Sprintf("|07Erase ALL info-forms for %s.. Are you sure? @", targetUser.Handle),
		outputMode, nodeNumber, termWidth, termHeight, false)
	if err != nil || !nukeYes {
		return currentUser, "", nil
	}

	infoformsMu.Lock()
	for i := 1; i <= 5; i++ {
		_ = deleteInfoFormResponse(e.RootConfigPath, targetUser.ID, i)
	}
	infoformsMu.Unlock()

	wv(terminal, "\r\n|10All infoforms deleted.\r\n", outputMode)
	slog.Info("sysop nuked infoforms for user", "node", nodeNumber, "handle", currentUser.Handle, "target", targetUser.Handle)
	return currentUser, "", nil
}
