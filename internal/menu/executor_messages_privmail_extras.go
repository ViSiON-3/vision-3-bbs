package menu

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// The mail menu's extras from ViSiON/2: View Outgoing Mail (SENTPRIVMAIL,
// EV/EO), with unsending a letter the recipient hasn't read (E!), and Copy
// and Forward Mail to a user (COPYPRIVMAIL, FORWARDPRIVMAIL; EC, EW).

// sentMailFilter accepts only private mail handle sent, for the reader and
// list SENTPRIVMAIL opens, so navigation never shows anything else.
func sentMailFilter(handle string, accountID int) msgOwnershipFilter {
	return func(m *message.DisplayMessage) bool { return m.SentBy(handle, accountID) }
}

// unsendableMail reports whether handle may still unsend m from the mailbox
// area: they sent it, and the recipient's lastread there hasn't reached it,
// so they haven't read it. (Mail to oneself is deletable as its recipient
// anyway.) If the recipient's lastread can't be read, it counts as read.
func unsendableMail(e *MenuExecutor, areaID int, m *message.DisplayMessage, handle string, accountID int) bool {
	if !m.SentBy(handle, accountID) {
		return false
	}
	// A group letter (#1036) can be unsent while any member hasn't read it.
	if m.MailGroupID != "" {
		return e.groupLetterUnsendable(areaID, m, handle, accountID)
	}
	lastRead, err := e.MessageMgr.GetLastRead(areaID, strings.TrimSpace(m.To))
	if err != nil {
		slog.Warn("unsend: recipient's lastread", "area", areaID, "to", m.To, "error", err)
		return false
	}
	return lastRead < m.MsgNum
}

// runSentPrivateMail is SENTPRIVMAIL, ViSiON/2's "View Outgoing Mail": the
// mail the caller sent, in the message reader, oldest first. L lists it, and
// D unsends a letter its recipient hasn't read yet. Reading here leaves the
// caller's own lastread in the mailbox alone, so their unread mail stays
// unread.
func runSentPrivateMail(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	currentUser := c.currentUser
	say := func(text string) {
		if err := e.writeString(c.terminal, c.outputMode, text); err != nil {
			slog.Debug("SENTPRIVMAIL write", "node", c.nodeNumber, "error", err)
		}
	}
	if currentUser == nil {
		say("\r\n|01Error: You must be logged in to view sent mail.|07\r\n")
		return nil, "", nil
	}
	if e.MessageMgr == nil {
		return currentUser, "", nil
	}
	area, ok := e.MessageMgr.GetAreaByTag("PRIVMAIL")
	if !ok {
		say("\r\n|01Error: Private mail area not configured.|07\r\n")
		return currentUser, "", nil
	}

	first, total := 0, 0
	sent := sentMailFilter(currentUser.Handle, currentUser.ID)
	err := e.MessageMgr.EachMessage(area.ID, func(m *message.DisplayMessage) bool { return !m.IsDeleted && sent(m) },
		func(m *message.DisplayMessage) bool {
			// The first sent letter is all the reader needs to start; the
			// walk stops there instead of reading every later letter (#1032).
			first = m.MsgNum
			return false
		})
	if err == nil {
		total, err = e.MessageMgr.GetMessageCountForArea(area.ID)
	}
	if err != nil {
		slog.Error("SENTPRIVMAIL: reading the mailbox", "node", c.nodeNumber, "error", err)
		say("\r\n|01Error loading private mail.|07\r\n")
		return currentUser, "", nil
	}
	if first == 0 {
		say("\r\n|07You haven't sent any mail.|07\r\n")
		uiPause(1 * time.Second)
		return currentUser, "", nil
	}

	// A group letter (#1036) is listed once, by its first copy, not once per
	// member; the first copy is also the first message found above.
	groupLetters := newSentLetters(e, area.ID, currentUser.Handle, currentUser.ID)
	letters := func(m *message.DisplayMessage) bool { return sent(m) && groupLetters.shows(m) }

	origID, origTag := currentUser.CurrentMessageAreaID, currentUser.CurrentMessageAreaTag
	currentUser.CurrentMessageAreaID, currentUser.CurrentMessageAreaTag = area.ID, area.Tag
	updated, next, _, err := runMessageReaderOpts(e, c.s, c.terminal, c.userManager, currentUser, c.nodeNumber,
		c.sessionStartTime, c.outputMode, first, total, false, c.termWidth, c.termHeight, letters, readerOpts{keepLastRead: true})
	currentUser.CurrentMessageAreaID, currentUser.CurrentMessageAreaTag = origID, origTag
	if updated != nil {
		updated.CurrentMessageAreaID, updated.CurrentMessageAreaTag = origID, origTag
	}
	if err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, editor.ErrIdleTimeout) {
			return nil, "LOGOFF", err
		}
		slog.Error("SENTPRIVMAIL: reader", "node", c.nodeNumber, "error", err)
	}
	if next == "LOGOFF" {
		return nil, "LOGOFF", io.EOF
	}
	return currentUser, "", nil
}

// forwardedSubject is the subject of mail copied or forwarded from one with
// subject: "Fwd: " ahead of it, once.
func forwardedSubject(subject string) string {
	if len(subject) >= 4 && strings.EqualFold(subject[:4], "fwd:") {
		return subject
	}
	return "Fwd: " + subject
}

// forwardedBody is the text of mail copied or forwarded from m: the
// caller's note, if any, then a header naming m's sender, recipient, date
// (in the board's zone) and subject, then m's text as it was.
func forwardedBody(note string, m *message.DisplayMessage, loc *time.Location) string {
	var b strings.Builder
	if note = strings.TrimRight(note, "\n "); strings.TrimSpace(note) != "" {
		b.WriteString(note + "\n\n")
	}
	fmt.Fprintf(&b, "--- Forwarded mail ---\nFrom: %s\nTo: %s\nDate: %s\nSubject: %s\n\n",
		m.From, mailTo(m), strings.ToLower(m.DateTime.In(loc).Format("01/02/06 at 3:04 PM")), m.Subject)
	b.WriteString(m.Body)
	return b.String()
}

// runCopyPrivateMail is COPYPRIVMAIL, ViSiON/2's "Copy Mail to User".
func runCopyPrivateMail(c *cmdCtx, args string) (*user.User, string, error) {
	return sendOnPrivateMail(c, false)
}

// runForwardPrivateMail is FORWARDPRIVMAIL, ViSiON/2's "Forward Mail": a
// copy, after which the caller's own is deleted.
func runForwardPrivateMail(c *cmdCtx, args string) (*user.User, string, error) {
	return sendOnPrivateMail(c, true)
}

// sendOnPrivateMail copies the caller's current mail (or the mail they name,
// as DELETEPRIVMAIL asks) to another user, as private mail from the caller
// titled "Fwd: subject": an optional note of theirs, written in the editor,
// then the original under a header saying who sent it to whom and when. The
// original's text can't be changed on the way. forward then deletes the
// caller's own copy, checked again under the base's lock as a delete is.
func sendOnPrivateMail(c *cmdCtx, forward bool) (*user.User, string, error) {
	e := c.e
	s := c.s
	currentUser := c.currentUser
	nodeNumber := c.nodeNumber
	cmd, verb, done := "COPYPRIVMAIL", "copy", "copied"
	if forward {
		cmd, verb, done = "FORWARDPRIVMAIL", "forward", "forwarded"
	}
	say := func(text string) {
		if err := e.writeString(c.terminal, c.outputMode, text); err != nil {
			slog.Debug(cmd+" write", "node", nodeNumber, "error", err)
		}
	}
	hungUp := func(err error) (*user.User, string, error) {
		if errors.Is(err, io.EOF) || errors.Is(err, editor.ErrIdleTimeout) {
			return nil, "LOGOFF", err
		}
		return currentUser, "", nil
	}

	if currentUser == nil {
		say("\r\n|01Error: You must be logged in to " + verb + " mail.|07\r\n")
		return nil, "", nil
	}
	if e.MessageMgr == nil {
		return currentUser, "", nil
	}
	area, ok := e.MessageMgr.GetAreaByTag("PRIVMAIL")
	if !ok {
		say("\r\n|01Error: Private mail area not configured.|07\r\n")
		return currentUser, "", nil
	}

	target, err := chooseOwnMail(c, area.ID, verb)
	if err != nil {
		return hungUp(err)
	}
	if target == nil {
		return currentUser, "", nil
	}
	say(fmt.Sprintf("\r\n|11%s|07 by |15%s|07\r\n", callerText(target.Subject), callerText(target.From)))

	say(fmt.Sprintf("|07%s it to (handle): |15", strings.ToUpper(verb[:1])+verb[1:]))
	name, err := readLineFromSessionIHMax(s, c.terminal, 30)
	say("|07")
	if err != nil {
		return hungUp(err)
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return currentUser, "", nil
	}
	// "@name" sends it on to a user group (#1036), one copy per member.
	group, toGroup := mailGroupFor(c.userManager, name)
	if toGroup {
		if refusal := e.checkGroupMail(c.userManager, currentUser, group, s, c.sessionStartTime); refusal != "" {
			say("\r\n" + refusal + "\r\n")
			uiPause(1 * time.Second)
			return currentUser, "", nil
		}
	}
	recipient, found := c.userManager.GetUser(name)
	if toGroup {
		recipient, found = &user.User{Handle: "@" + group.Name}, true
	}
	if !found || recipient == nil || recipient.DeletedUser {
		say(fmt.Sprintf("\r\n|01Error: User '%s' not found.|07\r\n", callerText(name)))
		uiPause(1 * time.Second)
		return currentUser, "", nil
	}

	note := ""
	addNote, err := e.PromptYesNo(s, c.terminal, "|07Add a note of your own? @", c.outputMode, nodeNumber, c.termWidth, c.termHeight, false)
	if err != nil {
		return hungUp(err)
	}
	subject := forwardedSubject(target.Subject)
	if addNote {
		ctx := editor.EditorContext{NodeNumber: nodeNumber, ConfArea: "Private Mail"}
		text, saved, edErr := editor.RunEditorWithMetadata("", s, sessionOutput(s), c.outputMode, subject, recipient.Handle, currentUser.Handle,
			false, "", "", "", "", false, nil, getSessionIH(s), withAutocorrect(e, ctx, area, currentUser, c.userManager))
		if edErr != nil {
			if errors.Is(edErr, editor.ErrIdleTimeout) || errors.Is(edErr, io.EOF) {
				return nil, "LOGOFF", edErr
			}
			slog.Error(cmd+": editor", "node", nodeNumber, "error", edErr)
			say(e.Strings().MsgEditorError)
			return currentUser, "", nil
		}
		if !saved {
			say("\r\n|07Nothing was " + done + ".|07\r\n")
			uiPause(1 * time.Second)
			return currentUser, "", nil
		}
		note = text
	}

	body := forwardedBody(note, target, config.LoadTimezone(e.GetServerConfig().Timezone))
	if toGroup {
		sent, gerr := e.sendGroupMail(c.userManager, currentUser, group, area.ID, currentUser.Handle, subject, body, s, c.sessionStartTime)
		if gerr != nil {
			msg := groupMailRefusal(group, gerr)
			if sent > 0 {
				msg = fmt.Sprintf("|01Error: the mail reached only part of @%s, so yours was kept.|07", group.Name)
			}
			say("\r\n" + msg + "\r\n")
			uiPause(2 * time.Second)
			return currentUser, "", nil
		}
	} else if _, err := e.MessageMgr.Post(area.ID, currentUser.Handle, recipient.Handle, subject, body, message.PostOptions{
		Private: true, SenderID: currentUser.ID, RecipientID: recipient.ID,
	}); err != nil {
		slog.Error(cmd+": saving", "node", nodeNumber, "to", recipient.Handle, "error", err)
		say("\r\n|01Error saving private message!|07\r\n")
		uiPause(2 * time.Second)
		return currentUser, "", nil
	}
	currentUser.MessagesPosted++
	if err := c.userManager.UpdateUser(currentUser); err != nil {
		slog.Error("failed to update MessagesPosted", "node", nodeNumber, "handle", currentUser.Handle, "error", err)
	}
	slog.Info("private mail "+done, "node", nodeNumber, "handle", currentUser.Handle, "to", recipient.Handle, "msg", target.MsgNum)

	if forward {
		shown := target
		handle := currentUser.Handle
		delErr := e.MessageMgr.DeleteMessageIf(area.ID, shown.MsgNum, func(m *message.DisplayMessage) bool {
			return message.SameMessage(m, shown) && m.MailFor(handle, currentUser.ID)
		})
		clearCurrentMail(s)
		switch {
		case errors.Is(delErr, message.ErrMessageChanged):
			say(fmt.Sprintf("\r\n|07Mail forwarded to |15%s|07, but yours was changed elsewhere, so it was kept.\r\n", callerText(recipient.Handle)))
			uiPause(1 * time.Second)
			return currentUser, "", nil
		case delErr != nil:
			slog.Error(cmd+": deleting the original", "node", nodeNumber, "msg", shown.MsgNum, "error", delErr)
			say(fmt.Sprintf("\r\n|07Mail forwarded to |15%s|07, but yours couldn't be deleted.\r\n", callerText(recipient.Handle)))
			uiPause(1 * time.Second)
			return currentUser, "", nil
		}
		if packErr := e.MessageMgr.PackAndLinkArea(area.ID); packErr != nil {
			slog.Error(cmd+": pack/link after delete", "node", nodeNumber, "area", area.ID, "error", packErr)
		}
	}
	say(fmt.Sprintf("\r\n|07Mail %s to |15%s|07.\r\n", done, callerText(recipient.Handle)))
	uiPause(1 * time.Second)
	return currentUser, "", nil
}
