package menu

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// welcomeMailTemplate is the welcome letter's file in the menu set's
// templates directory.
const welcomeMailTemplate = "WELCOME.MSG"

// welcomeMailDefaultSubject is used when the template has no Subject: line.
const welcomeMailDefaultSubject = "Welcome to |BN"

// deliverWelcomeMail sends the welcome letter (sendWelcomeMail, #263) to an
// account that signed up, has since been validated, and hasn't had it.
//
// It runs at login rather than where the account is validated. Validation
// happens in the admin editor, by a New User Vote, at signup with
// autoValidateNewUsers, and in ./ue, which runs outside the BBS and can't
// write mail; the next login follows all of them, and comes before the
// letter could be read anyway.
//
// WelcomePending is cleared whatever happens to the letter, so turning the
// setting on later doesn't welcome callers who have been here for months.
func (e *MenuExecutor) deliverWelcomeMail(userManager *user.UserMgr, u *user.User, nodeNumber int) {
	if u == nil || !u.WelcomePending || !u.Validated {
		return
	}
	u.WelcomePending = false
	if err := userManager.UpdateUser(u); err != nil {
		// Not sent: the flag is still set on disk, and a letter now would
		// be sent again at the next login.
		slog.Error("failed to clear welcome-mail pending", "node", nodeNumber, "handle", u.Handle, "error", err)
		return
	}
	cfg := e.GetServerConfig()
	if !cfg.SendWelcomeMail {
		return
	}
	// From the SysOp, user #1, who has no need of a letter from themselves.
	sysop, ok := newUserSysopRecipient(userManager, u.ID)
	if !ok {
		return
	}
	if e.MessageMgr == nil {
		return
	}
	area, ok := e.MessageMgr.GetAreaByTag("PRIVMAIL")
	if !ok {
		slog.Warn("welcome mail not sent: no PRIVMAIL area", "node", nodeNumber, "handle", u.Handle)
		return
	}
	path := filepath.Join(e.MenuSetPath, "templates", welcomeMailTemplate)
	raw, err := os.ReadFile(path)
	if err != nil {
		slog.Warn("welcome mail not sent: template unreadable", "node", nodeNumber, "handle", u.Handle, "path", path, "error", err)
		return
	}
	subject, body := welcomeMailFromTemplate(raw, cfg, u, sysop)
	if body == "" {
		slog.Warn("welcome mail not sent: template is empty", "node", nodeNumber, "path", path)
		return
	}
	if _, err := e.MessageMgr.Post(area.ID, sysop.Handle, u.Handle, subject, body, message.PostOptions{
		Private: true, SenderID: sysop.ID, RecipientID: u.ID,
	}); err != nil {
		slog.Error("failed to send welcome mail", "node", nodeNumber, "handle", u.Handle, "error", err)
		return
	}
	slog.Info("welcome mail sent", "node", nodeNumber, "handle", u.Handle)
}

// welcomeMailFromTemplate turns the template file into a subject and body
// for u. A first line "Subject: text" is the subject. The SAUCE record and
// anything after a DOS EOF mark are dropped, and the replacement codes are
// filled in: the user codes a menu prompt has (|UH, |NAME, |LEVEL ...)
// bar |UN, the SysOp's private note,
// |BN for the board, |SYSOP for the sender and |DATE. Colour pipe codes and
// ANSI escapes are left for the message reader to draw. The body is UTF-8,
// whichever of CP437 and UTF-8 the template is in.
func welcomeMailFromTemplate(raw []byte, cfg config.ServerConfig, u, sysop *user.User) (subject, body string) {
	data := ansi.StripSAUCE(raw)
	if i := bytes.IndexByte(data, 0x1A); i >= 0 {
		data = data[:i]
	}
	// The template may be CP437, as art is, or UTF-8; the values put into it
	// (handles, real names, the board's name) are UTF-8. It is read as the
	// reader would read it and the letter is UTF-8 throughout. Left as
	// CP437 bytes with UTF-8 names among them, the body would be valid as
	// neither, and the reader would draw "Björn" as "Bj├╢rn".
	text := ansi.DecodeBody(string(data), "")
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")

	subject = welcomeMailDefaultSubject
	if first, rest, _ := strings.Cut(text, "\n"); len(first) >= 8 && strings.EqualFold(first[:8], "subject:") {
		if s := strings.TrimSpace(first[8:]); s != "" {
			subject = s
		}
		text = rest
	}
	text = strings.Trim(text, "\n")
	if strings.TrimSpace(text) == "" {
		return "", ""
	}

	codes := map[string]string{
		"|BN":    cfg.BoardName,
		"|SYSOP": callerText(sysop.Handle),
		"|DATE":  config.NowIn(cfg.Timezone).Format("01/02/06"),
	}
	applyUserPlaceholders(codes, u)
	delete(codes, "|UN") // the SysOp's private note about the caller
	body = replacePlaceholders(text, codes)
	if strings.IndexByte(body, 0x1B) >= 0 {
		body += "\x1b[0m" // so the reader's footer isn't drawn in the art's last colour
	}
	return replacePlaceholders(subject, codes), body
}
