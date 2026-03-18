package menu

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gliderlabs/ssh"
	term "golang.org/x/term"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/registry"
)

func runV3NetRegistry(e *MenuExecutor, s ssh.Session, terminal *term.Terminal,
	_ *user.UserMgr, currentUser *user.User, _ int, _ time.Time, _ string,
	outputMode ansi.OutputMode, termWidth int, termHeight int,
) (*user.User, string, error) {
	if currentUser == nil {
		return nil, "", nil
	}

	var buf bytes.Buffer
	buf.Write([]byte(ansi.ClearScreen()))

	header := "|12V3Net Network Registry|07\r\n" +
		"|08" + strings.Repeat("─", 78) + "|07\r\n"
	buf.Write(ansi.ReplacePipeCodes([]byte(header)))

	if e.V3NetStatus == nil {
		buf.Write(ansi.ReplacePipeCodes([]byte(
			"|08V3Net is |01disabled|08 in server configuration.|07\r\n")))
	} else {
		regURL := e.V3NetStatus.RegistryURL()

		buf.Write(ansi.ReplacePipeCodes([]byte(
			fmt.Sprintf("|08  Source: |07%s|07\r\n\r\n", regURL))))

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		networks, err := registry.Fetch(ctx, regURL)
		if err != nil {
			buf.Write(ansi.ReplacePipeCodes([]byte(
				fmt.Sprintf("|04  Error fetching registry: %s|07\r\n", err))))
		} else if len(networks) == 0 {
			buf.Write(ansi.ReplacePipeCodes([]byte(
				"|08  No networks listed in registry.|07\r\n")))
		} else {
			// Column headers.
			buf.Write(ansi.ReplacePipeCodes([]byte(
				fmt.Sprintf("|03  %-20s %-30s %s|07\r\n", "Network", "Description", "Hub URL"))))
			buf.Write(ansi.ReplacePipeCodes([]byte(
				"|08  " + strings.Repeat("─", 74) + "|07\r\n")))

			// Build set of subscribed networks for marking.
			subscribed := make(map[string]bool)
			for _, n := range e.V3NetStatus.LeafNetworks() {
				subscribed[n] = true
			}

			for _, net := range networks {
				marker := "  "
				if subscribed[net.Name] {
					marker = "|10* "
				}
				name := truncateStr(net.Name, 18)
				desc := truncateStr(net.Description, 28)
				hub := truncateStr(net.HubURL, 24)
				line := fmt.Sprintf("%s|11%-18s |07%-28s |08%s|07\r\n",
					marker, name, desc, hub)
				buf.Write(ansi.ReplacePipeCodes([]byte(line)))
			}

			buf.Write(ansi.ReplacePipeCodes([]byte(
				fmt.Sprintf("\r\n|08  %d network(s) available. |10*|08 = subscribed|07\r\n",
					len(networks)))))
		}
	}

	buf.WriteString("\r\n")
	buf.Write(ansi.ReplacePipeCodes([]byte(
		"|08" + strings.Repeat("─", 78) + "|07\r\n")))

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
