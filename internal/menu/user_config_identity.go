package menu

import (
	"errors"
	"io"
	"log/slog"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

func runCfgRealName(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	s := c.s
	terminal := c.terminal
	userManager := c.userManager
	currentUser := c.currentUser
	nodeNumber := c.nodeNumber
	outputMode := c.outputMode

	return runCfgStringInput(e, s, terminal, userManager, currentUser, nodeNumber, outputMode,
		"Real Name", 40,
		func(u *user.User) string { return u.RealName },
		func(u *user.User, v string) { u.RealName = v },
	)
}

func runCfgNote(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	s := c.s
	terminal := c.terminal
	userManager := c.userManager
	currentUser := c.currentUser
	nodeNumber := c.nodeNumber
	outputMode := c.outputMode

	return runCfgStringInput(e, s, terminal, userManager, currentUser, nodeNumber, outputMode,
		"User Note", 35,
		func(u *user.User) string { return u.PrivateNote },
		func(u *user.User, v string) { u.PrivateNote = v },
	)
}

func runCfgPassword(c *cmdCtx, args string) (*user.User, string, error) {
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
		return nil, "", nil
	}

	// Prompt for current password
	msg := e.LoadedStrings.CfgCurrentPwPrompt
	terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)

	oldPw, err := readPasswordSecurely(s, terminal, outputMode)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, "LOGOFF", io.EOF
		}
		return currentUser, "", nil
	}

	// Verify current password
	if bcryptErr := bcrypt.CompareHashAndPassword([]byte(currentUser.PasswordHash), []byte(oldPw)); bcryptErr != nil {
		msg := e.LoadedStrings.CfgIncorrectPw
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		time.Sleep(1 * time.Second)
		return currentUser, "", nil
	}

	// Prompt for new password using existing helper
	newPw, err := e.promptForPassword(s, terminal, nodeNumber, outputMode, termWidth, termHeight)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, "LOGOFF", io.EOF
		}
		return currentUser, "", nil
	}
	if newPw == "" {
		return currentUser, "", nil
	}

	// Hash and save
	hashed, err := bcrypt.GenerateFromPassword([]byte(newPw), bcrypt.DefaultCost)
	if err != nil {
		slog.Error("failed to hash new password", "node", nodeNumber, "error", err)
		return currentUser, "", nil
	}

	currentUser.PasswordHash = string(hashed)
	if err := userManager.UpdateUser(currentUser); err != nil {
		slog.Error("failed to save password", "node", nodeNumber, "error", err)
		return currentUser, "", nil
	}

	msg = e.LoadedStrings.CfgPasswordChanged
	terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
	time.Sleep(1 * time.Second)
	return currentUser, "", nil
}

func runCfgCustomPrompt(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	s := c.s
	terminal := c.terminal
	userManager := c.userManager
	currentUser := c.currentUser
	nodeNumber := c.nodeNumber
	outputMode := c.outputMode

	if currentUser == nil {
		return nil, "", nil
	}

	help := e.LoadedStrings.CfgCustomPromptHelp
	terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(help)), outputMode)

	return runCfgStringInput(e, s, terminal, userManager, currentUser, nodeNumber, outputMode,
		"Custom Prompt", 80,
		func(u *user.User) string { return u.CustomPrompt },
		func(u *user.User, v string) { u.CustomPrompt = v },
	)
}
