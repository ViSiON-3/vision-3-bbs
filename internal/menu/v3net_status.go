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

// V3NetStatusProvider is an interface for querying V3Net service status.
// The concrete implementation is *v3net.Service, injected from cmd/vision3.
type V3NetStatusProvider interface {
	NodeID() string
	HubActive() bool
	LeafCount() int
	LeafNetworks() []string
}

func runV3NetStatus(e *MenuExecutor, s ssh.Session, terminal *term.Terminal, userManager *user.UserMgr, currentUser *user.User, nodeNumber int, sessionStartTime time.Time, args string, outputMode ansi.OutputMode, termWidth int, termHeight int) (*user.User, string, error) {
	if currentUser == nil {
		return nil, "", nil
	}

	var buf bytes.Buffer
	buf.Write([]byte(ansi.ClearScreen()))

	header := "|12V3Net Status|07\r\n|08────────────────────────────────────────────────────────────────────────────────|07\r\n"
	buf.Write(ansi.ReplacePipeCodes([]byte(header)))

	if e.V3NetStatus == nil {
		line := "|08V3Net is |01disabled|08 in server configuration.|07\r\n"
		buf.Write(ansi.ReplacePipeCodes([]byte(line)))
	} else {
		svc := e.V3NetStatus

		lines := []string{
			fmt.Sprintf("|03  Node ID        |08: |15%s|07", svc.NodeID()),
			"",
		}

		if svc.HubActive() {
			lines = append(lines, "|03  Hub Mode       |08: |10Active|07")
		} else {
			lines = append(lines, "|03  Hub Mode       |08: |08Inactive|07")
		}

		leafCount := svc.LeafCount()
		lines = append(lines, fmt.Sprintf("|03  Subscriptions  |08: |15%d|07", leafCount))

		networks := svc.LeafNetworks()
		if len(networks) > 0 {
			lines = append(lines, "")
			lines = append(lines, "|03  Networks:|07")
			for _, net := range networks {
				lines = append(lines, fmt.Sprintf("|08    · |11%s|07", net))
			}
		}

		for _, line := range lines {
			buf.Write(ansi.ReplacePipeCodes([]byte(line)))
			buf.WriteString("\r\n")
		}
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
