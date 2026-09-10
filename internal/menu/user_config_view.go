package menu

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

func runCfgViewConfig(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	s := c.s
	terminal := c.terminal
	currentUser := c.currentUser
	nodeNumber := c.nodeNumber
	outputMode := c.outputMode
	termWidth := c.termWidth
	termHeight := c.termHeight

	if currentUser == nil {
		return nil, "", nil
	}

	topPath := filepath.Join(e.MenuSetPath, "templates", "USRCFGV.TOP")
	botPath := filepath.Join(e.MenuSetPath, "templates", "USRCFGV.BOT")

	topBytes, err := os.ReadFile(topPath)
	if err != nil && !os.IsNotExist(err) {
		slog.Warn("failed to read template", "node", nodeNumber, "path", topPath, "error", err)
	}
	botBytes, err := os.ReadFile(botPath)
	if err != nil && !os.IsNotExist(err) {
		slog.Warn("failed to read template", "node", nodeNumber, "path", botPath, "error", err)
	}

	topBytes = stripSauceMetadata(topBytes)
	botBytes = stripSauceMetadata(botBytes)
	topBytes = normalizePipeCodeDelimiters(topBytes)
	botBytes = normalizePipeCodeDelimiters(botBytes)

	var buf bytes.Buffer

	if len(topBytes) > 0 {
		buf.Write(ansi.ReplacePipeCodes(topBytes))
		buf.WriteString("\r\n")
	}

	boolStr := func(v bool) string {
		if v {
			return e.Strings().CfgToggleOn
		}
		return e.Strings().CfgToggleOff
	}

	width := currentUser.ScreenWidth
	if width == 0 {
		width = 80
	}
	height := currentUser.ScreenHeight
	if height == 0 {
		height = 25
	}
	outMode := currentUser.OutputMode
	if outMode == "" {
		outMode = "cp437"
	}

	lines := []string{
		fmt.Sprintf(e.Strings().CfgViewScreenWidth, width),
		fmt.Sprintf(e.Strings().CfgViewScreenHeight, height),
		fmt.Sprintf(e.Strings().CfgViewTermType, strings.ToUpper(outMode)),
		fmt.Sprintf(e.Strings().CfgViewHotKeys, boolStr(currentUser.HotKeys)),
		fmt.Sprintf(e.Strings().CfgViewMorePrompts, boolStr(currentUser.MorePrompts)),
		fmt.Sprintf(e.Strings().CfgViewFileListMode, fileListModeDisplay(currentUser.FileListingMode)),
		fmt.Sprintf(e.Strings().CfgViewMsgHeader, currentUser.MsgHdr),
		fmt.Sprintf(e.Strings().CfgViewCustomPrompt, currentUser.CustomPrompt),
		"",
		fmt.Sprintf(e.Strings().CfgViewPromptColor, currentUser.Colors[0], currentUser.Colors[0], currentUser.Colors[1], currentUser.Colors[1]),
		fmt.Sprintf(e.Strings().CfgViewTextColor, currentUser.Colors[2], currentUser.Colors[2], currentUser.Colors[3], currentUser.Colors[3]),
		fmt.Sprintf(e.Strings().CfgViewText2Color, currentUser.Colors[4], currentUser.Colors[4], currentUser.Colors[5], currentUser.Colors[5]),
		fmt.Sprintf(e.Strings().CfgViewBarColor, currentUser.Colors[6], currentUser.Colors[6]),
		"",
		fmt.Sprintf(e.Strings().CfgViewRealName, currentUser.RealName),
		fmt.Sprintf(e.Strings().CfgViewNote, currentUser.PrivateNote),
	}

	// Append auto-signature info
	if currentUser.AutoSignature != "" {
		lines = append(lines, "", "|03  Auto-Signature:|07")
		sigLines := strings.Split(currentUser.AutoSignature, "\n")
		for _, sl := range sigLines {
			lines = append(lines, "    "+sl)
		}
	} else {
		lines = append(lines, "", "|08  Auto-Signature: (none)|07")
	}

	for _, line := range lines {
		buf.Write(ansi.ReplacePipeCodes([]byte(line)))
		buf.WriteString("\r\n")
	}

	if len(botBytes) > 0 {
		buf.Write(ansi.ReplacePipeCodes(botBytes))
		buf.WriteString("\r\n")
	}

	terminalio.WriteProcessedBytes(terminal, buf.Bytes(), outputMode)

	// Pause
	pausePrompt := e.Strings().PauseString
	if pausePrompt == "" {
		pausePrompt = "\r\n|07Press |15[ENTER]|07 to continue... "
	}
	if err := writeCenteredPausePrompt(s, terminal, pausePrompt, outputMode, termWidth, termHeight); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, "LOGOFF", io.EOF
		}
	}

	return currentUser, "", nil
}
