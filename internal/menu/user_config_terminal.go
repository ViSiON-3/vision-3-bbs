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
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

func runCfgScreenWidth(c *cmdCtx, args string) (*user.User, string, error) {
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

	current := currentUser.ScreenWidth
	if current == 0 {
		current = 80
	}
	prompt := fmt.Sprintf(e.LoadedStrings.CfgScreenWidthPrompt, current)
	terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(prompt)), outputMode)

	input, err := readLineFromSessionIH(s, terminal)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, "LOGOFF", io.EOF
		}
		return currentUser, "", nil
	}

	input = strings.TrimSpace(input)
	if input == "" {
		return currentUser, "", nil
	}

	val, parseErr := strconv.Atoi(input)
	if parseErr != nil || val < 40 || val > 255 {
		msg := e.LoadedStrings.CfgScreenWidthInvalid
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		time.Sleep(500 * time.Millisecond)
		return currentUser, "", nil
	}

	currentUser.ScreenWidth = val
	if err := userManager.UpdateUser(currentUser); err != nil {
		slog.Error("failed to save screen width", "node", nodeNumber, "error", err)
		return currentUser, "", nil
	}

	msg := fmt.Sprintf(e.LoadedStrings.CfgScreenWidthSet, val)
	terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
	time.Sleep(500 * time.Millisecond)
	return currentUser, "", nil
}

func runCfgScreenHeight(c *cmdCtx, args string) (*user.User, string, error) {
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

	current := currentUser.ScreenHeight
	if current == 0 {
		current = 25
	}
	prompt := fmt.Sprintf(e.LoadedStrings.CfgScreenHeightPrompt, current)
	terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(prompt)), outputMode)

	input, err := readLineFromSessionIH(s, terminal)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, "LOGOFF", io.EOF
		}
		return currentUser, "", nil
	}

	input = strings.TrimSpace(input)
	if input == "" {
		return currentUser, "", nil
	}

	val, parseErr := strconv.Atoi(input)
	if parseErr != nil || val < 21 || val > 60 {
		msg := e.LoadedStrings.CfgScreenHeightInvalid
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		time.Sleep(500 * time.Millisecond)
		return currentUser, "", nil
	}

	currentUser.ScreenHeight = val
	if err := userManager.UpdateUser(currentUser); err != nil {
		slog.Error("failed to save screen height", "node", nodeNumber, "error", err)
		return currentUser, "", nil
	}

	msg := fmt.Sprintf(e.LoadedStrings.CfgScreenHeightSet, val)
	terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
	time.Sleep(500 * time.Millisecond)
	return currentUser, "", nil
}

func runCfgTermType(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	terminal := c.terminal
	userManager := c.userManager
	currentUser := c.currentUser
	nodeNumber := c.nodeNumber
	outputMode := c.outputMode

	if currentUser == nil {
		return nil, "", nil
	}

	current := currentUser.OutputMode
	if current == "" {
		current = "cp437"
	}

	if current == "ansi" {
		currentUser.OutputMode = "cp437"
	} else {
		currentUser.OutputMode = "ansi"
	}

	if err := userManager.UpdateUser(currentUser); err != nil {
		slog.Error("failed to save terminal type", "node", nodeNumber, "error", err)
		return currentUser, "", nil
	}

	msg := fmt.Sprintf(e.LoadedStrings.CfgTermTypeSet, strings.ToUpper(currentUser.OutputMode))
	terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
	time.Sleep(500 * time.Millisecond)
	return currentUser, "", nil
}
