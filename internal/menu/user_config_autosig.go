package menu

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// maxAutoSigLines is the maximum number of lines allowed in an auto-signature.
const maxAutoSigLines = 5

func runCfgAutoSig(c *cmdCtx, args string) (*user.User, string, error) {
	s := c.s
	terminal := c.terminal
	userManager := c.userManager
	currentUser := c.currentUser
	nodeNumber := c.nodeNumber
	outputMode := c.outputMode

	if currentUser == nil {
		return nil, "", nil
	}

	for {
		// Display header
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n|15Auto-Signature|07\r\n")), outputMode)
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("|08An Auto-Signature is appended to the end of any message you post.|07\r\n\r\n")), outputMode)

		// Show current signature
		if currentUser.AutoSignature == "" {
			terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("|03You currently do not have an Auto-Signature.|07\r\n\r\n")), outputMode)
		} else {
			terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("|03Your current Auto-Signature is...|07\r\n\r\n")), outputMode)
			sigLines := strings.Split(currentUser.AutoSignature, "\n")
			for _, line := range sigLines {
				terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(line)), outputMode)
				terminalio.WriteProcessedBytes(terminal, []byte("\r\n"), outputMode)
			}
			terminalio.WriteProcessedBytes(terminal, []byte("\r\n"), outputMode)
		}

		// Menu prompt
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("|09C|07hange/create  |09D|07elete  |09Q|07uit : ")), outputMode)

		input, err := readLineFromSessionIH(s, terminal)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil, "LOGOFF", io.EOF
			}
			return currentUser, "", nil
		}

		input = strings.TrimSpace(strings.ToUpper(input))
		if input == "" || input == "Q" {
			return currentUser, "", nil
		}

		switch input {
		case "C":
			// Launch the ANSI editor with current signature as initial content
			ih := getSessionIH(s)
			terminalio.WriteProcessedBytes(terminal, []byte(ansi.ClearScreen()), outputMode)
			editorCtx := editor.EditorContext{
				NodeNumber: nodeNumber,
				ConfArea:   "Auto-Signature",
			}
			body, saved, edErr := editor.RunEditorWithMetadata(
				currentUser.AutoSignature, s, s, outputMode,
				"Auto-Signature", "All", currentUser.Handle, false,
				"", "", "", "", false, nil, ih, editorCtx,
			)
			terminalio.WriteProcessedBytes(terminal, []byte(ansi.ClearScreen()), outputMode)

			if edErr != nil {
				slog.Error("editor failed for auto-sig", "node", nodeNumber, "error", edErr)
				return currentUser, "", nil
			}
			if !saved {
				terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n|07Auto-Signature not changed.\r\n")), outputMode)
				time.Sleep(500 * time.Millisecond)
				continue
			}

			// Truncate to maxAutoSigLines lines
			body = strings.TrimRight(body, "\r\n")
			if body != "" {
				lines := strings.Split(body, "\n")
				if len(lines) > maxAutoSigLines {
					lines = lines[:maxAutoSigLines]
					terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(
						fmt.Sprintf("\r\n|03Signature truncated to %d lines.|07\r\n", maxAutoSigLines),
					)), outputMode)
				}
				body = strings.Join(lines, "\n")
			}

			currentUser.AutoSignature = body
			if err := userManager.UpdateUser(currentUser); err != nil {
				slog.Error("failed to save auto-signature", "node", nodeNumber, "error", err)
				return currentUser, "", nil
			}
			if body == "" {
				terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n|03Auto-Signature cleared.|07\r\n")), outputMode)
			} else {
				terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n|02Auto-Signature saved!|07\r\n")), outputMode)
			}
			time.Sleep(500 * time.Millisecond)

		case "D":
			if currentUser.AutoSignature == "" {
				terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n|03You don't have an Auto-Signature to delete!|07\r\n")), outputMode)
			} else {
				currentUser.AutoSignature = ""
				if err := userManager.UpdateUser(currentUser); err != nil {
					slog.Error("failed to delete auto-signature", "node", nodeNumber, "error", err)
					return currentUser, "", nil
				}
				terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n|03Auto-Signature has been deleted.|07\r\n")), outputMode)
			}
			time.Sleep(500 * time.Millisecond)
		}
	}
}
