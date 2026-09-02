package menu

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"github.com/ViSiON-3/vision-3-bbs/internal/version"
)

func runPlaceholderCommand(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	terminal := c.terminal
	currentUser := c.currentUser
	nodeNumber := c.nodeNumber
	outputMode := c.outputMode

	e.showUndefinedMenuInput(terminal, outputMode, nodeNumber)
	return currentUser, "", nil
}

func runMainLogoffCommand(c *cmdCtx, args string) (*user.User, string, error) {
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

	prompt := e.LoadedStrings.LogOffStr
	if prompt == "" {
		prompt = "\r\n|07Log off now? @"
	}

	confirm, err := e.PromptYesNo(s, terminal, prompt, outputMode, nodeNumber, termWidth, termHeight, false)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, "LOGOFF", io.EOF
		}
		return currentUser, "", err
	}

	if !confirm {
		return currentUser, "", nil
	}

	return runImmediateLogoffCommand(&cmdCtx{e: e, s: s, terminal: terminal, userManager: userManager, currentUser: currentUser, nodeNumber: nodeNumber, sessionStartTime: sessionStartTime, outputMode: outputMode, termWidth: termWidth, termHeight: termHeight}, args)
}

func runImmediateLogoffCommand(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	terminal := c.terminal
	currentUser := c.currentUser
	nodeNumber := c.nodeNumber
	outputMode := c.outputMode

	if displayErr := e.displayFile(terminal, "GOODBYE.ANS", outputMode, c.termHeight); displayErr != nil {
		slog.Warn("failed to display GOODBYE.ANS before logoff", "node", nodeNumber, "error", displayErr)
		_ = terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.LoadedStrings.ExecGoodbye)), outputMode)
	}

	time.Sleep(1 * time.Second)
	return currentUser, "LOGOFF", nil
}

// runShowStats displays the user statistics screen (YOURSTAT.ANS).
func runShowStats(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	s := c.s
	terminal := c.terminal
	currentUser := c.currentUser
	nodeNumber := c.nodeNumber
	sessionStartTime := c.sessionStartTime
	outputMode := c.outputMode
	termWidth := c.termWidth
	termHeight := c.termHeight

	if currentUser == nil {
		slog.Warn("showstats called without logged in user", "node", nodeNumber)
		msg := e.LoadedStrings.ExecStatsLogin
		wErr := terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		if wErr != nil {
			slog.Error("failed writing showstats error message", "error", wErr)
		}
		time.Sleep(1 * time.Second)
		return nil, "", nil // Updated return
	}

	ansFilename := "YOURSTAT.ANS"
	// Use MenuSetPath for ANSI file
	fullAnsPath := filepath.Join(e.MenuSetPath, "ansi", ansFilename)
	rawAnsiContent, readErr := ansi.GetAnsiFileContent(fullAnsPath)
	if readErr != nil {
		slog.Error("failed to read file for showstats", "node", nodeNumber, "path", fullAnsPath, "error", readErr)
		msg := fmt.Sprintf(e.LoadedStrings.ExecStatsError, ansFilename)
		wErr := terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		if wErr != nil {
			slog.Error("failed writing showstats file read error message", "error", wErr)
		}
		time.Sleep(1 * time.Second)
		return nil, "", fmt.Errorf("failed to read %s: %w", ansFilename, readErr) // Updated return
	}

	placeholders := map[string]string{
		"|UH": currentUser.Handle,
		"|UN": currentUser.PrivateNote,
		"|UL": strconv.Itoa(currentUser.AccessLevel),
		"|FL": strconv.Itoa(currentUser.AccessLevel),
		"|UK": strconv.Itoa(currentUser.NumUploads),
		"|NU": strconv.Itoa(currentUser.NumUploads),
		"|DK": "0", "|ND": "0", "|TP": "0", "|NM": "0", "|LC": "N/A",
	}
	if currentUser.TimeLimit <= 0 {
		placeholders["|TL"] = "Unlimited"
	} else {
		elapsedSeconds := time.Since(sessionStartTime).Seconds()
		totalSeconds := float64(currentUser.TimeLimit * 60)
		remainingSeconds := totalSeconds - elapsedSeconds
		if remainingSeconds < 0 {
			remainingSeconds = 0
		}
		placeholders["|TL"] = strconv.Itoa(int(remainingSeconds / 60))
	}

	// Branch based on output mode to preserve encoding correctness
	slog.Debug("showstats output mode", "node", nodeNumber, "outputMode", outputMode)
	var statsDisplayBytes []byte
	if outputMode == ansi.OutputModeUTF8 {
		// UTF-8 mode: Convert CP437→UTF-8 first, then substitute placeholders
		utf8Content := string(ansi.CP437BytesToUTF8(rawAnsiContent))
		for key, val := range placeholders {
			utf8Content = strings.ReplaceAll(utf8Content, key, val)
		}
		statsDisplayBytes = []byte(utf8Content)
	} else {
		// CP437 mode: Substitute placeholders directly on raw bytes
		// (WriteProcessedBytes will pass them through unchanged)
		statsDisplayBytes = rawAnsiContent
		for key, val := range placeholders {
			statsDisplayBytes = bytes.ReplaceAll(statsDisplayBytes, []byte(key), []byte(val))
		}
	}

	// Use WriteProcessedBytes for ClearScreen
	wErr := terminalio.WriteProcessedBytes(terminal, []byte(ansi.ClearScreen()), outputMode)
	if wErr != nil {
		// Log error but continue if possible
		slog.Error("failed clearing screen for showstats", "node", nodeNumber, "error", wErr)
	}
	// For CP437 mode with raw ANSI content, write bytes directly to avoid UTF-8 decode artifacts
	if outputMode == ansi.OutputModeCP437 {
		_, wErr = terminal.Write(statsDisplayBytes)
	} else {
		wErr = terminalio.WriteProcessedBytes(terminal, statsDisplayBytes, outputMode)
	}
	if wErr != nil {
		slog.Error("failed writing processed YOURSTAT.ANS", "node", nodeNumber, "error", wErr)
		return nil, "", wErr // Updated return
	}

	// 5. Wait for Enter key press
	pausePrompt := e.LoadedStrings.PauseString // Use configured pause string
	if pausePrompt == "" {
		pausePrompt = "\r\n|07Press |15[ENTER]|07 to continue... " // Fallback
	}

	slog.Debug("displaying showstats pause prompt (centered)", "node", nodeNumber)
	err := writeCenteredPausePrompt(s, terminal, pausePrompt, outputMode, termWidth, termHeight)
	if err != nil {
		if errors.Is(err, io.EOF) {
			slog.Info("user disconnected during showstats pause", "node", nodeNumber)
			return nil, "LOGOFF", io.EOF
		}
		slog.Error("failed during showstats pause", "error", err)
		return nil, "", err
	}
	return nil, "", nil // Updated return (Success)
}

// runShowVersion displays configured version information.
func runShowVersion(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	s := c.s
	terminal := c.terminal
	nodeNumber := c.nodeNumber
	outputMode := c.outputMode
	termWidth := c.termWidth
	termHeight := c.termHeight

	slog.Debug("running showversion", "node", nodeNumber)

	versionTemplate := e.LoadedStrings.ExecVersionString
	versionString := versionTemplate
	if strings.Contains(versionTemplate, "%s") {
		versionString = fmt.Sprintf(versionTemplate, version.Number)
	}

	// Display the version
	terminalio.WriteProcessedBytes(terminal, []byte(ansi.ClearScreen()), outputMode) // Optional: Clear screen
	terminalio.WriteProcessedBytes(terminal, []byte("\r\n\r\n"), outputMode)         // Add some spacing
	wErr := terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(versionString)), outputMode)
	if wErr != nil {
		slog.Error("failed writing showversion output", "node", nodeNumber, "error", wErr)
		// Don't return error, just log it
	}

	// Wait for Enter
	pausePrompt := e.LoadedStrings.PauseString // Use configured pause string
	if pausePrompt == "" {
		slog.Warn("pausestring empty, no pause prompt will be shown for showversion", "node", nodeNumber)
		// Don't use a hardcoded fallback. If it's empty, it's empty.
	} else {
		terminalio.WriteProcessedBytes(terminal, []byte("\r\n"), outputMode) // Add newline before pause
		slog.Debug("displaying showversion pause prompt (centered)", "node", nodeNumber)
		err := writeCenteredPausePrompt(s, terminal, pausePrompt, outputMode, termWidth, termHeight)
		if err != nil {
			if errors.Is(err, io.EOF) {
				slog.Info("user disconnected during showversion pause", "node", nodeNumber)
				return nil, "LOGOFF", io.EOF
			}
			slog.Error("failed during showversion pause", "node", nodeNumber, "error", err)
			return nil, "", err
		}
	}

	return nil, "", nil // Return to the current menu
}
