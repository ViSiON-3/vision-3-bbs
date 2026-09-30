package menu

import (
	"bytes"
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

// runLastCallers displays the last callers list using templates.
func runLastCallers(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	s := c.s
	terminal := c.terminal
	userManager := c.userManager
	nodeNumber := c.nodeNumber
	outputMode := c.outputMode
	termWidth := c.termWidth
	termHeight := c.termHeight

	slog.Debug("running LASTCALLERS", "node", nodeNumber)

	// Parse optional caller count argument (e.g., RUN:LASTCALLERS 25).
	// An explicit argument overrides the default in either direction; either
	// way the count is trimmed below to what fits on the caller's terminal.
	callerLimit := defaultLastCallerRows
	if strings.TrimSpace(args) != "" {
		if parsedLimit, parseErr := strconv.Atoi(strings.TrimSpace(args)); parseErr == nil && parsedLimit > 0 {
			callerLimit = parsedLimit
		}
	}

	// 1. Load Template Files from MenuSetPath/templates
	topTemplatePath := e.templateFile("LASTCALL.TOP")
	midTemplatePath := e.templateFile("LASTCALL.MID")
	botTemplatePath := e.templateFile("LASTCALL.BOT")

	topTemplateBytes, errTop := readTemplateFile(topTemplatePath)
	midTemplateBytes, errMid := readTemplateFile(midTemplatePath)
	botTemplateBytes, errBot := readTemplateFile(botTemplatePath)

	if errTop != nil || errMid != nil || errBot != nil {
		slog.Error("failed to load LASTCALL template files", "node", nodeNumber, "top", errTop, "mid", errMid, "bot", errBot)
		msg := e.Strings().ExecLastcallTemplateErr
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		uiPause(1 * time.Second)
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
	lastCallers := visibleCallRecords(userManager.GetLastCallers())
	users := userManager.GetAllUsers()
	totalUsers := len(users)
	userNotesByID := make(map[int]string, len(users))
	for _, userRecord := range users {
		if userRecord == nil {
			continue
		}
		userNotesByID[userRecord.ID] = userRecord.PrivateNote
	}
	timeLoc := getLastCallerTimeLocation(strings.TrimSpace(e.GetServerConfig().Timezone))

	processedTopTemplate = renderLastCallerGlobalATTokens(processedTopTemplate, totalUsers)
	processedBotTemplate = renderLastCallerGlobalATTokens(processedBotTemplate, totalUsers)
	usersOnline := strconv.Itoa(e.activeNodeCount())
	processedTopTemplate = strings.ReplaceAll(processedTopTemplate, "@U@", usersOnline)
	processedBotTemplate = strings.ReplaceAll(processedBotTemplate, "@U@", usersOnline)

	pausePrompt := e.Strings().PauseString
	if pausePrompt == "" {
		pausePrompt = "\r\n|07Press |15[ENTER]|07 to continue... " // Fallback
	}

	// Trim the row count so the header is not scrolled off the top of the
	// screen by the footer and pause prompt.
	if fit := lastCallerRowsThatFit(termHeight, processedTopTemplate, processedMidTemplate, processedBotTemplate, pausePrompt); fit >= 0 && callerLimit > fit {
		slog.Debug("trimming LASTCALLERS rows to terminal height", "node", nodeNumber, "requested", callerLimit, "fit", fit, "termHeight", termHeight)
		callerLimit = fit
	}
	if callerLimit == 0 {
		lastCallers = nil // mostRecentCallRecords would read 0 as "no limit"
	} else {
		lastCallers = mostRecentCallRecords(lastCallers, callerLimit)
	}

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

// defaultLastCallerRows is how many callers the screen shows when the menu
// command passes no count. Hidden logins are filtered out before this limit is
// applied, so it is a count of real callers rather than of stored records.
const defaultLastCallerRows = 20

// lastCallerRowsThatFit returns how many callers can be drawn on a
// termHeight-row screen without scrolling the header off the top, given the
// processed top, per-caller and bottom templates and the pause prompt that
// follows them. A multi-line mid template costs its line count per caller.
// It returns -1 when the height is unknown, meaning no trimming should occur.
// The result is never negative otherwise: a screen too short for the frame
// shows no rows rather than a negative count, which would mean "no limit".
func lastCallerRowsThatFit(termHeight int, top, mid, bot, pausePrompt string) int {
	if termHeight <= 0 {
		return -1
	}
	// Rows used = line breaks emitted + 1 for the line the cursor ends on.
	// runLastCallers terminates the top template and every caller row with a
	// line break. writeCenteredPausePrompt emits one break before the prompt
	// only when the prompt does not start with one; a leading break is
	// stripped and not written, so it adds no row.
	breaks := strings.Count(top, "\n")
	if !strings.HasSuffix(top, "\n") {
		breaks++
	}
	breaks += strings.Count(bot, "\n")
	switch {
	case strings.HasPrefix(pausePrompt, "\r\n"):
		pausePrompt = strings.TrimPrefix(pausePrompt, "\r\n")
	case strings.HasPrefix(pausePrompt, "\n"):
		pausePrompt = strings.TrimPrefix(pausePrompt, "\n")
	default:
		breaks++
	}
	breaks += strings.Count(pausePrompt, "\n")

	// runLastCallers trims the mid template's trailing breaks and adds one.
	rowsPerCaller := strings.Count(strings.TrimRight(mid, "\r\n"), "\n") + 1

	free := termHeight - (breaks + 1)
	if free < 0 {
		return 0
	}
	return free / rowsPerCaller
}

// visibleCallRecords drops call records made by users who declined the
// "add this login to the last caller list?" prompt. The exclusion applies to
// every viewer, including the SysOp: the caller asked not to be listed, so the
// login must not appear on the screen no matter who is looking (issue #334).
func visibleCallRecords(records []user.CallRecord) []user.CallRecord {
	visible := make([]user.CallRecord, 0, len(records))
	for _, rec := range records {
		if !rec.Invisible {
			visible = append(visible, rec)
		}
	}
	return visible
}

// mostRecentCallRecords keeps the newest limit records, preserving the
// oldest-first order LASTCALL.MID renders. A limit of zero or less keeps
// everything. Callers must filter hidden records first so that the limit
// counts rows the viewer will actually see.
func mostRecentCallRecords(records []user.CallRecord, limit int) []user.CallRecord {
	if limit <= 0 || len(records) <= limit {
		return records
	}
	return records[len(records)-limit:]
}
