package menu

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// runSearchFiles prompts for a search string, searches filenames and descriptions
// across all file areas (respecting ACS), and displays paginated results.
func runSearchFiles(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	s := c.s
	terminal := c.terminal
	currentUser := c.currentUser
	nodeNumber := c.nodeNumber
	sessionStartTime := c.sessionStartTime
	outputMode := c.outputMode
	termWidth := c.termWidth
	termHeight := c.termHeight

	slog.Debug("running SEARCH_FILES", "node", nodeNumber)

	if currentUser == nil {
		return nil, "", nil
	}

	// Prompt for search text
	terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.LoadedStrings.SearchFilesPrompt)), outputMode)

	input, err := readLineFromSessionIH(s, terminal)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, "LOGOFF", io.EOF
		}
		return currentUser, "", fmt.Errorf("failed reading search input: %w", err)
	}

	query := strings.TrimSpace(input)
	if query == "" {
		return currentUser, "", nil
	}

	if len(query) < 3 {
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.LoadedStrings.SearchFilesMinChars)), outputMode)
		return currentUser, "", nil
	}

	slog.Debug("search files query", "node", nodeNumber, "query", query)

	results := e.FileMgr.SearchFiles(query)
	slog.Debug("search files raw results", "node", nodeNumber, "count", len(results))

	// Filter by area ACS and build display lines
	type searchResult struct {
		areaTag     string
		filename    string
		size        int64
		description string
	}

	var filtered []searchResult
	for _, rec := range results {
		area, ok := e.FileMgr.GetAreaByID(rec.AreaID)
		if !ok {
			continue
		}
		if !checkACS(area.ACSList, currentUser, s, terminal, sessionStartTime) {
			continue
		}
		filtered = append(filtered, searchResult{
			areaTag:     area.Tag,
			filename:    rec.Filename,
			size:        rec.Size,
			description: rec.Description,
		})
	}

	if len(filtered) == 0 {
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.LoadedStrings.SearchNoResults)), outputMode)
		return currentUser, "", nil
	}

	// Display header
	header := fmt.Sprintf(e.LoadedStrings.SearchResultsHeader, query)
	terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(header)), outputMode)

	// Paginate results: leave room for header/pause prompt
	linesPerPage := termHeight - 3
	if linesPerPage < 5 {
		linesPerPage = 5
	}

	lineCount := 0
	for _, r := range filtered {
		fname := r.filename
		if len(fname) > 12 {
			fname = fname[:12]
		}

		line := fmt.Sprintf("\r\n|09%-8s |15%-12s |07%5dk |14%s", r.areaTag, fname, (r.size+1023)/1024, r.description)
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(line)), outputMode)
		lineCount++

		if lineCount >= linesPerPage {
			if err := writeCenteredPausePrompt(s, terminal, e.LoadedStrings.PauseString, outputMode, termWidth, termHeight); err != nil {
				return currentUser, "", nil
			}
			lineCount = 0
		}
	}

	terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(
		fmt.Sprintf(e.LoadedStrings.SearchResultsSummary, len(filtered)),
	)), outputMode)

	writeCenteredPausePrompt(s, terminal, e.LoadedStrings.PauseString, outputMode, termWidth, termHeight)
	return currentUser, "", nil
}
