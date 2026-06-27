package menu

import (
	"bytes"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

func runV3NetAccessRequests(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	s := c.s
	terminal := c.terminal
	currentUser := c.currentUser
	outputMode := c.outputMode
	termWidth := c.termWidth
	termHeight := c.termHeight

	if currentUser == nil || e.V3NetStatus == nil {
		return nil, "", nil
	}

	var buf bytes.Buffer
	buf.Write([]byte(ansi.ClearScreen()))

	header := "|12V3Net: Area Access Requests|07\r\n|08────────────────────────────────────────────────────────────────────────────────|07\r\n"
	buf.Write(ansi.ReplacePipeCodes([]byte(header)))

	colHeader := "|03  NETWORK     AREA TAG          BBS NAME                  REQUESTED|07\r\n"
	buf.Write(ansi.ReplacePipeCodes([]byte(colHeader)))

	// Placeholder — in full implementation, this queries the hub for pending
	// access requests for areas where this node is the manager.
	noReqs := "|08  No pending access requests.|07\r\n"
	buf.Write(ansi.ReplacePipeCodes([]byte(noReqs)))

	buf.WriteString("\r\n")
	footer := "|08────────────────────────────────────────────────────────────────────────────────|07\r\n"
	buf.Write(ansi.ReplacePipeCodes([]byte(footer)))

	helpLine := "|08  [|15A|08]pprove  [|15D|08]eny  [|15B|08]lacklist  [|15Q|08]uit|07\r\n"
	buf.Write(ansi.ReplacePipeCodes([]byte(helpLine)))

	terminalio.WriteProcessedBytes(terminal, buf.Bytes(), outputMode)

	pausePrompt := e.LoadedStrings.PauseString
	if pausePrompt == "" {
		pausePrompt = "\r\n|07Press |15[ENTER]|07 to continue... "
	}
	if err := writeCenteredPausePrompt(s, terminal, pausePrompt, outputMode, termWidth, termHeight); err != nil {
		return nil, "", err
	}

	return nil, "", nil
}
