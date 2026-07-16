package menu

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// runShowFileInfo prompts for a filename, looks it up in the current file area,
// and displays full metadata (filename, size, date, uploader, download count,
// description, area name).
func runShowFileInfo(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	s := c.s
	terminal := c.terminal
	currentUser := c.currentUser
	nodeNumber := c.nodeNumber
	outputMode := c.outputMode
	termWidth := c.termWidth
	termHeight := c.termHeight

	slog.Debug("running SHOWFILEINFO", "node", nodeNumber)

	if currentUser == nil {
		return nil, "", nil
	}

	currentAreaID := currentUser.CurrentFileAreaID
	if currentAreaID <= 0 {
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.LoadedStrings.FileNoAreaSelected)), outputMode)
		time.Sleep(1 * time.Second)
		return currentUser, "", nil
	}

	// Prompt for filename.
	terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.LoadedStrings.FileInfoPrompt)), outputMode)

	input, err := readLineFromSessionIH(s, terminal)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, "LOGOFF", io.EOF
		}
		return currentUser, "", fmt.Errorf("failed reading input: %w", err)
	}

	filename := strings.TrimSpace(input)
	if filename == "" {
		return currentUser, "", nil
	}

	// Look up the file in the current area.
	rec, err := findFileInArea(e.FileMgr, currentAreaID, filename)
	if err != nil {
		msg := fmt.Sprintf(e.LoadedStrings.FileNotFoundFormat, filename)
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		time.Sleep(1 * time.Second)
		return currentUser, "", nil
	}

	// Resolve the area name.
	areaName := ""
	if area, ok := e.FileMgr.GetAreaByID(rec.AreaID); ok {
		areaName = area.Name
	}

	// Display header and file metadata.
	terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.LoadedStrings.FileInfoHeader)), outputMode)

	sizeStr := ""
	if rec.Size < 1024 {
		sizeStr = fmt.Sprintf("%d bytes", rec.Size)
	} else {
		sizeStr = fmt.Sprintf("%d KB", (rec.Size+1023)/1024)
	}
	info := fmt.Sprintf(
		"\r\n|15Filename    : |07%s\r\n"+
			"|15Size        : |07%s\r\n"+
			"|15Date        : |07%s\r\n"+
			"|15Uploaded By : |07%s\r\n"+
			"|15Downloads   : |07%d\r\n"+
			"|15Area        : |07%s\r\n"+
			"|15Description : |07%s\r\n",
		rec.Filename,
		sizeStr,
		rec.UploadedAt.Format("01/02/2006"),
		rec.UploadedBy,
		rec.DownloadCount,
		areaName,
		rec.Description,
	)
	terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(info)), outputMode)

	// Pause before returning.
	_ = writeCenteredPausePrompt(s, terminal, e.LoadedStrings.PauseString, outputMode, termWidth, termHeight) // best-effort pause prompt

	return currentUser, "", nil
}
