package menu

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"github.com/gliderlabs/ssh"
	"golang.org/x/term"
)

// newUserSysopRecipient resolves the SysOp account a new-user introduction
// message is addressed to. By convention that is user #1 (the same account the
// user editor treats as the SysOp). excludeID is the freshly created account:
// on a brand-new board the first signup can itself be user #1, and there is no
// point mailing yourself, so that case is treated as "no recipient".
func newUserSysopRecipient(um *user.UserMgr, excludeID int) (*user.User, bool) {
	u, ok := um.GetUserByID(1)
	if !ok || u == nil || u.ID == excludeID {
		return nil, false
	}
	return u, true
}

// requireNewUserSysopEmail is the optional final step of signup, gated by
// requireNewUserEmail. It shows the caller NUEMAIL.ANS (or a configured
// fallback string), pauses, and then drops them straight into the message
// editor addressed to the SysOp, saving the result as private mail.
//
// It is deliberately forgiving past that point: the account already exists, so
// a caller who aborts or writes nothing is not trapped — the requirement is
// expressed by putting the step in front of every new user, not by refusing to
// let them leave. Only a dropped connection (io.EOF) propagates, so the
// caller's logoff bookkeeping runs; anything else is logged and swallowed so a
// misconfiguration cannot break signup.
func (e *MenuExecutor) requireNewUserSysopEmail(
	s ssh.Session,
	terminal *term.Terminal,
	userManager *user.UserMgr,
	newUser *user.User,
	nodeNumber int,
	outputMode ansi.OutputMode,
	termWidth, termHeight int,
) error {
	sysop, ok := newUserSysopRecipient(userManager, newUser.ID)
	if !ok {
		slog.Info("skipping new-user sysop email: no distinct sysop account (user #1)",
			"node", nodeNumber, "handle", newUser.Handle)
		return nil
	}

	privmailArea, exists := e.MessageMgr.GetAreaByTag("PRIVMAIL")
	if !exists {
		slog.Error("skipping new-user sysop email: PRIVMAIL area not configured", "node", nodeNumber)
		return nil
	}

	// Introduce the step: the customizable art if present, otherwise the string.
	if err := e.displayNewUserEmailScreen(terminal, outputMode, nodeNumber); err != nil {
		slog.Warn("failed to display NUEMAIL.ANS", "node", nodeNumber, "error", err)
	}
	if !e.newUserEmailScreenExists() {
		prompt := e.LoadedStrings.NewUserEmailPrompt
		if prompt == "" {
			prompt = "\r\n|15Before you go, please leave the |14SysOp|15 a private message so they\r\n" +
				"know who you are. Your account may not be validated without it.|07\r\n"
		}
		terminalio.WriteStringCP437(terminal, ansi.ReplacePipeCodes([]byte(prompt)), outputMode)
	}

	// Auto-pause before proceeding into the editor.
	terminalio.WriteProcessedBytes(terminal, []byte("\r\n"), outputMode)
	e.holdScreen(s, terminal, outputMode, termWidth, termHeight)

	// Default subject carries the handle so the SysOp can tell applications
	// apart at a glance in their inbox.
	subjectFmt := e.LoadedStrings.NewUserEmailSubject
	if subjectFmt == "" {
		subjectFmt = "New user application - %s"
	}
	subject := subjectFmt
	if strings.Contains(subjectFmt, "%s") {
		subject = fmt.Sprintf(subjectFmt, newUser.Handle)
	}

	terminalio.WriteProcessedBytes(terminal, []byte(ansi.ClearScreen()), outputMode)

	nextMsg := 0
	if msgCount, mcErr := e.MessageMgr.GetMessageCountForArea(privmailArea.ID); mcErr == nil {
		nextMsg = msgCount + 1
	}
	editorCtx := editor.EditorContext{
		NodeNumber: nodeNumber,
		NextMsgNum: nextMsg,
		ConfArea:   "Private Mail",
	}
	body, saved, err := editor.RunEditorWithMetadata("", s, s, outputMode, subject,
		sysop.Handle, newUser.Handle, false, "", "", "", "", false, nil, getSessionIH(s), editorCtx)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return io.EOF
		}
		slog.Error("editor failed during new-user sysop email", "node", nodeNumber, "handle", newUser.Handle, "error", err)
		return nil
	}

	terminalio.WriteProcessedBytes(terminal, []byte(ansi.ClearScreen()), outputMode)

	if !saved || strings.TrimSpace(body) == "" {
		slog.Info("new user left no sysop message", "node", nodeNumber, "handle", newUser.Handle, "saved", saved)
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes(
			[]byte("\r\n|08No message left for the SysOp.|07\r\n")), outputMode)
		time.Sleep(1 * time.Second)
		return nil
	}

	if _, err := e.MessageMgr.AddPrivateMessage(privmailArea.ID, newUser.Handle, sysop.Handle, subject, body, ""); err != nil {
		slog.Error("failed to save new-user sysop email", "node", nodeNumber, "handle", newUser.Handle, "error", err)
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes(
			[]byte("\r\n|01Error saving your message.|07\r\n")), outputMode)
		time.Sleep(2 * time.Second)
		return nil
	}

	slog.Info("new user left a message for the sysop", "node", nodeNumber, "handle", newUser.Handle, "sysop", sysop.Handle)
	terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes(
		[]byte(fmt.Sprintf("\r\n|02Your message has been sent to %s.|07\r\n", sysop.Handle))), outputMode)
	time.Sleep(1 * time.Second)
	return nil
}

// newUserEmailArtPath is the customizable art shown at the start of the
// require-email step. Like NEWUSER.ANS it lives in the active menu set's ansi
// directory and is optional.
func (e *MenuExecutor) newUserEmailArtPath() string {
	return filepath.Join(e.MenuSetPath, "ansi", "NUEMAIL.ANS")
}

// newUserEmailScreenExists reports whether the customizable art is present, so
// the caller can fall back to the configured string when it is not.
func (e *MenuExecutor) newUserEmailScreenExists() bool {
	_, err := os.Stat(e.newUserEmailArtPath())
	return err == nil
}

// displayNewUserEmailScreen loads and displays NUEMAIL.ANS if present. A
// missing file is not an error — the caller shows the fallback string instead.
func (e *MenuExecutor) displayNewUserEmailScreen(terminal *term.Terminal, outputMode ansi.OutputMode, nodeNumber int) error {
	rawContent, err := ansi.GetAnsiFileContent(e.newUserEmailArtPath())
	if err != nil {
		if os.IsNotExist(err) {
			slog.Debug("NUEMAIL.ANS not found, using fallback string", "node", nodeNumber)
			return nil
		}
		return fmt.Errorf("failed to read NUEMAIL.ANS: %w", err)
	}

	terminalio.WriteProcessedBytes(terminal, []byte(ansi.ClearScreen()), outputMode)
	// For CP437 mode, write raw bytes directly to avoid UTF-8 false positives
	// (some CP437 byte pairs accidentally form valid UTF-8).
	if outputMode == ansi.OutputModeCP437 {
		_, _ = terminal.Write(rawContent) // best-effort display
	} else {
		terminalio.WriteProcessedBytes(terminal, rawContent, outputMode)
	}
	return nil
}
