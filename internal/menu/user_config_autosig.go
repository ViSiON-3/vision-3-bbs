package menu

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/gliderlabs/ssh"
	"golang.org/x/term"

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
		if currentUser.AutoSigAsk {
			terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("|03You're asked before it is added to each message.|07\r\n\r\n")), outputMode)
		}

		// Menu prompt
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("|09C|07hange/create  |09D|07elete  |09A|07sk each post  |09Q|07uit : ")), outputMode)

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
			body, saved, truncated, edErr := runAutoSigEditor(c, currentUser)
			if edErr != nil {
				if errors.Is(edErr, errAutoSigEditorFailed) {
					return currentUser, "", nil
				}
				return currentUser, "", edErr
			}
			if !saved {
				terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n|07Auto-Signature not changed.\r\n")), outputMode)
				uiPause(500 * time.Millisecond)
				continue
			}
			if truncated {
				terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(
					fmt.Sprintf("\r\n|03Signature truncated to %d lines.|07\r\n", maxAutoSigLines),
				)), outputMode)
			}

			originalSig := currentUser.AutoSignature
			currentUser.AutoSignature = body
			if err := userManager.UpdateUser(currentUser); err != nil {
				currentUser.AutoSignature = originalSig
				slog.Error("failed to save auto-signature", "node", nodeNumber, "error", err)
				return currentUser, "", nil
			}
			if body == "" {
				terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n|03Auto-Signature cleared.|07\r\n")), outputMode)
			} else {
				terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n|02Auto-Signature saved!|07\r\n")), outputMode)
			}
			uiPause(500 * time.Millisecond)

		case "D":
			if currentUser.AutoSignature == "" {
				terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n|03You don't have an Auto-Signature to delete!|07\r\n")), outputMode)
			} else {
				originalSig := currentUser.AutoSignature
				currentUser.AutoSignature = ""
				if err := userManager.UpdateUser(currentUser); err != nil {
					currentUser.AutoSignature = originalSig
					slog.Error("failed to delete auto-signature", "node", nodeNumber, "error", err)
					return currentUser, "", nil
				}
				terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n|03Auto-Signature has been deleted.|07\r\n")), outputMode)
			}
			uiPause(500 * time.Millisecond)

		case "A":
			currentUser.AutoSigAsk = !currentUser.AutoSigAsk
			if err := userManager.UpdateUser(currentUser); err != nil {
				currentUser.AutoSigAsk = !currentUser.AutoSigAsk
				slog.Error("failed to save auto-signature ask setting", "node", nodeNumber, "error", err)
				return currentUser, "", nil
			}
			if currentUser.AutoSigAsk {
				terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n|02You'll be asked before your Auto-Signature is added.|07\r\n")), outputMode)
			} else {
				terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n|02Your Auto-Signature is added to every message.|07\r\n")), outputMode)
			}
			uiPause(500 * time.Millisecond)
		}
	}
}

// errAutoSigEditorFailed reports that the message editor could not be run
// for the auto-signature. It has already been logged.
var errAutoSigEditorFailed = errors.New("auto-signature editor failed")

// runAutoSigEditor opens the full-screen editor on u's auto-signature and
// returns the edited text, trimmed of trailing newlines and cut to
// maxAutoSigLines (truncated reports a cut). saved is false when the caller
// abandoned the edit. Nothing is stored: that is the caller's job.
func runAutoSigEditor(c *cmdCtx, u *user.User) (body string, saved, truncated bool, err error) {
	terminalio.WriteProcessedBytes(c.terminal, []byte(ansi.ClearScreen()), c.outputMode)
	editorCtx := editor.EditorContext{
		NodeNumber: c.nodeNumber,
		ConfArea:   "Auto-Signature",
	}
	body, saved, edErr := editor.RunEditorWithMetadata(
		u.AutoSignature, c.s, sessionOutput(c.s), c.outputMode,
		"Auto-Signature", "All", u.Handle, false,
		"", "", "", "", false, nil, getSessionIH(c.s), editorCtx,
	)
	terminalio.WriteProcessedBytes(c.terminal, []byte(ansi.ClearScreen()), c.outputMode)
	if edErr != nil {
		if errors.Is(edErr, io.EOF) || errors.Is(edErr, editor.ErrIdleTimeout) {
			return "", false, false, edErr
		}
		slog.Error("editor failed for auto-sig", "node", c.nodeNumber, "error", edErr)
		return "", false, false, errAutoSigEditorFailed
	}
	if !saved {
		return "", false, false, nil
	}
	body = strings.TrimRight(body, "\r\n")
	if body != "" {
		lines := strings.Split(body, "\n")
		if len(lines) > maxAutoSigLines {
			lines = lines[:maxAutoSigLines]
			truncated = true
		}
		body = strings.Join(lines, "\n")
	}
	return body, true, truncated, nil
}

// appendAutoSig adds the caller's Auto-Signature to a message body they have
// just saved. With AutoSigAsk set the caller is asked first, Yes by default.
// A failed or dropped prompt counts as Yes, so the message is still posted the
// way it always was rather than lost over the question.
func (e *MenuExecutor) appendAutoSig(s ssh.Session, terminal *term.Terminal, u *user.User, body string,
	outputMode ansi.OutputMode, nodeNumber, termWidth, termHeight int) string {
	if u.AutoSignature == "" {
		return body
	}
	if u.AutoSigAsk {
		terminalio.WriteProcessedBytes(terminal, []byte("\r\n"), outputMode)
		add, err := e.PromptYesNoString(s, terminal, e.Strings().MsgAutoSigPrompt, outputMode, nodeNumber, termWidth, termHeight, true)
		terminalio.WriteProcessedBytes(terminal, []byte("\r\n"), outputMode)
		if err != nil {
			slog.Warn("auto-signature prompt failed, adding signature", "node", nodeNumber, "handle", u.Handle, "error", err)
			add = true
		}
		if !add {
			return body
		}
	}
	return body + "\n\n" + u.AutoSignature
}
