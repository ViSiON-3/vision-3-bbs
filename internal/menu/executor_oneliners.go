package menu

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"

	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// runOneliners displays the oneliners using templates.
func runOneliners(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	s := c.s
	terminal := c.terminal
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

	if err := displayOnelinerScreen(e, terminal, outputMode, nodeNumber, currentOneLiners, startIdx); err != nil {
		return nil, "", err
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
		wErr := terminalio.WriteProcessedBytes(terminal, []byte(posCmd), outputMode)
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
		return promptAddOneliner(c, currentOneLiners, onelinerPath)
	}

	return nil, "", nil
}
