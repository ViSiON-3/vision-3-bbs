package menu

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/gliderlabs/ssh"
	"golang.org/x/term"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// Shared plumbing for the two V3Net management screens: the area-manager
// access request queue and the coordinator's proposal queue. Both are plain
// numbered lists with a one-line command prompt, in the style of the BBS
// list and rumor screens, so they work on any terminal the menu supports.

const v3netManageTimeout = 15 * time.Second

// v3netRule draws the separator used under screen headers.
func v3netRule(width int) string {
	if width < 2 {
		width = 2
	}
	return "|08" + strings.Repeat("─", width-1) + "|07\r\n"
}

// v3netAgo renders a hub timestamp as a short relative age. The hub stores
// proposed_at and requested_at with SQLite's datetime('now'), which comes
// back as "YYYY-MM-DD HH:MM:SS" in UTC; RFC 3339 is accepted too. Anything
// else is shown as-is, truncated, so a hub bug never hides a row.
func v3netAgo(stamp string, now time.Time) string {
	t, err := time.Parse(time.RFC3339, stamp)
	if err != nil {
		t, err = time.ParseInLocation("2006-01-02 15:04:05", stamp, time.UTC)
	}
	if err != nil {
		return truncateStr(stamp, 10)
	}
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

// parseListCommand splits an entry like "A 3", "a3" or "q" into an action
// letter and a 1-based row number. ok is false when the line is not a letter
// followed by a number.
func parseListCommand(line string) (action byte, row int, ok bool) {
	line = strings.TrimSpace(strings.ToUpper(line))
	if line == "" {
		return 0, 0, false
	}
	action = line[0]
	n, err := strconv.Atoi(strings.TrimSpace(line[1:]))
	if err != nil || n < 1 {
		return action, 0, false
	}
	return action, n, true
}

// v3netPromptLine writes a prompt and reads one line, mapping a disconnect
// to the LOGOFF action the menu executor expects.
func v3netPromptLine(s ssh.Session, terminal *term.Terminal, outputMode ansi.OutputMode, prompt string) (string, string, error) {
	terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(prompt)), outputMode)
	line, err := readLineFromSessionIH(s, terminal)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return "", "LOGOFF", io.EOF
		}
		return "", "", err
	}
	return strings.TrimSpace(line), "", nil
}

// v3netPause shows the standard pause prompt under a finished screen.
func v3netPause(e *MenuExecutor, s ssh.Session, terminal *term.Terminal, outputMode ansi.OutputMode, termWidth, termHeight int) (*user.User, string, error) {
	pausePrompt := e.Strings().PauseString
	if pausePrompt == "" {
		pausePrompt = "\r\n|07Press |15[ENTER]|07 to continue... "
	}
	if err := writeCenteredPausePrompt(s, terminal, pausePrompt, outputMode, termWidth, termHeight); err != nil {
		return nil, "", err
	}
	return nil, "", nil
}

// v3netRoles fetches each subscribed network's NAL once and reports which
// networks this node coordinates and which areas it manages. Fetch failures
// are returned as display strings rather than aborting the screen, since one
// unreachable hub should not hide the others.
type v3netManagedArea struct {
	network string
	area    protocol.Area
}

func v3netRoles(ctx context.Context, svc V3NetStatusProvider) (coordinated []string, managed []v3netManagedArea, errs []string) {
	me := svc.NodeID()
	for _, net := range svc.LeafNetworks() {
		n, err := svc.FetchNALForNetwork(ctx, net)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %s", net, err))
			continue
		}
		if n == nil {
			errs = append(errs, fmt.Sprintf("%s: no NAL returned", net))
			continue
		}
		if n.CoordNodeID != "" && n.CoordNodeID == me {
			coordinated = append(coordinated, net)
		}
		for _, a := range n.Areas {
			if a.ManagerNodeID != "" && a.ManagerNodeID == me {
				managed = append(managed, v3netManagedArea{network: net, area: a})
			}
		}
	}
	return coordinated, managed, errs
}
