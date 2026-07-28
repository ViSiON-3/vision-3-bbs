package menu

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/term"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// displayOnelinerScreen renders the ONELINER TOP/MID/BOT templates and the
// current one-liner records (template load through BOT write, verbatim from
// the original runOneliners body).
func displayOnelinerScreen(e *MenuExecutor, terminal *term.Terminal, outputMode ansi.OutputMode, nodeNumber int, records []onelinerRecord, startIdx int) error {
	numLiners := len(records)

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
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		time.Sleep(1 * time.Second)
		return fmt.Errorf("failed loading ONELINER templates")
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
		return wErr
	}

	if numLiners == 0 {
		line := strings.ReplaceAll(midTemplateRaw, "^NU", formatOnelinerDisplayName("System"))
		line = strings.ReplaceAll(line, "^OL", "No one-liners yet. Be the first!")
		line = "    " + line
		lineBytes := ansi.ReplacePipeCodes([]byte(line))
		wErr = terminalio.WriteProcessedBytes(terminal, lineBytes, outputMode)
		if wErr != nil {
			slog.Error("failed writing empty oneliner state", "node", nodeNumber, "error", wErr)
			return wErr
		}
	} else {
		anonymousName := strings.TrimSpace(e.LoadedStrings.AnonymousName)
		if anonymousName == "" {
			anonymousName = "Anonymous"
		}
		for i := startIdx; i < numLiners; i++ {
			record := records[i]
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
				return wErr
			}
		}
	}

	wErr = terminalio.WriteProcessedBytes(terminal, processedBotTemplate, outputMode)
	if wErr != nil {
		slog.Error("failed writing ONELINER bottom template", "node", nodeNumber, "error", wErr)
		return wErr
	}

	return nil
}

// promptAddOneliner handles the "add a new one-liner" flow: optional
// anonymous prompt, text entry, and saving the new record (verbatim from the
// original runOneliners `if addYes` body).
func promptAddOneliner(c *cmdCtx, currentOneLiners []onelinerRecord, onelinerPath string) (*user.User, string, error) {
	e := c.e
	s := c.s
	terminal := c.terminal
	currentUser := c.currentUser
	nodeNumber := c.nodeNumber
	outputMode := c.outputMode
	termWidth := c.termWidth
	termHeight := c.termHeight

	allowAnon := currentUser != nil && currentUser.AccessLevel >= e.ServerCfg.AnonymousLevel
	isAnonymous := false
	if allowAnon {
		anonPrompt := e.LoadedStrings.OneLinerAnonymousPrompt
		if anonPrompt == "" {
			anonPrompt = "|09Post this one-liner as |08[|15A|08]nonymous|09? @"
		}
		// Start anonymous prompt at column 1 to avoid inherited indentation.
		wErr := terminalio.WriteProcessedBytes(terminal, []byte("\r\x1b[2K"), outputMode)
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
	terminalio.WriteProcessedBytes(terminal, []byte(ansi.SaveCursor()), outputMode)
	// Clear legend row, detected input row, and prompt row fallback.
	posClearCmd := fmt.Sprintf("\x1b[%d;1H\x1b[2K\x1b[%d;1H\x1b[2K\x1b[%d;1H\x1b[2K\x1b[%d;1H", legendRow, inputRow, promptRow, inputRow)
	terminalio.WriteProcessedBytes(terminal, []byte(posClearCmd), outputMode)

	if legendText != "" {
		legendText = truncatePipeCodedText(legendText, promptColWidth)
		legendPosCmd := fmt.Sprintf("\x1b[%d;1H", legendRow)
		wErr := terminalio.WriteProcessedBytes(terminal, []byte(legendPosCmd), outputMode)
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
	wErr := terminalio.WriteProcessedBytes(terminal, enterPromptBytes, outputMode)
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

	return nil, "", nil
}
