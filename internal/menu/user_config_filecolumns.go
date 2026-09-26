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

// runCfgFileColumns is CFG_FILECOLUMNS, the standalone column switcher the
// stock file menu binds. USERCONFIG offers the same switches in a box.
func runCfgFileColumns(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	s := c.s
	terminal := c.terminal
	userManager := c.userManager
	currentUser := c.currentUser
	nodeNumber := c.nodeNumber
	outputMode := c.outputMode

	if currentUser == nil {
		return nil, "", nil
	}

	originalColumns := currentUser.FileListColumns

	boolStr := func(v bool) string {
		if v {
			return e.Strings().CfgToggleOn
		}
		return e.Strings().CfgToggleOff
	}

	for {
		c := currentUser.FileListColumns
		allDefault := !c.Name && !c.Size && !c.Date && !c.Downloads && !c.Uploader && !c.Description

		displayState := func(val bool) string {
			if allDefault {
				return boolStr(true)
			}
			return boolStr(val)
		}

		var buf strings.Builder
		buf.WriteString(e.Strings().CfgFileColumnsHeader)
		fmt.Fprintf(&buf, e.Strings().CfgFileColumnsToggle, "N", "Name", displayState(c.Name))
		fmt.Fprintf(&buf, e.Strings().CfgFileColumnsToggle, "S", "Size", displayState(c.Size))
		fmt.Fprintf(&buf, e.Strings().CfgFileColumnsToggle, "D", "Date", displayState(c.Date))
		fmt.Fprintf(&buf, e.Strings().CfgFileColumnsToggle, "L", "Downloads", displayState(c.Downloads))
		fmt.Fprintf(&buf, e.Strings().CfgFileColumnsToggle, "U", "Uploader", displayState(c.Uploader))
		fmt.Fprintf(&buf, e.Strings().CfgFileColumnsToggle, "E", "Description", displayState(c.Description))
		buf.WriteString(e.Strings().CfgFileColumnsHeader) // reuse as prompt separator
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(buf.String())), outputMode)

		input, err := readLineFromSessionIH(s, terminal)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil, "LOGOFF", io.EOF
			}
			return currentUser, "", nil
		}

		input = strings.TrimSpace(strings.ToUpper(input))
		if input == "" || input == "Q" {
			if err := userManager.UpdateUser(currentUser); err != nil {
				currentUser.FileListColumns = originalColumns
				slog.Error("failed to save file column preferences", "node", nodeNumber, "error", err)
			}
			terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.Strings().CfgFileColumnsSaved)), outputMode)
			time.Sleep(500 * time.Millisecond)
			return currentUser, "", nil
		}

		if allDefault {
			currentUser.FileListColumns.Name = true
			currentUser.FileListColumns.Size = true
			currentUser.FileListColumns.Date = true
			currentUser.FileListColumns.Downloads = true
			currentUser.FileListColumns.Uploader = true
			currentUser.FileListColumns.Description = true
		}

		switch input {
		case "N":
			currentUser.FileListColumns.Name = !currentUser.FileListColumns.Name
		case "S":
			currentUser.FileListColumns.Size = !currentUser.FileListColumns.Size
		case "D":
			currentUser.FileListColumns.Date = !currentUser.FileListColumns.Date
		case "L":
			currentUser.FileListColumns.Downloads = !currentUser.FileListColumns.Downloads
		case "U":
			currentUser.FileListColumns.Uploader = !currentUser.FileListColumns.Uploader
		case "E":
			currentUser.FileListColumns.Description = !currentUser.FileListColumns.Description
		}
	}
}
