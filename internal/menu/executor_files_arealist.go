package menu

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/file"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"github.com/gliderlabs/ssh"
	"golang.org/x/term"
)

// displayFileAreaList is an internal helper to display the list of accessible file areas.
// It does not include a pause prompt.
func displayFileAreaList(e *MenuExecutor, s ssh.Session, terminal *term.Terminal, currentUser *user.User, outputMode ansi.OutputMode, nodeNumber int, sessionStartTime time.Time) error {
	slog.Debug("displaying file area list", "node", nodeNumber)

	// 1. Define Template filenames and paths
	topTemplateFilename := "FILEAREA.TOP"
	midTemplateFilename := "FILEAREA.MID"
	botTemplateFilename := "FILEAREA.BOT"
	templateDir := filepath.Join(e.MenuSetPath, "templates")
	topTemplatePath := filepath.Join(templateDir, topTemplateFilename)
	midTemplatePath := filepath.Join(templateDir, midTemplateFilename)
	botTemplatePath := filepath.Join(templateDir, botTemplateFilename)

	// 2. Load Template Files
	topTemplateBytes, errTop := readTemplateFile(topTemplatePath)
	midTemplateBytes, errMid := readTemplateFile(midTemplatePath)
	botTemplateBytes, errBot := readTemplateFile(botTemplatePath)

	if errTop != nil || errMid != nil || errBot != nil {
		slog.Error("failed to load FILEAREA template files", "node", nodeNumber, "top_error", errTop, "mid_error", errMid, "bot_error", errBot)
		// Display error message to terminal
		msg := "\r\n|01Error loading File Area screen templates.|07\r\n"
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		time.Sleep(1 * time.Second)
		return fmt.Errorf("failed loading FILEAREA templates")
	}

	// 3. Process Pipe Codes in Templates FIRST
	processedTopTemplate := ansi.ReplacePipeCodes(topTemplateBytes)
	processedMidTemplate := string(ansi.ReplacePipeCodes(midTemplateBytes))
	processedBotTemplate := ansi.ReplacePipeCodes(botTemplateBytes)

	// Conference header template (optional)
	confHdrBytes, errConf := readTemplateFile(filepath.Join(templateDir, "FILECONF.HDR"))
	confHdrTemplate := ""
	if errConf == nil {
		confHdrTemplate = string(ansi.ReplacePipeCodes(confHdrBytes))
	}

	// 4. Get file area list data and group by conference
	areas := e.FileMgr.ListAreas()

	// Build conference groups: conferenceID -> []file.FileArea (ACS-filtered)
	groups := make(map[int][]file.FileArea)
	confIDs := make(map[int]bool)
	for _, area := range areas {
		if !checkACS(area.ACSList, currentUser, s, terminal, sessionStartTime) {
			slog.Debug("user denied list access to file area due to ACS", "node", nodeNumber, "handle", currentUser.Handle, "area", area.Tag, "acs", area.ACSList)
			continue
		}
		groups[area.ConferenceID] = append(groups[area.ConferenceID], area)
		confIDs[area.ConferenceID] = true
	}

	// Sort conference IDs (0/ungrouped first)
	var sortedConfIDs []int
	if e.ConferenceMgr != nil {
		sortedConfIDs = e.ConferenceMgr.GetSortedConferenceIDs(confIDs)
	} else {
		for cid := range confIDs {
			sortedConfIDs = append(sortedConfIDs, cid)
		}
		sort.Ints(sortedConfIDs)
	}

	// 5. Build the output string using processed templates and data
	var outputBuffer bytes.Buffer
	outputBuffer.Write(processedTopTemplate)

	areasDisplayed := 0
	for _, cid := range sortedConfIDs {
		areasInConf := groups[cid]
		if len(areasInConf) == 0 {
			continue
		}

		// Check conference ACS and write header
		if cid != 0 && e.ConferenceMgr != nil {
			conf, found := e.ConferenceMgr.GetByID(cid)
			if found && !checkACS(conf.ACS, currentUser, s, terminal, sessionStartTime) {
				continue
			}
			if found && confHdrTemplate != "" {
				hdr := confHdrTemplate
				hdr = strings.ReplaceAll(hdr, "^CN", conf.Name)
				hdr = strings.ReplaceAll(hdr, "^CT", conf.Tag)
				hdr = strings.ReplaceAll(hdr, "^CD", conf.Description)
				hdr = strings.ReplaceAll(hdr, "^CI", strconv.Itoa(conf.ID))
				outputBuffer.WriteString(hdr)
			}
		}

		for _, area := range areasInConf {
			line := processedMidTemplate
			name := string(ansi.ReplacePipeCodes([]byte(area.Name)))
			desc := string(ansi.ReplacePipeCodes([]byte(area.Description)))
			idStr := strconv.Itoa(area.ID)
			tag := string(ansi.ReplacePipeCodes([]byte(area.Tag)))
			fileCount, countErr := e.FileMgr.GetFileCountForArea(area.ID)
			if countErr != nil {
				slog.Warn("failed getting file count for area", "node", nodeNumber, "area", area.Tag, "id", area.ID, "error", countErr)
				fileCount = 0
			}
			fileCountStr := strconv.Itoa(fileCount)

			line = strings.ReplaceAll(line, "^ID", idStr)
			line = strings.ReplaceAll(line, "^TAG", tag)
			line = strings.ReplaceAll(line, "^NA", name)
			line = strings.ReplaceAll(line, "^DS", desc)
			line = strings.ReplaceAll(line, "^NF", fileCountStr)

			outputBuffer.WriteString(line)
			areasDisplayed++
		}
	}

	if areasDisplayed == 0 {
		slog.Debug("no accessible file areas to display", "node", nodeNumber, "handle", currentUser.Handle)
		outputBuffer.WriteString("\r\n|07   No accessible file areas found.   \r\n")
	}

	outputBuffer.Write(processedBotTemplate)

	// 6. Clear screen and display the assembled content
	writeErr := terminalio.WriteProcessedBytes(terminal, []byte(ansi.ClearScreen()), outputMode)
	if writeErr != nil {
		slog.Error("failed clearing screen for file area list", "node", nodeNumber, "error", writeErr)
		// Try to continue anyway?
	}

	processedContent := outputBuffer.Bytes()
	// For CP437 mode, write raw bytes directly to avoid UTF-8 false positives
	var wErr error
	if outputMode == ansi.OutputModeCP437 {
		_, wErr = terminal.Write(processedContent)
	} else {
		wErr = terminalio.WriteProcessedBytes(terminal, processedContent, outputMode)
	}
	if wErr != nil {
		slog.Error("failed writing file area list output", "node", nodeNumber, "error", wErr)
		return wErr // Return the error from writing
	}

	return nil // Success
}

// runListFileAreas displays a list of file areas using templates.
func runListFileAreas(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	s := c.s
	terminal := c.terminal
	currentUser := c.currentUser
	nodeNumber := c.nodeNumber
	sessionStartTime := c.sessionStartTime
	outputMode := c.outputMode
	termWidth := c.termWidth
	termHeight := c.termHeight

	slog.Debug("running LISTFILEAR", "node", nodeNumber)

	if currentUser == nil {
		slog.Warn("LISTFILEAR called without logged in user", "node", nodeNumber)
		msg := "\r\n|01Error: You must be logged in to list file areas.|07\r\n"
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		time.Sleep(1 * time.Second)
		return nil, "", nil
	}

	// Call the helper to display the list
	if err := displayFileAreaList(e, s, terminal, currentUser, outputMode, nodeNumber, sessionStartTime); err != nil {
		// Error already logged by helper, maybe add context?
		slog.Error("error occurred during displayFileAreaList", "node", nodeNumber, "error", err)
		// Need to decide if we still pause or just return.
		// For now, return the error to prevent pause on failed display.
		return nil, "", err
	}

	// Wait for Enter using configured PauseString (centered)
	pausePrompt := e.LoadedStrings.PauseString
	if pausePrompt == "" {
		pausePrompt = "\r\n|07Press |15[ENTER]|07 to continue... " // Fallback
	}

	slog.Debug("displaying LISTFILEAR pause prompt", "node", nodeNumber)
	err := writeCenteredPausePrompt(s, terminal, pausePrompt, outputMode, termWidth, termHeight)
	if err != nil {
		if errors.Is(err, io.EOF) {
			slog.Info("user disconnected during LISTFILEAR pause", "node", nodeNumber)
			return nil, "LOGOFF", io.EOF
		}
		slog.Error("failed during LISTFILEAR pause", "node", nodeNumber, "error", err)
		return nil, "", err
	}

	return nil, "", nil // Success, return to current menu (FILEM)
}
