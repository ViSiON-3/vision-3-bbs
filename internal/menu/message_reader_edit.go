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

// asciiText reports whether s is all ASCII. The editor counts columns and
// deletes in bytes, so it would split a multibyte character.
func asciiText(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

// widestLine is the length of text's longest line, in bytes: characters,
// for ASCII text.
func widestLine(text string) int {
	w := 0
	for line := range strings.SplitSeq(text, "\n") {
		w = max(w, len(strings.TrimRight(line, "\r")))
	}
	return w
}

// editResult is how an edit from the reader ended.
type editResult int

const (
	editNone  editResult = iota // nothing saved; the message is as shown
	editSaved                   // saved
	editStale                   // the message changed elsewhere, or the save may have half gone through: read it again
)

// minEditSubject is the shortest the subject prompt allows: what a new post
// is given. A longer subject, such as a reply's "Re: ...", gets its length.
const minEditSubject = 30

// editPostedMessage edits msg, which the reader is showing in area, for
// currentUser: the subject at a prompt that starts with the current one,
// then the text in the full-screen editor. may reports whether the caller
// may edit a message, as a moderator of the area or not; moderator tells
// whether they are one now. Both are asked again as the message is saved,
// under the base's lock, along with whether it is still the message shown.
// It returns io.EOF when the caller hung up.
func (e *MenuExecutor) editPostedMessage(s ssh.Session, ih *editor.InputHandler, terminal *term.Terminal,
	userManager *user.UserMgr, currentUser *user.User, nodeNumber int, outputMode ansi.OutputMode,
	area *message.MessageArea, msg *message.DisplayMessage, confName string,
	may func(m *message.DisplayMessage, moderator bool) bool, moderator func() bool,
	termWidth, termHeight int) (editResult, error) {

	notice := func(text string) {
		e.showReaderNotice(terminal, outputMode, text, termHeight)
	}

	// The editor holds editor.MaxLines lines and drops the rest of a longer
	// text, which can come in by QWK or v3mail. Saving would lose it.
	text := withoutEditedLine(msg.Body)
	if n := strings.Count(strings.TrimRight(text, "\n"), "\n") + 1; n > editor.MaxLines {
		notice(fmt.Sprintf("|01This message is %d lines long, more than the editor holds (%d). It can't be edited here.|07", n, editor.MaxLines))
		return editNone, nil
	}
	// It also works in bytes, so it would split a character such as an
	// accented letter that came in by QWK or v3mail, or in a quote.
	if !asciiText(text) {
		notice("|01This message has characters other than plain ASCII, which the editor can't handle. It can't be edited here.|07")
		return editNone, nil
	}
	// And it loads lines as they are, without wrapping, but shows each as one
	// screen row, so a wider line, as from v3mail, couldn't be seen whole.
	if w := widestLine(text); w > editor.MaxLineLength {
		notice(fmt.Sprintf("|01This message has a line %d characters wide, more than the editor shows (%d). It can't be edited here.|07", w, editor.MaxLineLength))
		return editNone, nil
	}

	limit := max(minEditSubject, utf8.RuneCountInString(msg.Subject))
	val, aborted, err := e.promptComposeField(s, terminal, "\r\n|07Subject: |15", limit, msg.Subject, "subject", outputMode, nodeNumber, termWidth, termHeight)
	if err != nil {
		return editNone, err
	}
	if aborted {
		return editNone, nil
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
	body, saved, err := editor.RunEditorWithMetadata(text, s, sessionOutput(s), outputMode,
		subject, msg.To, msg.From, false, "", "", "", "", false, nil, ih,
		withAutocorrect(e, ctx, area, currentUser, userManager))
	if err != nil {
		if errors.Is(err, editor.ErrIdleTimeout) {
			return editNone, err
		}
		slog.Error("editor failed", "node", nodeNumber, "error", err)
		e.writeString(terminal, outputMode, e.Strings().MsgEditorError)
		uiPause(2 * time.Second)
		return editNone, nil
	}
	if !saved {
		notice("|07Edit cancelled. The message is as it was.")
		return editNone, nil
	}
	// In the board's time zone, as the editor's clock shows it.
	body = withEditedLine(body, currentUser.Handle, config.NowIn(e.GetServerConfig().Timezone))

	// The right to edit is checked again now: the sysop may have changed the
	// area's sponsor or the caller's roles while they typed. Then, under the
	// base's lock, the message must still be the one shown, and still theirs
	// to edit, with moderation read again there too.
	if !may(msg, moderator()) {
		notice("|01You can no longer edit this message. Nothing was saved.|07")
		return editNone, nil
	}
	err = e.MessageMgr.EditMessageIf(area.ID, msg.MsgNum, func(m *message.DisplayMessage) (string, string, bool) {
		if !message.SameMessage(m, msg) || !may(m, moderator()) {
			return "", "", false
		}
		return subject, body, true
	})
	if errors.Is(err, message.ErrMessageChanged) {
		notice("|01That message was changed or deleted elsewhere. Nothing was saved.|07")
		return editStale, nil
	}
	if errors.Is(err, message.ErrEditUncertain) {
		slog.Error("edit message", "node", nodeNumber, "area", area.ID, "msg", msg.MsgNum, "error", err)
		notice("|01Error saving the message. It may or may not have been saved.|07")
		return editStale, nil
	}
	if err != nil {
		slog.Error("edit message", "node", nodeNumber, "area", area.ID, "msg", msg.MsgNum, "error", err)
		notice("|01Error saving the message.|07")
		return editNone, nil
	}
	slog.Info("message edited", "node", nodeNumber, "handle", currentUser.Handle, "area", area.Tag, "msg", msg.MsgNum, "author", msg.From)
	notice("|07Message saved.")
	return editSaved, nil
}
