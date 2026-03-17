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

func runV3NetCoordinator(e *MenuExecutor, s ssh.Session, terminal *term.Terminal, userManager *user.UserMgr, currentUser *user.User, nodeNumber int, sessionStartTime time.Time, args string, outputMode ansi.OutputMode, termWidth int, termHeight int) (*user.User, string, error) {
	if currentUser == nil || e.V3NetStatus == nil {
		return nil, "", nil
	}

	var buf bytes.Buffer
	buf.Write([]byte(ansi.ClearScreen()))

	header := "|12V3Net: Coordinator Panel|07\r\n|08────────────────────────────────────────────────────────────────────────────────|07\r\n"
	buf.Write(ansi.ReplacePipeCodes([]byte(header)))

	lines := []string{
		"",
		"|03  [|15P|03]ending area proposals|07",
		"|03  [|15M|03]anage area managers|07",
		"|03  [|15T|03]ransfer coordinator role|07",
		"|03  [|15Q|03]uit|07",
		"",
	}

	for _, line := range lines {
		buf.Write(ansi.ReplacePipeCodes([]byte(line)))
		buf.WriteString("\r\n")
	}

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
