package menu

import (
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gliderlabs/ssh"
	"golang.org/x/term"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// Editing a posted message from the reader (E): ViSiON/2's ME. The author
// may edit their own message, an area's sponsor any message in it, and a
// co-sysop any message, in local public areas only. Echomail, netmail, QWK
// network and V3Net copies have already left the board, so an edit would
// change ours alone, and private mail has already been delivered.

// editableArea reports whether messages in area may be edited: a local area
// other than private mail that no network shares (MessageManager.IsNetworked,
// which also counts a local area a V3Net leaf binds).
func editableArea(mm *message.MessageManager, area *message.MessageArea) bool {
	if area == nil || strings.EqualFold(area.Tag, "PRIVMAIL") {
		return false
	}
	return !mm.IsNetworked(area)
}

// ownMessage reports whether u wrote m. A post signed with a real name keeps
// the poster's handle (DisplayMessage.SenderHandle); one signed with the
// handle keeps nothing, and its From is the handle. An anonymous post keeps
// no handle, so its author can't be told and it is never anyone's own.
func ownMessage(m *message.DisplayMessage, u *user.User, anonymousName string) bool {
	if u == nil {
		return false
	}
	if m.SenderHandle != "" {
		return strings.EqualFold(m.SenderHandle, u.Handle)
	}
	from := strings.TrimSpace(m.From)
	if from == "" || strings.EqualFold(from, strings.TrimSpace(anonymousName)) {
		return false
	}
	return strings.EqualFold(from, u.Handle)
}

// editedLineFormat is the line added to the end of an edited message: who
// edited it and when.
const editedLineFormat = " * Edited by %s on %s"

// editedLine matches a line editedLineFormat wrote, so a message edited
// again carries only its latest one.
var editedLine = regexp.MustCompile(`^ \* Edited by .+ on \d\d/\d\d/\d\d at \d{1,2}:\d\d [ap]m$`)

// withoutEditedLine is body less a last line saying who edited it, and the
// blank lines before that.
func withoutEditedLine(body string) string {
	trimmed := strings.TrimRight(body, "\n ")
	i := strings.LastIndex(trimmed, "\n")
	if !editedLine.MatchString(trimmed[i+1:]) {
		return body
	}
	return strings.TrimRight(trimmed[:max(i, 0)], "\n ")
}

// withEditedLine is body ending with a line saying handle edited it at when.
func withEditedLine(body, handle string, when time.Time) string {
	return strings.TrimRight(body, "\n ") + "\n\n" +
		fmt.Sprintf(editedLineFormat, handle, strings.ToLower(when.Format("01/02/06 at 3:04 PM")))
}

// minEditSubject is the shortest the subject prompt allows: what a new post
// is given. A longer subject, such as a reply's "Re: ...", gets its length.
const minEditSubject = 30

// editPostedMessage edits msg, which the reader is showing in area, for
// currentUser: the subject at a prompt that starts with the current one,
// then the text in the full-screen editor. may reports whether the caller
// may edit a message, as a sponsor or not; sponsor tells whether they are
// one now. Both are asked again as the message is saved, under the base's
// lock, along with whether it is still the message shown. It returns true
// when the message was saved, and io.EOF when the caller hung up.
func (e *MenuExecutor) editPostedMessage(s ssh.Session, ih *editor.InputHandler, terminal *term.Terminal,
	userManager *user.UserMgr, currentUser *user.User, nodeNumber int, outputMode ansi.OutputMode,
	area *message.MessageArea, msg *message.DisplayMessage, confName string,
	may func(m *message.DisplayMessage, sponsor bool) bool, sponsor func() bool,
	termWidth, termHeight int) (bool, error) {

	notice := func(text string) {
		e.showReaderNotice(terminal, outputMode, text, termHeight)
	}

	limit := max(minEditSubject, utf8.RuneCountInString(msg.Subject))
	val, aborted, err := e.promptComposeField(s, terminal, "\r\n|07Subject: |15", limit, msg.Subject, "subject", outputMode, nodeNumber, termWidth, termHeight)
	if err != nil {
		return false, err
	}
	if aborted {
		return false, nil
	}
	subject := strings.TrimSpace(val)
	if subject == "" {
		subject = msg.Subject
	}

	e.writeString(terminal, outputMode, e.Strings().MsgLaunchingEditor)
	ctx := editor.EditorContext{
		NodeNumber: nodeNumber,
		NextMsgNum: msg.MsgNum,
		ConfArea:   fmt.Sprintf("%s > %s", confName, area.Name),
	}
	body, saved, err := editor.RunEditorWithMetadata(withoutEditedLine(msg.Body), s, sessionOutput(s), outputMode,
		subject, msg.To, msg.From, false, "", "", "", "", false, nil, ih,
		withAutocorrect(e, ctx, area, currentUser, userManager))
	if err != nil {
		if errors.Is(err, editor.ErrIdleTimeout) {
			return false, err
		}
		slog.Error("editor failed", "node", nodeNumber, "error", err)
		e.writeString(terminal, outputMode, e.Strings().MsgEditorError)
		uiPause(2 * time.Second)
		return false, nil
	}
	if !saved {
		notice("|07Edit cancelled. The message is as it was.")
		return false, nil
	}
	// In the board's time zone, as the editor's clock shows it.
	body = withEditedLine(body, currentUser.Handle, config.NowIn(e.GetServerConfig().Timezone))

	// The right to edit is checked again now: the sysop may have changed the
	// area's sponsor while the caller typed. Then, under the base's lock, the
	// message must still be the one shown, and still theirs to edit.
	isSponsor := sponsor()
	if !may(msg, isSponsor) {
		notice("|01You can no longer edit this message. Nothing was saved.|07")
		return false, nil
	}
	err = e.MessageMgr.EditMessageIf(area.ID, msg.MsgNum, func(m *message.DisplayMessage) (string, string, bool) {
		if !message.SameMessage(m, msg) || !may(m, isSponsor) {
			return "", "", false
		}
		return subject, body, true
	})
	if errors.Is(err, message.ErrMessageChanged) {
		notice("|01That message was changed or deleted elsewhere. Nothing was saved.|07")
		return false, nil
	}
	if err != nil {
		slog.Error("edit message", "node", nodeNumber, "area", area.ID, "msg", msg.MsgNum, "error", err)
		notice("|01Error saving the message.|07")
		return false, nil
	}
	slog.Info("message edited", "node", nodeNumber, "handle", currentUser.Handle, "area", area.Tag, "msg", msg.MsgNum, "author", msg.From)
	notice("|07Message saved.")
	return true, nil
}
