package menu

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

func truncateRunes(value string, max int) string {
	value = strings.TrimSpace(value)
	if max <= 0 || value == "" {
		return ""
	}
	if utf8.RuneCountInString(value) <= max {
		return value
	}
	runes := []rune(value)
	if max <= 3 {
		return string(runes[:max])
	}
	return string(runes[:max-3]) + "..."
}

func isPipeCodeStartChar(b byte) bool {
	return (b >= '0' && b <= '9') || (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z')
}

func pipeCodeLenAt(value string, index int) int {
	if index < 0 || index >= len(value) || value[index] != '|' || index+1 >= len(value) {
		return 0
	}

	// 3-char forms: |00..|15, |CR, |DE, |CL, |PP, |23
	if index+2 < len(value) {
		two := value[index+1 : index+3]
		if len(two) == 2 {
			if (two[0] >= '0' && two[0] <= '9') && (two[1] >= '0' && two[1] <= '9') {
				return 3
			}
			u := strings.ToUpper(two)
			if u == "CR" || u == "DE" || u == "CL" || u == "PP" {
				return 3
			}
		}
	}

	// Background forms: |B0..|B9 (3 chars), |B10..|B15 (4 chars)
	if index+2 < len(value) {
		if (value[index+1] == 'B' || value[index+1] == 'b') && (value[index+2] >= '0' && value[index+2] <= '9') {
			if index+3 < len(value) && (value[index+3] >= '0' && value[index+3] <= '9') {
				return 4 // |B10..|B15 (validated loosely)
			}
			return 3 // |B0..|B9
		}
	}

	// 2-char form: |P
	if index+1 < len(value) && (value[index+1] == 'P' || value[index+1] == 'p') {
		return 2
	}

	if index+1 < len(value) && isPipeCodeStartChar(value[index+1]) {
		return 0
	}

	return 0
}

func truncateOnelinerPreservePipeCodes(value string, maxVisible int) string {
	value = strings.TrimSpace(value)
	if value == "" || maxVisible <= 0 {
		return ""
	}

	var out strings.Builder
	visible := 0
	i := 0
	for i < len(value) {
		if value[i] == '|' {
			codeLen := pipeCodeLenAt(value, i)
			if codeLen > 0 && i+codeLen <= len(value) {
				out.WriteString(value[i : i+codeLen])
				i += codeLen
				continue
			}
		}

		r, size := utf8.DecodeRuneInString(value[i:])
		if r == utf8.RuneError && size == 1 {
			size = 1
		}
		if visible >= maxVisible {
			break
		}
		out.WriteString(value[i : i+size])
		visible++
		i += size
	}

	return strings.TrimSpace(out.String())
}

func truncatePipeCodedText(value string, maxVisible int) string {
	if value == "" || maxVisible <= 0 {
		return ""
	}

	var out strings.Builder
	visible := 0
	i := 0
	for i < len(value) {
		if value[i] == '|' && i+1 < len(value) && value[i+1] == '|' {
			if visible >= maxVisible {
				break
			}
			out.WriteString("||")
			visible++
			i += 2
			continue
		}

		if value[i] == '|' {
			codeLen := pipeCodeLenAt(value, i)
			if codeLen > 0 && i+codeLen <= len(value) {
				out.WriteString(value[i : i+codeLen])
				i += codeLen
				continue
			}
		}

		_, size := utf8.DecodeRuneInString(value[i:])
		if size <= 0 {
			size = 1
		}
		if visible >= maxVisible {
			break
		}
		out.WriteString(value[i : i+size])
		visible++
		i += size
	}

	return out.String()
}

func containsDisallowedOnelinerColorCode(value string) bool {
	i := 0
	for i < len(value) {
		if value[i] == '|' && i+1 < len(value) && value[i+1] == '|' {
			i += 2
			continue
		}

		if value[i] == '|' {
			codeLen := pipeCodeLenAt(value, i)
			if codeLen > 0 && i+codeLen <= len(value) {
				// Only standard foreground colors |01..|15 are allowed.
				if codeLen != 3 {
					return true
				}

				colorCode := value[i+1 : i+3]
				if colorCode < "01" || colorCode > "15" {
					return true
				}

				i += codeLen
				continue
			}
		}

		_, size := utf8.DecodeRuneInString(value[i:])
		if size <= 0 {
			size = 1
		}
		i += size
	}

	return false
}

func formatOnelinerDisplayName(name string) string {
	formatted := truncateRunes(name, oneLinerNameWidth)
	if formatted == "" {
		formatted = "Unknown"
	}
	padding := oneLinerNameWidth - utf8.RuneCountInString(formatted)
	if padding > 0 {
		formatted = strings.Repeat(" ", padding) + formatted
	}
	return formatted
}

func onelinerVisibleName(record onelinerRecord, anonymousName string) string {
	if strings.TrimSpace(anonymousName) == "" {
		anonymousName = "Anonymous"
	}
	if record.Anonymous {
		return anonymousName
	}
	if strings.TrimSpace(record.PostedByHandle) != "" {
		return record.PostedByHandle
	}
	if strings.TrimSpace(record.PostedByUsername) != "" {
		return record.PostedByUsername
	}
	return "Unknown"
}

func loadOnelinerRecords(onelinerPath string) ([]onelinerRecord, error) {
	jsonData, readErr := os.ReadFile(onelinerPath)
	if readErr != nil {
		if os.IsNotExist(readErr) {
			return []onelinerRecord{}, nil
		}
		return nil, readErr
	}

	if strings.TrimSpace(string(jsonData)) == "" {
		return []onelinerRecord{}, nil
	}

	var rawEntries []json.RawMessage
	if err := json.Unmarshal(jsonData, &rawEntries); err != nil {
		return nil, err
	}

	records := make([]onelinerRecord, 0, len(rawEntries))
	for _, raw := range rawEntries {
		var legacyText string
		if err := json.Unmarshal(raw, &legacyText); err == nil {
			legacyText = truncateOnelinerPreservePipeCodes(legacyText, oneLinerMaxLength)
			if legacyText != "" {
				records = append(records, onelinerRecord{
					Text:             legacyText,
					PostedByUsername: "Unknown",
				})
			}
			continue
		}

		var compat onelinerRecordCompat
		if err := json.Unmarshal(raw, &compat); err != nil {
			continue
		}

		record := onelinerRecord{
			Text:             truncateOnelinerPreservePipeCodes(compat.Text, oneLinerMaxLength),
			Anonymous:        compat.Anonymous,
			PostedByUsername: strings.TrimSpace(compat.PostedByUsername),
			PostedByHandle:   strings.TrimSpace(compat.PostedByHandle),
			PostedAt:         compat.PostedAt,
		}

		if record.PostedByUsername == "" {
			record.PostedByUsername = strings.TrimSpace(compat.Username)
		}
		if record.PostedByHandle == "" {
			if strings.TrimSpace(compat.DisplayName) != "" && !record.Anonymous {
				record.PostedByHandle = strings.TrimSpace(compat.DisplayName)
			} else if strings.TrimSpace(compat.Username) != "" {
				record.PostedByHandle = strings.TrimSpace(compat.Username)
			}
		}

		if record.Text == "" {
			continue
		}

		records = append(records, record)
	}

	return records, nil
}

func saveOnelinerRecords(onelinerPath string, records []onelinerRecord) error {
	if len(records) > oneLinerMaxStored {
		records = records[len(records)-oneLinerMaxStored:]
	}

	updatedJSON, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(onelinerPath, updatedJSON, 0644)
}

// runOneliners displays the oneliners using templates.
func runOneliners(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	s := c.s
	terminal := c.terminal
	currentUser := c.currentUser
	nodeNumber := c.nodeNumber
	outputMode := c.outputMode
	termWidth := c.termWidth
	termHeight := c.termHeight

	slog.Debug("running ONELINER", "node", nodeNumber)

	onelinerPath := filepath.Join("data", "oneliners.json")

	var currentOneLiners []onelinerRecord
	onelinerMutex.Lock()
	loadedOneLiners, loadErr := loadOnelinerRecords(onelinerPath)
	onelinerMutex.Unlock()
	if loadErr != nil {
		slog.Error("failed loading oneliners", "path", onelinerPath, "error", loadErr)
		currentOneLiners = []onelinerRecord{}
	} else {
		currentOneLiners = loadedOneLiners
	}
	slog.Debug("loaded oneliners", "count", len(currentOneLiners), "path", onelinerPath)

	numLiners := len(currentOneLiners)
	maxLinesToShow := oneLinerMaxDisplay
	startIdx := 0
	if numLiners > maxLinesToShow {
		startIdx = numLiners - maxLinesToShow
	}

	// 1. Load template files (same flow as LASTCALLERS)
	topTemplatePath := filepath.Join(e.MenuSetPath, "templates", "ONELINER.TOP")
	midTemplatePath := filepath.Join(e.MenuSetPath, "templates", "ONELINER.MID")
	botTemplatePath := filepath.Join(e.MenuSetPath, "templates", "ONELINER.BOT")

	topTemplateBytes, errTop := readTemplateFile(topTemplatePath)
	midTemplateBytes, errMid := readTemplateFile(midTemplatePath)
	botTemplateBytes, errBot := readTemplateFile(botTemplatePath)
	if errTop != nil || errMid != nil || errBot != nil {
		slog.Error("failed to load one or more ONELINER template files", "node", nodeNumber, "topError", errTop, "midError", errMid, "botError", errBot)
		msg := e.LoadedStrings.ExecOnelinerTemplateErr
		wErr := terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		if wErr != nil {
		}
		time.Sleep(1 * time.Second)
		return nil, "", fmt.Errorf("failed loading ONELINER templates")
	}

	// Strip SAUCE metadata and normalize broken bar delimiters, matching LASTCALLERS behavior.
	topTemplateBytes = stripSauceMetadata(topTemplateBytes)
	midTemplateBytes = stripSauceMetadata(midTemplateBytes)
	botTemplateBytes = stripSauceMetadata(botTemplateBytes)

	topTemplateBytes = normalizePipeCodeDelimiters(topTemplateBytes)
	midTemplateBytes = normalizePipeCodeDelimiters(midTemplateBytes)
	botTemplateBytes = normalizePipeCodeDelimiters(botTemplateBytes)

	processedTopTemplate := ansi.ReplacePipeCodes(topTemplateBytes)
	midTemplateRaw := string(midTemplateBytes)
	processedBotTemplate := ansi.ReplacePipeCodes(botTemplateBytes)

	wErr := terminalio.WriteProcessedBytes(terminal, []byte(ansi.ClearScreen()), outputMode)
	if wErr != nil {
		slog.Error("failed clearing screen for ONELINER", "node", nodeNumber, "error", wErr)
	}

	wErr = terminalio.WriteProcessedBytes(terminal, processedTopTemplate, outputMode)
	if wErr != nil {
		slog.Error("failed writing ONELINER top template", "node", nodeNumber, "error", wErr)
		return nil, "", wErr
	}

	if numLiners == 0 {
		line := strings.ReplaceAll(midTemplateRaw, "^NU", formatOnelinerDisplayName("System"))
		line = strings.ReplaceAll(line, "^OL", "No one-liners yet. Be the first!")
		line = "    " + line
		lineBytes := ansi.ReplacePipeCodes([]byte(line))
		wErr = terminalio.WriteProcessedBytes(terminal, lineBytes, outputMode)
		if wErr != nil {
			slog.Error("failed writing empty oneliner state", "node", nodeNumber, "error", wErr)
			return nil, "", wErr
		}
	} else {
		anonymousName := strings.TrimSpace(e.LoadedStrings.AnonymousName)
		if anonymousName == "" {
			anonymousName = "Anonymous"
		}
		for i := startIdx; i < numLiners; i++ {
			record := currentOneLiners[i]
			displayName := onelinerVisibleName(record, anonymousName)
			displayName = formatOnelinerDisplayName(displayName)
			messageText := truncateOnelinerPreservePipeCodes(record.Text, oneLinerMaxLength)

			line := strings.ReplaceAll(midTemplateRaw, "^NU", displayName)
			line = strings.ReplaceAll(line, "^OL", messageText)
			line = "    " + line

			lineBytes := ansi.ReplacePipeCodes([]byte(line))
			wErr = terminalio.WriteProcessedBytes(terminal, lineBytes, outputMode)
			if wErr != nil {
				slog.Error("failed writing oneliner line", "node", nodeNumber, "line", i, "error", wErr)
				return nil, "", wErr
			}
		}
	}

	wErr = terminalio.WriteProcessedBytes(terminal, processedBotTemplate, outputMode)
	if wErr != nil {
		slog.Error("failed writing ONELINER bottom template", "node", nodeNumber, "error", wErr)
		return nil, "", wErr
	}
	// --- Ask to Add New One ---
	askPrompt := e.LoadedStrings.AskOneLiner
	if askPrompt == "" {
		slog.Error("required string 'AskOneLiner' is missing or empty in strings configuration")
		return nil, "", fmt.Errorf("missing AskOneLiner string in configuration")
	}

	// Position the prompt on the last row of the terminal.
	if termHeight > 0 {
		lastRow := termHeight
		posCmd := fmt.Sprintf("\x1b[%d;1H", lastRow)
		wErr = terminalio.WriteProcessedBytes(terminal, []byte(posCmd), outputMode)
		if wErr != nil {
			slog.Warn("failed positioning cursor for ONELINER ask prompt", "node", nodeNumber, "error", wErr)
		}
	}

	slog.Debug("calling promptYesNo for ONELINER add prompt", "node", nodeNumber)
	addYes, err := e.PromptYesNo(s, terminal, askPrompt, outputMode, nodeNumber, termWidth, termHeight, false)
	if err != nil {
		if errors.Is(err, io.EOF) {
			slog.Info("user disconnected during ONELINER add prompt", "node", nodeNumber)
			return nil, "LOGOFF", io.EOF
		}
		slog.Error("failed getting Yes/No input for ONELINER add", "error", err)
		return nil, "", err
	}

	if addYes {
		allowAnon := currentUser != nil && currentUser.AccessLevel >= e.ServerCfg.AnonymousLevel
		isAnonymous := false
		if allowAnon {
			anonPrompt := e.LoadedStrings.OneLinerAnonymousPrompt
			if anonPrompt == "" {
				anonPrompt = "|09Post this one-liner as |08[|15A|08]nonymous|09? @"
			}
			// Start anonymous prompt at column 1 to avoid inherited indentation.
			wErr = terminalio.WriteProcessedBytes(terminal, []byte("\r\x1b[2K"), outputMode)
			if wErr != nil {
				slog.Warn("failed to clear line before ONELINER anonymous prompt", "node", nodeNumber, "error", wErr)
			}
			anonYes, anonErr := e.PromptYesNo(s, terminal, anonPrompt, outputMode, nodeNumber, termWidth, termHeight, false)
			if anonErr != nil {
				if errors.Is(anonErr, io.EOF) {
					slog.Info("user disconnected during ONELINER anonymous prompt", "node", nodeNumber)
					return nil, "LOGOFF", io.EOF
				}
				slog.Warn("failed anonymous prompt for ONELINER", "node", nodeNumber, "error", anonErr)
			} else {
				isAnonymous = anonYes
			}
		}

		enterPrompt := e.LoadedStrings.EnterOneLiner
		if enterPrompt == "" {
			slog.Error("required string 'EnterOneLiner' is missing or empty in strings configuration")
			return nil, "", fmt.Errorf("missing EnterOneLiner string in configuration")
		}

		promptRow := 23
		promptColWidth := 80
		if termHeight > 0 {
			promptRow = termHeight
		}
		if termWidth > 0 {
			promptColWidth = termWidth
		}

		// Use the known prompt row directly. requestCursorPosition sends a DSR
		// (\x1b[6n) and tries to read the response via a raw bufio.Reader on the
		// session, but the session input is already consumed by the shared
		// InputHandler goroutine. The blocking ReadByte call inside a
		// select/default also prevents the timeout from ever firing, causing the
		// screen to freeze until the user presses a key. Since we already know
		// termHeight, just compute the position.
		inputRow := promptRow

		legendText := strings.TrimSpace(e.LoadedStrings.OneLinerLegend)
		legendRow := inputRow - 1
		if legendRow < 1 {
			legendRow = 1
		}

		// Use WriteProcessedBytes for SaveCursor, positioning, and clear line
		wErr = terminalio.WriteProcessedBytes(terminal, []byte(ansi.SaveCursor()), outputMode)
		if wErr != nil { /* Log? */
		}
		// Clear legend row, detected input row, and prompt row fallback.
		posClearCmd := fmt.Sprintf("\x1b[%d;1H\x1b[2K\x1b[%d;1H\x1b[2K\x1b[%d;1H\x1b[2K\x1b[%d;1H", legendRow, inputRow, promptRow, inputRow)
		wErr = terminalio.WriteProcessedBytes(terminal, []byte(posClearCmd), outputMode)
		if wErr != nil { /* Log? */
		}

		if legendText != "" {
			legendText = truncatePipeCodedText(legendText, promptColWidth)
			legendPosCmd := fmt.Sprintf("\x1b[%d;1H", legendRow)
			wErr = terminalio.WriteProcessedBytes(terminal, []byte(legendPosCmd), outputMode)
			if wErr != nil {
				slog.Warn("failed positioning ONELINER legend row", "node", nodeNumber, "error", wErr)
			}

			legendBytes := ansi.ReplacePipeCodes([]byte(legendText))
			wErr = terminalio.WriteProcessedBytes(terminal, legendBytes, outputMode)
			if wErr != nil {
				slog.Warn("failed writing ONELINER legend", "node", nodeNumber, "error", wErr)
			}

			wErr = terminalio.WriteProcessedBytes(terminal, []byte(fmt.Sprintf("\x1b[%d;1H", inputRow)), outputMode)
			if wErr != nil {
				slog.Warn("failed restoring ONELINER input row after legend", "node", nodeNumber, "error", wErr)
			}
		}

		enterPromptBytes := ansi.ReplacePipeCodes([]byte(enterPrompt))
		slog.Debug("writing oneliner enter prompt bytes", "node", nodeNumber, "bytes", fmt.Sprintf("%X", enterPromptBytes))
		wErr = terminalio.WriteProcessedBytes(terminal, enterPromptBytes, outputMode)
		if wErr != nil {
			slog.Error("failed writing EnterOneLiner prompt", "node", nodeNumber, "error", wErr)
		}

		newOneliner, err := readLineFromSessionIH(s, terminal)
		if err != nil {
			if errors.Is(err, io.EOF) {
				slog.Info("user disconnected while entering oneliner", "node", nodeNumber)
				return nil, "LOGOFF", io.EOF
			}
			slog.Error("failed reading new oneliner input", "error", err)
			return nil, "", err
		}
		newOneliner = truncateOnelinerPreservePipeCodes(newOneliner, oneLinerMaxLength)
		if containsDisallowedOnelinerColorCode(newOneliner) {
			msg := e.LoadedStrings.ExecOnelinerColorError
			terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
			time.Sleep(500 * time.Millisecond)
			return nil, "", nil
		}

		if newOneliner != "" {
			postedByHandle := ""
			if currentUser != nil {
				postedByHandle = currentUser.Handle
			}
			if strings.TrimSpace(postedByHandle) == "" {
				postedByHandle = "Unknown"
			}

			entry := onelinerRecord{
				Text:             newOneliner,
				Anonymous:        isAnonymous,
				PostedByUsername: postedByHandle,
				PostedByHandle:   postedByHandle,
				PostedAt:         time.Now().UTC().Format(time.RFC3339),
			}

			onelinerMutex.Lock()
			latestOneLiners, latestErr := loadOnelinerRecords(onelinerPath)
			if latestErr != nil {
				slog.Warn("failed reloading oneliners before save", "node", nodeNumber, "error", latestErr)
				latestOneLiners = currentOneLiners
			}
			latestOneLiners = append(latestOneLiners, entry)
			saveErr := saveOnelinerRecords(onelinerPath, latestOneLiners)
			onelinerMutex.Unlock()

			if saveErr != nil {
				slog.Error("failed to write updated oneliners JSON", "node", nodeNumber, "path", onelinerPath, "error", saveErr)
				msg := e.LoadedStrings.ExecOnelinerWriteError
				terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
			} else {
				slog.Info("successfully saved updated oneliners", "node", nodeNumber, "path", onelinerPath)
				msg := e.LoadedStrings.ExecOnelinerAdded
				terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
				time.Sleep(500 * time.Millisecond)
			}
		} else {
			msg := e.LoadedStrings.ExecOnelinerEmpty
			terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
			time.Sleep(500 * time.Millisecond)
		}
	} // end if addYes

	return nil, "", nil
}
