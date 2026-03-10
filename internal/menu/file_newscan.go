package menu

import (
	"fmt"
	"io"
	"log"
	"strings"
	"time"

	"github.com/gliderlabs/ssh"
	"golang.org/x/term"

	"github.com/stlalpha/vision3/internal/ansi"
	"github.com/stlalpha/vision3/internal/file"
	"github.com/stlalpha/vision3/internal/terminalio"
	"github.com/stlalpha/vision3/internal/user"
)

// runFileNewscan scans file areas for files uploaded since the user's last login.
func runFileNewscan(e *MenuExecutor, s ssh.Session, terminal *term.Terminal,
	userManager *user.UserMgr, currentUser *user.User, nodeNumber int,
	sessionStartTime time.Time, args string, outputMode ansi.OutputMode,
	termWidth int, termHeight int) (*user.User, string, error) {

	if currentUser == nil {
		return currentUser, "", nil
	}

	log.Printf("INFO: Node %d: FILE_NEWSCAN for user %s (last login: %s, args: %q)",
		nodeNumber, currentUser.Handle, currentUser.LastLogin.Format(time.RFC3339), args)

	since := currentUser.LastLogin

	// Determine which areas to scan
	var areas []file.FileArea
	if strings.EqualFold(args, "CURRENT") {
		area, ok := e.FileMgr.GetAreaByID(currentUser.CurrentFileAreaID)
		if ok {
			areas = []file.FileArea{*area}
		}
	} else {
		// Scan all accessible areas
		for _, area := range e.FileMgr.ListAreas() {
			if checkACS(area.ACSList, currentUser, s, terminal, sessionStartTime) {
				areas = append(areas, area)
			}
		}
	}

	// Display header
	terminalio.WriteProcessedBytes(terminal,
		ansi.ReplacePipeCodes([]byte(e.LoadedStrings.FileNewscanHeader)), outputMode)

	totalNew := 0
	lineCount := 0
	pausePrompt := e.LoadedStrings.PauseString
	// Reserve lines for header/footer; pause every (termHeight - 2) lines
	pageLines := termHeight - 2
	if pageLines < 5 {
		pageLines = 5
	}

	for _, area := range areas {
		newFiles := e.FileMgr.GetFilesNewerThan(area.ID, since)
		if len(newFiles) == 0 {
			continue
		}

		// Area header
		areaHdr := fmt.Sprintf(e.LoadedStrings.FileNewscanAreaHdr, area.Name, len(newFiles))
		terminalio.WriteProcessedBytes(terminal,
			ansi.ReplacePipeCodes([]byte(areaHdr)), outputMode)
		lineCount += 2 // area header typically takes ~2 lines

		for _, f := range newFiles {
			desc := f.Description
			maxDesc := termWidth - 40
			if maxDesc < 10 {
				maxDesc = 10
			}
			if len(desc) > maxDesc {
				desc = desc[:maxDesc-3] + "..."
			}

			sizeKB := (f.Size + 1023) / 1024
			dateFmt := f.UploadedAt.Format("01/02/06")
			line := fmt.Sprintf("  %-12s %6dK  %s  %s\r\n",
				f.Filename, sizeKB, dateFmt, desc)

			terminalio.WriteProcessedBytes(terminal,
				ansi.ReplacePipeCodes([]byte(line)), outputMode)
			lineCount++
			totalNew++

			if lineCount >= pageLines {
				lineCount = 0
				err := writeCenteredPausePrompt(s, terminal, pausePrompt, outputMode, termWidth, termHeight)
				if err != nil {
					if err == io.EOF {
						return currentUser, "", nil
					}
					return currentUser, "", nil
				}
			}
		}
	}

	// Summary
	if totalNew == 0 {
		terminalio.WriteProcessedBytes(terminal,
			ansi.ReplacePipeCodes([]byte(e.LoadedStrings.FileNewscanNoNew)), outputMode)
	} else {
		msg := fmt.Sprintf(e.LoadedStrings.FileNewscanComplete, totalNew)
		terminalio.WriteProcessedBytes(terminal,
			ansi.ReplacePipeCodes([]byte(msg)), outputMode)
	}

	_ = writeCenteredPausePrompt(s, terminal, pausePrompt, outputMode, termWidth, termHeight)

	return currentUser, "", nil
}
