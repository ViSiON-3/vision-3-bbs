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

// newUserIntroMaxAttempts is how many times a caller may reach the required
// intro gate and leave without sending before the account is soft-deleted. The
// signup itself counts as the first, so this allows the signup plus two
// reconnects.
const newUserIntroMaxAttempts = 3

// runNewUserIntroGate runs the required-intro gate for a user who still owes the
// SysOp a message and applies the persistent consequences. It is called both at
// signup and, on reconnect, from the login flow.
//
// Returns (proceed, sent, err):
//   - proceed=false with io.EOF: the caller disconnected without sending. The
//     attempt has been counted and persisted; when it reaches the limit the
//     account is soft-deleted. The session must end.
//   - proceed=true, sent=true: a message was sent; IntroPending is cleared.
//   - proceed=true, sent=false: the gate could not be delivered (no distinct
//     SysOp, no PRIVMAIL area, or a non-EOF editor failure). IntroPending is
//     cleared so a sysop misconfiguration does not trap the caller forever, and
//     login/signup is allowed to continue.
func (e *MenuExecutor) runNewUserIntroGate(
	s ssh.Session,
	terminal *term.Terminal,
	userManager *user.UserMgr,
	u *user.User,
	nodeNumber int,
	outputMode ansi.OutputMode,
	termWidth, termHeight int,
) (proceed, sent bool, err error) {
	sent, err = e.requireNewUserSysopEmail(s, terminal, userManager, u, nodeNumber, outputMode, termWidth, termHeight)
	if errors.Is(err, io.EOF) {
		if recordAbandonedIntroAttempt(u, time.Now()) {
			slog.Info("soft-deleting new user who never left the required sysop message",
				"node", nodeNumber, "handle", u.Handle, "attempts", u.IntroAttempts)
		}
		if upErr := userManager.UpdateUser(u); upErr != nil {
			slog.Error("failed to persist new-user intro state", "node", nodeNumber, "handle", u.Handle, "error", upErr)
		}
		return false, false, io.EOF
	}

	// Sent, or undeliverable: clear the obligation either way.
	if u.IntroPending {
		u.IntroPending = false
		if upErr := userManager.UpdateUser(u); upErr != nil {
			slog.Error("failed to clear new-user intro pending", "node", nodeNumber, "handle", u.Handle, "error", upErr)
		}
	}
	return true, sent, nil
}

// recordAbandonedIntroAttempt bumps the attempt counter for a caller who left
// the required intro gate without sending, and reports whether that tips the
// account over the limit — in which case it is soft-deleted. Split out from
// runNewUserIntroGate so the counting and removal rule is unit-testable without
// a live editor session.
func recordAbandonedIntroAttempt(u *user.User, now time.Time) (removed bool) {
	u.IntroAttempts++
	if u.IntroAttempts >= newUserIntroMaxAttempts {
		u.DeletedUser = true
		u.DeletedAt = &now
		u.IntroPending = false
		return true
	}
	u.IntroPending = true
	return false
}

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
// fallback string), pauses, and then drops them into the message editor
// addressed to the SysOp, saving the result as private mail.
//
// This is the classic BBS "leave the SysOp feedback to finish" gate: the caller
// cannot skip past it. Aborting the editor or saving an empty body re-prompts
// and returns them to the editor — the only way out is to send a message or to
// drop the connection. It returns (sent, err): sent is true once a message is
// saved, so the caller can tell them they are being logged in.
//
// Only a dropped connection (io.EOF) propagates as an error, so the caller's
// logoff bookkeeping runs. The gate is skipped (returning false) only when it
// cannot possibly be satisfied — no distinct SysOp account, no PRIVMAIL area,
// or a non-EOF editor failure — since trapping a caller in a loop that can
// never deliver would be worse than letting signup finish.
func (e *MenuExecutor) requireNewUserSysopEmail(
	s ssh.Session,
	terminal *term.Terminal,
	userManager *user.UserMgr,
	newUser *user.User,
	nodeNumber int,
	outputMode ansi.OutputMode,
	termWidth, termHeight int,
) (bool, error) {
	sysop, ok := newUserSysopRecipient(userManager, newUser.ID)
	if !ok {
		slog.Info("skipping new-user sysop email: no distinct sysop account (user #1)",
			"node", nodeNumber, "handle", newUser.Handle)
		return false, nil
	}

	privmailArea, exists := e.MessageMgr.GetAreaByTag("PRIVMAIL")
	if !exists {
		slog.Error("skipping new-user sysop email: PRIVMAIL area not configured", "node", nodeNumber)
		return false, nil
	}

	// Introduce the step once: the customizable art if it actually renders,
	// otherwise the string. Basing the fallback on whether the art was shown
	// (not merely present) covers a transient read error, which would otherwise
	// leave the caller with no instruction before the editor. The retry loop
	// below does not re-show it — only a short reminder.
	displayed, err := e.displayNewUserEmailScreen(terminal, outputMode, nodeNumber)
	if err != nil {
		slog.Warn("failed to display NUEMAIL.ANS", "node", nodeNumber, "error", err)
	}
	if !displayed {
		terminalio.WriteStringCP437(terminal, ansi.ReplacePipeCodes([]byte(e.Strings().NewUserEmailPrompt)), outputMode)
	}

	// Auto-pause before proceeding into the editor.
	terminalio.WriteProcessedBytes(terminal, []byte("\r\n"), outputMode)
	e.holdScreen(s, terminal, outputMode, termWidth, termHeight)

	// Default subject carries the handle so the SysOp can tell applications
	// apart at a glance in their inbox. The %s guard tolerates a sysop who
	// edited the subject string and removed the placeholder.
	subjectFmt := e.Strings().NewUserEmailSubject
	subject := subjectFmt
	if strings.Contains(subjectFmt, "%s") {
		subject = fmt.Sprintf(subjectFmt, newUser.Handle)
	}

	nextMsg := 0
	if msgCount, mcErr := e.MessageMgr.GetMessageCountForArea(privmailArea.ID); mcErr == nil {
		nextMsg = msgCount + 1
	}
	editorCtx := editor.EditorContext{
		NodeNumber: nodeNumber,
		NextMsgNum: nextMsg,
		ConfArea:   "Private Mail",
	}

	// No-escape loop: keep returning them to the editor until they save a
	// non-empty message. The connection dropping (io.EOF) is the only exit that
	// is not a sent message.
	for {
		terminalio.WriteProcessedBytes(terminal, []byte(ansi.ClearScreen()), outputMode)

		body, saved, err := editor.RunEditorWithMetadata("", s, s, outputMode, subject,
			sysop.Handle, newUser.Handle, false, "", "", "", "", false, nil, getSessionIH(s), editorCtx)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return false, io.EOF
			}
			// A non-EOF editor failure cannot be recovered by retrying; do not
			// trap the caller in a broken loop.
			slog.Error("editor failed during new-user sysop email", "node", nodeNumber, "handle", newUser.Handle, "error", err)
			return false, nil
		}

		terminalio.WriteProcessedBytes(terminal, []byte(ansi.ClearScreen()), outputMode)

		if !saved || strings.TrimSpace(body) == "" {
			slog.Info("new user tried to skip the sysop message", "node", nodeNumber, "handle", newUser.Handle, "saved", saved)
			terminalio.WriteStringCP437(terminal, ansi.ReplacePipeCodes([]byte(e.Strings().NewUserEmailRequired)), outputMode)
			e.holdScreen(s, terminal, outputMode, termWidth, termHeight)
			continue
		}

		if _, err := e.MessageMgr.AddPrivateMessage(privmailArea.ID, newUser.Handle, sysop.Handle, subject, body, ""); err != nil {
			slog.Error("failed to save new-user sysop email", "node", nodeNumber, "handle", newUser.Handle, "error", err)
			terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes(
				[]byte("\r\n|01Error saving your message.|07\r\n")), outputMode)
			time.Sleep(2 * time.Second)
			// The store, not the caller, is at fault, and retrying cannot fix a
			// persistent fault. Give up the same way a non-EOF editor failure
			// does: don't trap them in an undeliverable loop whose only exit is
			// a disconnect that would count against them and eventually delete
			// the account.
			return false, nil
		}

		slog.Info("new user left a message for the sysop", "node", nodeNumber, "handle", newUser.Handle, "sysop", sysop.Handle)
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes(
			[]byte(fmt.Sprintf("\r\n|02Your message has been sent to %s.|07\r\n", sysop.Handle))), outputMode)
		time.Sleep(1 * time.Second)
		return true, nil
	}
}

// newUserEmailArtPath is the customizable art shown at the start of the
// require-email step. Like NEWUSER.ANS it lives in the active menu set's ansi
// directory and is optional.
func (e *MenuExecutor) newUserEmailArtPath() string {
	return filepath.Join(e.MenuSetPath, "ansi", "NUEMAIL.ANS")
}

// displayNewUserEmailScreen loads and displays NUEMAIL.ANS. It returns
// displayed=true only when the art was actually written, so the caller shows
// the fallback string on both a missing file (no error) and a read failure
// (error), rather than leaving the caller with no instruction at all.
func (e *MenuExecutor) displayNewUserEmailScreen(terminal *term.Terminal, outputMode ansi.OutputMode, nodeNumber int) (displayed bool, err error) {
	rawContent, err := ansi.GetAnsiFileContent(e.newUserEmailArtPath())
	if err != nil {
		if os.IsNotExist(err) {
			slog.Debug("NUEMAIL.ANS not found, using fallback string", "node", nodeNumber)
			return false, nil
		}
		return false, fmt.Errorf("failed to read NUEMAIL.ANS: %w", err)
	}

	terminalio.WriteProcessedBytes(terminal, []byte(ansi.ClearScreen()), outputMode)
	// For CP437 mode, write raw bytes directly to avoid UTF-8 false positives
	// (some CP437 byte pairs accidentally form valid UTF-8).
	if outputMode == ansi.OutputModeCP437 {
		_, _ = terminal.Write(rawContent) // best-effort display
	} else {
		terminalio.WriteProcessedBytes(terminal, rawContent, outputMode)
	}
	return true, nil
}
