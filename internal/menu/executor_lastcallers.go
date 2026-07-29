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
)

// runLastCallers displays the last callers list using templates.
func runLastCallers(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	s := c.s
	terminal := c.terminal
	userManager := c.userManager
	currentUser := c.currentUser
	nodeNumber := c.nodeNumber
	outputMode := c.outputMode
	termWidth := c.termWidth
	termHeight := c.termHeight

	slog.Debug("running LASTCALLERS", "node", nodeNumber)

	// Parse optional caller count argument (e.g., RUN:LASTCALLERS 10)
	callerLimit := 10
	if strings.TrimSpace(args) != "" {
		if parsedLimit, parseErr := strconv.Atoi(strings.TrimSpace(args)); parseErr == nil && parsedLimit > 0 {
			callerLimit = parsedLimit
		}
	}

	// 1. Load Template Files from MenuSetPath/templates
	topTemplatePath := filepath.Join(e.MenuSetPath, "templates", "LASTCALL.TOP")
	midTemplatePath := filepath.Join(e.MenuSetPath, "templates", "LASTCALL.MID")
	botTemplatePath := filepath.Join(e.MenuSetPath, "templates", "LASTCALL.BOT")

	topTemplateBytes, errTop := readTemplateFile(topTemplatePath)
	midTemplateBytes, errMid := readTemplateFile(midTemplatePath)
	botTemplateBytes, errBot := readTemplateFile(botTemplatePath)

	if errTop != nil || errMid != nil || errBot != nil {
		slog.Error("failed to load LASTCALL template files", "node", nodeNumber, "top", errTop, "mid", errMid, "bot", errBot)
		msg := e.LoadedStrings.ExecLastcallTemplateErr
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		time.Sleep(1 * time.Second)
		return nil, "", fmt.Errorf("failed loading LASTCALL templates")
	}

	// Strip SAUCE metadata, normalize delimiters, and process pipe codes in templates first.
	topTemplateBytes = stripSauceMetadata(topTemplateBytes)
	midTemplateBytes = stripSauceMetadata(midTemplateBytes)
	botTemplateBytes = stripSauceMetadata(botTemplateBytes)

	// Normalize delimiters and process pipe codes in templates first.
	// Some ANSI/ASCII assets may use broken bar (¦) instead of literal pipe (|).
	topTemplateBytes = normalizePipeCodeDelimiters(topTemplateBytes)
	midTemplateBytes = normalizePipeCodeDelimiters(midTemplateBytes)
	botTemplateBytes = normalizePipeCodeDelimiters(botTemplateBytes)

	processedTopTemplate := string(ansi.ReplacePipeCodes(topTemplateBytes))
	processedMidTemplate := string(ansi.ReplacePipeCodes(midTemplateBytes)) // Process MID template
	processedBotTemplate := string(ansi.ReplacePipeCodes(botTemplateBytes))
	// --- END Template Processing ---

	// 2. Get last callers data from UserManager
	lastCallers := userManager.GetLastCallers()
	// Filter out invisible call records for non-CoSysOp viewers
	if !e.isCoSysOpOrAbove(currentUser) {
		filtered := make([]user.CallRecord, 0, len(lastCallers))
		for _, rec := range lastCallers {
			if !rec.Invisible {
				filtered = append(filtered, rec)
			}
		}
		lastCallers = filtered
	}
	users := userManager.GetAllUsers()
	totalUsers := len(users)
	userNotesByID := make(map[int]string, len(users))
	for _, userRecord := range users {
		if userRecord == nil {
			continue
		}
		userNotesByID[userRecord.ID] = userRecord.PrivateNote
	}
	timeLoc := getLastCallerTimeLocation(strings.TrimSpace(e.ServerCfg.Timezone))
	if callerLimit > 0 && len(lastCallers) > callerLimit {
		lastCallers = lastCallers[len(lastCallers)-callerLimit:]
	}

	processedTopTemplate = renderLastCallerGlobalATTokens(processedTopTemplate, totalUsers)
	processedBotTemplate = renderLastCallerGlobalATTokens(processedBotTemplate, totalUsers)
	usersOnline := strconv.Itoa(e.SessionRegistry.ActiveCount())
	processedTopTemplate = strings.ReplaceAll(processedTopTemplate, "@U@", usersOnline)
	processedBotTemplate = strings.ReplaceAll(processedBotTemplate, "@U@", usersOnline)

	// 3. Build the output string using processed templates and processed data
	var outputBuffer bytes.Buffer
	outputBuffer.WriteString(processedTopTemplate) // Write processed top template
	if !strings.HasSuffix(processedTopTemplate, "\r\n") && !strings.HasSuffix(processedTopTemplate, "\n") {
		outputBuffer.WriteString("\r\n")
	}

	if len(lastCallers) == 0 {
		// Optional: Handle empty state. The template might handle this.
		slog.Debug("no last callers to display", "node", nodeNumber)
		// If templates don't handle empty, add a message here.
	} else {
		// Iterate through call records and format using processed LASTCALL.MID
		for _, record := range lastCallers {
			line := processedMidTemplate // Start with the pipe-code-processed mid template
			userNote := string(ansi.ReplacePipeCodes([]byte(userNotesByID[record.UserID])))

			// Format data for substitution with fixed-width padding for column alignment
			baud := record.BaudRate
			name := string(ansi.ReplacePipeCodes([]byte(record.Handle)))
			groupLoc := string(ansi.ReplacePipeCodes([]byte(record.GroupLocation)))
			onTime := formatLastCallerShortLocalTime(record.ConnectTime, timeLoc)
			actions := record.Actions
			hours := int(record.Duration.Hours())
			mins := int(record.Duration.Minutes()) % 60
			hmm := fmt.Sprintf("%d:%02d", hours, mins)
			upM := fmt.Sprintf("%.1f", record.UploadedMB)
			dnM := fmt.Sprintf("%.1f", record.DownloadedMB)
			nodeStr := strconv.Itoa(record.NodeID)
			callNumStr := strconv.FormatUint(record.CallNumber, 10)

			// Replace placeholders with padded data to match header column widths.
			// Header: " # |  Node |  Handle           | Baud         | Group/Affil"
			// Widths:   3     7      19                  14             rest
			// All spacing is in the padding — template has no extra spaces.
			line = strings.ReplaceAll(line, "^CN", fmt.Sprintf(" %-2s", callNumStr)) // 3 chars
			line = strings.ReplaceAll(line, "^ND", fmt.Sprintf("  %-5s", nodeStr))   // 7 chars
			line = strings.ReplaceAll(line, "^UN", fmt.Sprintf("  %-17s", name))     // 19 chars
			line = strings.ReplaceAll(line, "^BA", fmt.Sprintf(" %-13s", baud))      // 14 chars
			line = strings.ReplaceAll(line, "^GL", fmt.Sprintf(" %s", groupLoc))
			line = strings.ReplaceAll(line, "^OT", fmt.Sprintf("%-8s", onTime))
			line = strings.ReplaceAll(line, "^AC", actions)
			line = strings.ReplaceAll(line, "^HM", fmt.Sprintf("%-5s", hmm))
			line = strings.ReplaceAll(line, "^UM", fmt.Sprintf("%-6s", upM))
			line = strings.ReplaceAll(line, "^DM", fmt.Sprintf("%-6s", dnM))
			line = strings.ReplaceAll(line, "^NT", userNote)
			line = renderLastCallerATTokens(line, record, totalUsers, userNote, timeLoc)

			line = strings.TrimRight(line, "\r\n") + "\r\n"
			outputBuffer.WriteString(line) // Add the fully substituted and processed line
		}
	}

	outputBuffer.WriteString(processedBotTemplate) // Write processed bottom template

	// 4. Clear screen and display the assembled content
	writeErr := terminalio.WriteProcessedBytes(terminal, []byte(ansi.ClearScreen()), outputMode)
	if writeErr != nil {
		slog.Error("failed clearing screen for LASTCALLERS", "node", nodeNumber, "error", writeErr)
		return nil, "", writeErr
	}

	// Use WriteProcessedBytes for the assembled template content
	processedContent := outputBuffer.Bytes() // Contains already-processed ANSI bytes
	// For CP437 mode with raw ANSI content, write bytes directly to avoid UTF-8 decode artifacts
	var wErr error
	if outputMode == ansi.OutputModeCP437 {
		_, wErr = terminal.Write(processedContent)
	} else {
		wErr = terminalio.WriteProcessedBytes(terminal, processedContent, outputMode)
	}
	if wErr != nil {
		slog.Error("failed writing LASTCALLERS output", "node", nodeNumber, "error", wErr)
		return nil, "", wErr
	}

	// 5. Wait for Enter using configured PauseString
	pausePrompt := e.LoadedStrings.PauseString
	if pausePrompt == "" {
		pausePrompt = "\r\n|07Press |15[ENTER]|07 to continue... " // Fallback
	}

	slog.Debug("displaying LASTCALLERS pause prompt (centered)", "node", nodeNumber)
	err := writeCenteredPausePrompt(s, terminal, pausePrompt, outputMode, termWidth, termHeight)
	if err != nil {
		if errors.Is(err, io.EOF) {
			slog.Info("user disconnected during LASTCALLERS pause", "node", nodeNumber)
			return nil, "LOGOFF", io.EOF
		}
		slog.Error("failed during LASTCALLERS pause", "node", nodeNumber, "error", err)
		return nil, "", err
	}

	return nil, "", nil // Success
}
