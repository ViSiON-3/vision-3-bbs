package menu

import (
	"errors"
	"fmt"
	"io"
	"log"
	"strings"
	"time"

	"github.com/gliderlabs/ssh"
	"golang.org/x/term"

	"github.com/stlalpha/vision3/internal/ansi"
	"github.com/stlalpha/vision3/internal/terminalio"
	"github.com/stlalpha/vision3/internal/user"
)

// runSearchFiles prompts for a search string, searches filenames and descriptions
// across all file areas (respecting ACS), and displays paginated results.
func runSearchFiles(e *MenuExecutor, s ssh.Session, terminal *term.Terminal, userManager *user.UserMgr, currentUser *user.User, nodeNumber int, sessionStartTime time.Time, args string, outputMode ansi.OutputMode, termWidth int, termHeight int) (*user.User, string, error) {
	log.Printf("DEBUG: Node %d: Running SEARCH_FILES", nodeNumber)

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

	// Search all files
	results := e.FileMgr.SearchFiles(query)

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
		desc := r.description
		if len(desc) > 40 {
			desc = desc[:40]
		}

		line := fmt.Sprintf("\r\n|09%-8s |15%-12s |07%5dk |14%s", r.areaTag, r.filename, r.size/1024, desc)
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
		fmt.Sprintf(e.LoadedStrings.SearchResultsHeader, fmt.Sprintf("%d file(s) found", len(filtered))),
	)), outputMode)

	return currentUser, "", nil
}
