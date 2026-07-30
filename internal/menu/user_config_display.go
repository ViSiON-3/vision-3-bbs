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

func runCfgHotKeys(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	s := c.s
	terminal := c.terminal
	userManager := c.userManager
	currentUser := c.currentUser
	nodeNumber := c.nodeNumber
	sessionStartTime := c.sessionStartTime
	outputMode := c.outputMode

	return runCfgToggle(e, s, terminal, userManager, currentUser, nodeNumber, sessionStartTime, args, outputMode,
		"Hot Keys",
		func(u *user.User) bool { return u.HotKeys },
		func(u *user.User, v bool) { u.HotKeys = v },
	)
}

func runCfgMorePrompts(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	s := c.s
	terminal := c.terminal
	userManager := c.userManager
	currentUser := c.currentUser
	nodeNumber := c.nodeNumber
	sessionStartTime := c.sessionStartTime
	outputMode := c.outputMode

	return runCfgToggle(e, s, terminal, userManager, currentUser, nodeNumber, sessionStartTime, args, outputMode,
		"More Prompts",
		func(u *user.User) bool { return u.MorePrompts },
		func(u *user.User, v bool) { u.MorePrompts = v },
	)
}

var colorSlotNames = [7]string{"Prompt", "Input", "Text", "Stat", "Text2", "Stat2", "Bar"}

func runCfgColor(c *cmdCtx, args string) (*user.User, string, error) {
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

	slot := 0
	if trimmed := strings.TrimSpace(args); trimmed != "" {
		if parsed, err := strconv.Atoi(trimmed); err == nil && parsed >= 0 && parsed < 7 {
			slot = parsed
		}
	}

	slotName := colorSlotNames[slot]

	// Display color palette
	var palette strings.Builder
	fmt.Fprintf(&palette, e.LoadedStrings.CfgColorSelectPrompt, slotName)
	for i := 0; i < 16; i++ {
		fmt.Fprintf(&palette, "|%02d  %2d  ", i, i)
		if i == 7 {
			palette.WriteString("\r\n")
		}
	}
	palette.WriteString(e.LoadedStrings.CfgColorInputPrompt)
	terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(palette.String())), outputMode)

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
	if parseErr != nil || val < 0 || val > 15 {
		msg := e.LoadedStrings.CfgColorInvalid
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		time.Sleep(500 * time.Millisecond)
		return currentUser, "", nil
	}

	currentUser.Colors[slot] = val
	if err := userManager.UpdateUser(currentUser); err != nil {
		slog.Error("failed to save color", "node", nodeNumber, "error", err)
		return currentUser, "", nil
	}

	msg := fmt.Sprintf(e.LoadedStrings.CfgColorSet, slotName, val, val)
	terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
	time.Sleep(500 * time.Millisecond)
	return currentUser, "", nil
}

func runCfgFileListMode(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	terminal := c.terminal
	userManager := c.userManager
	currentUser := c.currentUser
	nodeNumber := c.nodeNumber
	outputMode := c.outputMode

	if currentUser == nil {
		return nil, "", nil
	}

	originalMode := currentUser.FileListingMode
	if strings.ToLower(originalMode) != "classic" {
		currentUser.FileListingMode = "classic"
	} else {
		currentUser.FileListingMode = "lightbar"
	}

	if err := userManager.UpdateUser(currentUser); err != nil {
		currentUser.FileListingMode = originalMode
		slog.Error("failed to save file listing mode", "node", nodeNumber, "error", err)
		return currentUser, "", nil
	}

	msg := fmt.Sprintf(e.LoadedStrings.CfgFileListModeSet, fileListModeDisplay(currentUser.FileListingMode))
	terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
	time.Sleep(500 * time.Millisecond)
	return currentUser, "", nil
}

func runCfgFileColumns(c *cmdCtx, args string) (*user.User, string, error) {
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

	boolStr := func(v bool) string {
		if v {
			return e.LoadedStrings.CfgToggleOn
		}
		return e.LoadedStrings.CfgToggleOff
	}

	for {
		c := currentUser.FileListColumns
		allDefault := !c.Name && !c.Size && !c.Date && !c.Downloads && !c.Uploader && !c.Description

		displayState := func(val bool) string {
			if allDefault {
				return boolStr(true)
			}
			return boolStr(val)
		}

		var buf strings.Builder
		buf.WriteString(e.LoadedStrings.CfgFileColumnsHeader)
		fmt.Fprintf(&buf, e.LoadedStrings.CfgFileColumnsToggle, "N", "Name", displayState(c.Name))
		fmt.Fprintf(&buf, e.LoadedStrings.CfgFileColumnsToggle, "S", "Size", displayState(c.Size))
		fmt.Fprintf(&buf, e.LoadedStrings.CfgFileColumnsToggle, "D", "Date", displayState(c.Date))
		fmt.Fprintf(&buf, e.LoadedStrings.CfgFileColumnsToggle, "L", "Downloads", displayState(c.Downloads))
		fmt.Fprintf(&buf, e.LoadedStrings.CfgFileColumnsToggle, "U", "Uploader", displayState(c.Uploader))
		fmt.Fprintf(&buf, e.LoadedStrings.CfgFileColumnsToggle, "E", "Description", displayState(c.Description))
		buf.WriteString(e.LoadedStrings.CfgFileColumnsHeader) // reuse as prompt separator
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(buf.String())), outputMode)

		input, err := readLineFromSessionIH(s, terminal)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil, "LOGOFF", io.EOF
			}
			return currentUser, "", nil
		}

		input = strings.TrimSpace(strings.ToUpper(input))
		if input == "" || input == "Q" {
			if err := userManager.UpdateUser(currentUser); err != nil {
				slog.Error("failed to save file column preferences", "node", nodeNumber, "error", err)
			}
			terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.LoadedStrings.CfgFileColumnsSaved)), outputMode)
			time.Sleep(500 * time.Millisecond)
			return currentUser, "", nil
		}

		if allDefault {
			currentUser.FileListColumns.Name = true
			currentUser.FileListColumns.Size = true
			currentUser.FileListColumns.Date = true
			currentUser.FileListColumns.Downloads = true
			currentUser.FileListColumns.Uploader = true
			currentUser.FileListColumns.Description = true
		}

		switch input {
		case "N":
			currentUser.FileListColumns.Name = !currentUser.FileListColumns.Name
		case "S":
			currentUser.FileListColumns.Size = !currentUser.FileListColumns.Size
		case "D":
			currentUser.FileListColumns.Date = !currentUser.FileListColumns.Date
		case "L":
			currentUser.FileListColumns.Downloads = !currentUser.FileListColumns.Downloads
		case "U":
			currentUser.FileListColumns.Uploader = !currentUser.FileListColumns.Uploader
		case "E":
			currentUser.FileListColumns.Description = !currentUser.FileListColumns.Description
		}
	}
}
