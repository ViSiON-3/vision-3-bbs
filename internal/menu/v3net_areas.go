package menu

import (
	"bytes"
	"fmt"
	"time"

	"github.com/gliderlabs/ssh"
	term "golang.org/x/term"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

func runV3NetAreas(e *MenuExecutor, s ssh.Session, terminal *term.Terminal, userManager *user.UserMgr, currentUser *user.User, nodeNumber int, sessionStartTime time.Time, args string, outputMode ansi.OutputMode, termWidth int, termHeight int) (*user.User, string, error) {
	if currentUser == nil || e.V3NetStatus == nil {
		return nil, "", nil
	}

	var buf bytes.Buffer
	buf.Write([]byte(ansi.ClearScreen()))

	header := fmt.Sprintf("|12V3Net: %s — Area Subscriptions|07\r\n|08────────────────────────────────────────────────────────────────────────────────|07\r\n", args)
	buf.Write(ansi.ReplacePipeCodes([]byte(header)))

	colHeader := "|03  TAG                 NAME              STATUS     LOCAL BOARD|07\r\n"
	buf.Write(ansi.ReplacePipeCodes([]byte(colHeader)))

	// Display placeholder — area data comes from the NAL via the V3Net service.
	// In a full implementation, this would iterate over cached NAL areas and
	// display subscription status from the hub.
	noAreas := "|08  No areas available. NAL not yet fetched.|07\r\n"
	buf.Write(ansi.ReplacePipeCodes([]byte(noAreas)))

	buf.WriteString("\r\n")
	footer := "|08────────────────────────────────────────────────────────────────────────────────|07\r\n"
	buf.Write(ansi.ReplacePipeCodes([]byte(footer)))

	helpLine := "|08  [|15Space|08] subscribe/unsubscribe  [|15E|08]dit local board name  [|15P|08]ropose new area  [|15Q|08]uit|07\r\n"
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
