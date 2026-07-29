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

// runSendPrivateMail handles sending private mail to another user.
// It validates the recipient exists and sets the MSG_PRIVATE flag.
func runSendPrivateMail(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	s := c.s
	terminal := c.terminal
	userManager := c.userManager
	currentUser := c.currentUser
	nodeNumber := c.nodeNumber
	outputMode := c.outputMode
	termWidth := c.termWidth
	termHeight := c.termHeight

	slog.Debug("running SENDPRIVMAIL", "node", nodeNumber)

	if currentUser == nil {
		msg := "\r\n|01Error: You must be logged in to send private mail.|07\r\n"
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		time.Sleep(1 * time.Second)
		return nil, "", nil
	}

	// Get PRIVMAIL area
	privmailArea, exists := e.MessageMgr.GetAreaByTag("PRIVMAIL")
	if !exists {
		slog.Error("PRIVMAIL area not found", "node", nodeNumber)
		msg := "\r\n|01Error: Private mail area not configured.|07\r\n"
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		time.Sleep(1 * time.Second)
		return nil, "", nil
	}

	// Prompt for recipient username
	terminalio.WriteProcessedBytes(terminal, []byte("\r\n"), outputMode)
	recipientPrompt := "|07Send private mail to: |15"
	var recipient string
	for {
		wErr := terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(recipientPrompt)), outputMode)
		if wErr != nil {
			slog.Warn("failed to write recipient prompt", "node", nodeNumber, "error", wErr)
		}
		var inputErr error
		recipient, inputErr = styledInput(terminal, s, outputMode, 24, "")
		if inputErr != nil {
			if errors.Is(inputErr, io.EOF) {
				slog.Info("user disconnected during recipient input", "node", nodeNumber)
				return nil, "LOGOFF", io.EOF
			}
			if errors.Is(inputErr, errInputAborted) {
				abort, confirmErr := e.confirmAbortPost(s, terminal, outputMode, nodeNumber, termWidth, termHeight)
				if confirmErr != nil {
					if errors.Is(confirmErr, io.EOF) {
						return nil, "LOGOFF", io.EOF
					}
					return nil, "", nil
				}
				if abort {
					return nil, "", nil
				}
				continue // No — re-show prompt and retry
			}
			slog.Error("failed reading recipient input", "node", nodeNumber, "error", inputErr)
			terminalio.WriteProcessedBytes(terminal, []byte("\r\nError reading recipient.\r\n"), outputMode)
			time.Sleep(1 * time.Second)
			return nil, "", nil
		}
		break
	}
	recipient = strings.TrimSpace(recipient)
	if recipient == "" {
		msg := "\r\n|01Recipient cannot be empty.|07\r\n"
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		time.Sleep(1 * time.Second)
		return nil, "", nil
	}

	// Validate recipient user exists
	recipientUser, found := userManager.GetUser(recipient)
	if !found || recipientUser == nil {
		msg := fmt.Sprintf("\r\n|01Error: User '%s' not found.|07\r\n", recipient)
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		time.Sleep(1 * time.Second)
		return nil, "", nil
	}

	// Prompt for subject
	titlePrompt := "|07Subject: |15"
	var subject string
	for {
		if wErr := terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(titlePrompt)), outputMode); wErr != nil {
			slog.Warn("failed to write subject prompt", "node", nodeNumber, "error", wErr)
		}
		var inputErr error
		subject, inputErr = styledInput(terminal, s, outputMode, 30, "")
		if inputErr != nil {
			if errors.Is(inputErr, io.EOF) {
				slog.Info("user disconnected during subject input", "node", nodeNumber)
				return nil, "LOGOFF", io.EOF
			}
			if errors.Is(inputErr, errInputAborted) {
				abort, confirmErr := e.confirmAbortPost(s, terminal, outputMode, nodeNumber, termWidth, termHeight)
				if confirmErr != nil {
					if errors.Is(confirmErr, io.EOF) {
						return nil, "LOGOFF", io.EOF
					}
					return nil, "", nil
				}
				if abort {
					return nil, "", nil
				}
				continue // No — re-show prompt and retry
			}
			slog.Error("failed reading subject input", "node", nodeNumber, "error", inputErr)
			terminalio.WriteProcessedBytes(terminal, []byte("\r\nError reading subject.\r\n"), outputMode)
			time.Sleep(1 * time.Second)
			return nil, "", nil
		}
		subject = strings.TrimSpace(subject)
		if subject != "" {
			break
		}
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("|01Subject is required.|07\r\n")), outputMode)
	}

	// Launch editor
	slog.Debug("clearing screen before calling editor for private mail", "node", nodeNumber)
	terminalio.WriteProcessedBytes(terminal, []byte(ansi.ClearScreen()), outputMode)

	// Launch editor for private mail (no anonymous option for private mail)
	privNextMsg := 0
	if msgCount, mcErr := e.MessageMgr.GetMessageCountForArea(privmailArea.ID); mcErr == nil {
		privNextMsg = msgCount + 1
	}
	privEditorCtx := editor.EditorContext{
		NodeNumber: nodeNumber,
		NextMsgNum: privNextMsg,
		ConfArea:   "Private Mail",
	}
	// Share the session-scoped InputHandler with the editor; passing nil would
	// spawn a second reader goroutine on the session that races the menu's
	// reader for bytes (the "double key press" bug).
	body, saved, err := editor.RunEditorWithMetadata("", s, s, outputMode, subject, recipientUser.Handle, currentUser.Handle, false, "", "", "", "", false, nil, getSessionIH(s), privEditorCtx)
	slog.Debug("editor returned", "node", nodeNumber, "error", err, "saved", saved, "length", len(body))

	if err != nil {
		slog.Error("editor failed", "node", nodeNumber, "handle", currentUser.Handle, "error", err)
		return nil, "", fmt.Errorf("editor error: %w", err)
	}

	// Clear screen after editor exits
	terminalio.WriteProcessedBytes(terminal, []byte(ansi.ClearScreen()), outputMode)

	if !saved {
		slog.Info("user aborted private mail composition", "node", nodeNumber, "handle", currentUser.Handle)
		terminalio.WriteProcessedBytes(terminal, []byte("\r\nMessage aborted.\r\n"), outputMode)
		time.Sleep(1 * time.Second)
		return nil, "", nil
	}

	if strings.TrimSpace(body) == "" {
		slog.Info("user saved empty private mail", "node", nodeNumber, "handle", currentUser.Handle)
		terminalio.WriteProcessedBytes(terminal, []byte("\r\nMessage body empty. Aborting.\r\n"), outputMode)
		time.Sleep(1 * time.Second)
		return nil, "", nil
	}

	// Append auto-signature if user has one
	if currentUser.AutoSignature != "" {
		body = body + "\n\n" + currentUser.AutoSignature
	}

	// Save the private message with MSG_PRIVATE flag
	msgNum, err := e.MessageMgr.AddPrivateMessage(privmailArea.ID, currentUser.Handle, recipientUser.Handle, subject, body, "")
	if err != nil {
		slog.Error("failed to save private message", "node", nodeNumber, "handle", currentUser.Handle, "recipient", recipientUser.Handle, "error", err)
		errorMsg := ansi.ReplacePipeCodes([]byte("\r\n|01Error saving private message!|07\r\n"))
		terminalio.WriteProcessedBytes(terminal, errorMsg, outputMode)
		time.Sleep(2 * time.Second)
		return nil, "", fmt.Errorf("failed saving private message: %w", err)
	}

	// Update user message counter
	currentUser.MessagesPosted++
	if err := userManager.UpdateUser(currentUser); err != nil {
		slog.Error("failed to update MessagesPosted", "node", nodeNumber, "handle", currentUser.Handle, "error", err)
	}

	// Confirmation
	slog.Info("user sent private message", "node", nodeNumber, "handle", currentUser.Handle, "num", msgNum, "recipient", recipientUser.Handle)
	confirmMsg := ansi.ReplacePipeCodes([]byte(fmt.Sprintf("\r\n|02Private message sent to %s!|07\r\n", recipientUser.Handle)))
	terminalio.WriteProcessedBytes(terminal, confirmMsg, outputMode)
	time.Sleep(1 * time.Second)

	return nil, "", nil
}
