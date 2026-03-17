package menu

import (
	"bytes"
	"time"

	"github.com/gliderlabs/ssh"
	term "golang.org/x/term"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

func runV3NetPropose(e *MenuExecutor, s ssh.Session, terminal *term.Terminal, userManager *user.UserMgr, currentUser *user.User, nodeNumber int, sessionStartTime time.Time, args string, outputMode ansi.OutputMode, termWidth int, termHeight int) (*user.User, string, error) {
	if currentUser == nil || e.V3NetStatus == nil {
		return nil, "", nil
	}

	var buf bytes.Buffer
	buf.Write([]byte(ansi.ClearScreen()))

	header := "|12V3Net: Propose New Area|07\r\n|08────────────────────────────────────────────────────────────────────────────────|07\r\n"
	buf.Write(ansi.ReplacePipeCodes([]byte(header)))

	lines := []string{
		"|03  Area Tag     |08: |15fel.________________|07",
		"|03  Display Name |08: |15________________________________|07",
		"|03  Description  |08: |15________________________________________________|07",
		"|03  Language     |08: |15en|07",
		"|03  Access Mode  |08: |15[Open]|08 / Approval / Closed|07",
		"|03  Allow ANSI   |08: |15[Y]|07",
		"",
		"|08  [|15S|08]ubmit  [|15Q|08]uit|07",
	}

	for _, line := range lines {
		buf.Write(ansi.ReplacePipeCodes([]byte(line)))
		buf.WriteString("\r\n")
	}

	buf.WriteString("\r\n")
	footer := "|08────────────────────────────────────────────────────────────────────────────────|07\r\n"
	buf.Write(ansi.ReplacePipeCodes([]byte(footer)))

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
