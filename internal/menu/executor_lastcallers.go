package menu

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

var lastCallerATTokenRegex = regexp.MustCompile(`@([A-Za-z]{2,12})(?::(-?\d+))?@`)

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
		wErr := terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		if wErr != nil { /* Log? */
		}
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

func renderLastCallerATTokens(template string, record user.CallRecord, totalUsers int, userNote string, timeLoc *time.Location) string {
	return lastCallerATTokenRegex.ReplaceAllStringFunc(template, func(match string) string {
		parts := lastCallerATTokenRegex.FindStringSubmatch(match)
		if len(parts) < 2 {
			return match
		}

		code := strings.ToUpper(parts[1])
		value, ok := lastCallerATTokenValue(code, record, totalUsers, userNote, timeLoc)
		if !ok {
			return match
		}

		if len(parts) > 2 && parts[2] != "" {
			if width, err := strconv.Atoi(parts[2]); err == nil {
				if isLastCallerATCenterAligned(code) {
					value = formatLastCallerATWidthCentered(value, width)
				} else {
					value = formatLastCallerATWidth(value, width, isLastCallerATNumeric(code))
				}
			}
		}

		return value
	})
}

func renderLastCallerGlobalATTokens(template string, totalUsers int) string {
	return lastCallerATTokenRegex.ReplaceAllStringFunc(template, func(match string) string {
		parts := lastCallerATTokenRegex.FindStringSubmatch(match)
		if len(parts) < 2 {
			return match
		}

		code := strings.ToUpper(parts[1])
		if code != "UC" && code != "USERCT" {
			return match
		}

		value := strconv.Itoa(totalUsers)
		if len(parts) > 2 && parts[2] != "" {
			if width, err := strconv.Atoi(parts[2]); err == nil {
				value = formatLastCallerATWidth(value, width, true)
			}
		}

		return value
	})
}

func normalizePipeCodeDelimiters(input []byte) []byte {
	if len(input) == 0 {
		return input
	}

	// Only normalize likely pipe-code delimiters (e.g. ¦CR, ¦08, │DE).
	// Do NOT blanket-convert ANSI line-art bytes (such as CP437 0xB3), which
	// can corrupt imported art templates.
	normalized := make([]byte, 0, len(input))

	isPipeCodeLead := func(b byte) bool {
		return (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9')
	}

	for i := 0; i < len(input); {
		replaced := false

		// UTF-8 broken bar (U+00A6 => 0xC2 0xA6)
		if i+1 < len(input) && input[i] == 0xC2 && input[i+1] == 0xA6 {
			if i+2 < len(input) && isPipeCodeLead(input[i+2]) {
				normalized = append(normalized, '|')
				i += 2
				replaced = true
			}
		}

		if !replaced {
			// UTF-8 box drawing light vertical (U+2502 => 0xE2 0x94 0x82)
			if i+2 < len(input) && input[i] == 0xE2 && input[i+1] == 0x94 && input[i+2] == 0x82 {
				if i+3 < len(input) && isPipeCodeLead(input[i+3]) {
					normalized = append(normalized, '|')
					i += 3
					replaced = true
				}
			}
		}

		if !replaced {
			// Raw single-byte broken bar (0xA6)
			if input[i] == 0xA6 {
				if i+1 < len(input) && isPipeCodeLead(input[i+1]) {
					normalized = append(normalized, '|')
					i++
					replaced = true
				}
			}
		}

		if !replaced {
			normalized = append(normalized, input[i])
			i++
		}
	}

	return normalized
}

// readTemplateFile reads a template file at path, trying the base path first,
// then path+".ANS", then path+".ans", so files saved by ANSI editors (which
// typically append the uppercase .ANS extension) are recognised automatically.
// SAUCE metadata is stripped from the returned content.
func readTemplateFile(path string) ([]byte, error) {
	var (
		data []byte
		err  error
	)
	for _, candidate := range []string{path, path + ".ANS", path + ".ans"} {
		data, err = os.ReadFile(candidate)
		if err == nil {
			return stripSauceMetadata(data), nil
		}
		if !os.IsNotExist(err) {
			// Real I/O error (permissions, etc.) — stop trying.
			return nil, err
		}
	}
	return nil, err
}

func stripSauceMetadata(input []byte) []byte {
	if len(input) < 7 {
		return input
	}

	idx := bytes.LastIndex(input, []byte("SAUCE00"))
	if idx < 0 {
		return input
	}

	// SAUCE record should be near EOF; ignore stray in-body text matches.
	if idx < len(input)-512 {
		return input
	}

	cut := idx

	// If full SAUCE record is present, remove optional COMNT block too.
	if idx+128 <= len(input) {
		comments := int(input[idx+104])
		if comments > 0 {
			commentLen := 5 + (comments * 64)
			commentStart := idx - commentLen
			if commentStart >= 0 && bytes.Equal(input[commentStart:commentStart+5], []byte("COMNT")) {
				cut = commentStart
			}
		}
	}

	// Remove CP/M EOF marker if present before metadata.
	if cut > 0 && input[cut-1] == 0x1A {
		cut--
	}

	return input[:cut]
}

func lastCallerATTokenValue(code string, record user.CallRecord, totalUsers int, userNote string, timeLoc *time.Location) (string, bool) {
	switch code {
	case "UC", "USERCT":
		return strconv.Itoa(totalUsers), true
	case "NOTE", "NT":
		return userNote, true
	case "CA":
		return strconv.FormatUint(record.CallNumber, 10), true
	case "UN":
		return record.Handle, true
	case "LC":
		return record.GroupLocation, true
	case "ND":
		return strconv.Itoa(record.NodeID), true
	case "LO":
		if record.ConnectTime.IsZero() {
			return "", true
		}
		return formatLastCallerShortLocalTime(record.ConnectTime, timeLoc), true
	case "LT":
		if !record.DisconnectTime.IsZero() {
			return formatLastCallerShortLocalTime(record.DisconnectTime, timeLoc), true
		}
		if !record.ConnectTime.IsZero() {
			return formatLastCallerShortLocalTime(record.ConnectTime.Add(record.Duration), timeLoc), true
		}
		return "", true
	case "NU":
		if record.CallNumber <= 1 {
			return "*", true
		}
		return " ", true
	case "TO":
		return strconv.Itoa(int(record.Duration.Minutes())), true
	case "MP", "MR", "DL", "UL", "ES", "FS":
		return "0", true
	case "DK":
		return strconv.Itoa(int(record.DownloadedMB * 1024.0)), true
	case "UK":
		return strconv.Itoa(int(record.UploadedMB * 1024.0)), true
	default:
		return "", false
	}
}

func isLastCallerATNumeric(code string) bool {
	switch code {
	case "CA", "ND", "TO", "MP", "MR", "DL", "DK", "UL", "UK", "ES", "FS":
		return true
	default:
		return false
	}
}

func isLastCallerATCenterAligned(code string) bool {
	switch code {
	case "ND", "CA", "TO":
		return true
	default:
		return false
	}
}

// replaceMenuATCode replaces @CODE@, @CODE:N@, @CODE##…@, and modifier forms
// @CODE|L:N@, @CODE|R:N@, @CODE|C:N@ (plus ## visual-width variants) in raw
// ANSI content with the supplied value, applying width/padding when specified.
//
// Alignment modifiers (between | and width):
//
//	L = left-align (default), R = right-align, C = center
//
// Examples: @RR@, @RR:60@, @RR|C:60@, @RR|C##########@, @RR|R8@
func replaceMenuATCode(content []byte, code string, value string) []byte {
	pat := regexp.MustCompile(`@` + regexp.QuoteMeta(code) + `(?:\|([LRC])(\d+)?)?(?::(\d+)|(#+))?@`)
	return pat.ReplaceAllFunc(content, func(match []byte) []byte {
		parts := pat.FindSubmatch(match)
		// parts[1] = alignment modifier (L/R/C)
		// parts[2] = digits after modifier (e.g. @RR|C60@)
		// parts[3] = :N explicit width
		// parts[4] = ## visual width
		alignMode := ansi.AlignLeft
		if len(parts) > 1 && len(parts[1]) > 0 {
			alignMode = ansi.ParseAlignment(string(parts[1]))
		}

		width := 0
		if len(parts) > 2 && len(parts[2]) > 0 {
			// digits after modifier (e.g. @RR|R8@)
			width, _ = strconv.Atoi(string(parts[2]))
		} else if len(parts) > 3 && len(parts[3]) > 0 {
			// :N explicit width
			width, _ = strconv.Atoi(string(parts[3]))
		} else if len(parts) > 4 && len(parts[4]) > 0 {
			// ## visual width — total placeholder length
			width = len(match)
		}

		result := value
		if width > 0 {
			result = ansi.ApplyWidthConstraintAligned(value, width, alignMode)
		}
		return []byte(result)
	})
}

func formatLastCallerATWidth(value string, width int, alignRight bool) string {
	if width == 0 {
		return value
	}

	if width < 0 {
		width = -width
		alignRight = true
	}

	runes := []rune(value)
	if len(runes) > width {
		runes = runes[:width]
	}
	value = string(runes)

	padding := width - utf8.RuneCountInString(value)
	if padding <= 0 {
		return value
	}

	pad := strings.Repeat(" ", padding)
	if alignRight {
		return pad + value
	}
	return value + pad
}

func formatLastCallerATWidthCentered(value string, width int) string {
	if width == 0 {
		return value
	}

	if width < 0 {
		width = -width
	}

	runes := []rune(value)
	if len(runes) > width {
		runes = runes[:width]
	}
	value = string(runes)

	padding := width - utf8.RuneCountInString(value)
	if padding <= 0 {
		return value
	}

	left := padding / 2
	right := padding - left
	return strings.Repeat(" ", left) + value + strings.Repeat(" ", right)
}

func formatLastCallerShortLocalTime(t time.Time, timeLoc *time.Location) string {
	if t.IsZero() {
		return ""
	}
	if timeLoc == nil {
		timeLoc = time.Local
	}
	return t.In(timeLoc).Format("03:04pm")
}

func getLastCallerTimeLocation(configTZ string) *time.Location {
	return config.LoadTimezone(configTZ)
}
