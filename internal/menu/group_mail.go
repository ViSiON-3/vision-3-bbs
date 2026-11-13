package menu

import (
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/perm"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"github.com/gliderlabs/ssh"
	"github.com/google/uuid"
	"golang.org/x/term"
)

// Group mail (#1036). Private mail addressed to "@name", a user group in
// configs/roles.json, is expanded when it is sent: each member gets their own
// copy in PRIVMAIL, addressed to their handle, so every private-mail rule
// (who can read it, read status, delete, unsend) applies to it unchanged.
// Each copy carries the group's name and an id all copies of the letter share
// (message.PostOptions.MailGroup), so headers show "@name" rather than the
// member's handle, the sender's sent mail shows the letter once, and
// unsending reaches every unread copy. The member list is never shown.

// maxGroupMailRecipients bounds how many copies one letter may make.
const maxGroupMailRecipients = 500

// Why group mail can't be sent.
var (
	errGroupMailNoRecipients = errors.New("nobody in that group can receive mail")
	errGroupMailTooMany      = fmt.Errorf("that group has more than %d members, the most one letter can go to", maxGroupMailRecipients)
	errGroupMailNotAllowed   = errors.New("you may not send mail to that group")
)

// mailGroupFor reports whether to addresses a user group: it starts with '@'
// and names a group configs/roles.json defines. A recipient that is exactly
// an existing handle is that user first, so an account made before handles
// were refused a leading '@' keeps its mail.
func mailGroupFor(um *user.UserMgr, to string) (perm.Group, bool) {
	to = strings.TrimSpace(to)
	if !strings.HasPrefix(to, "@") {
		return perm.Group{}, false
	}
	if um != nil && um.HandleExists(to) {
		return perm.Group{}, false
	}
	return perm.Current().Group(strings.TrimPrefix(to, "@"))
}

// mayMailGroup reports whether sender may send mail to g: g's mail_acs when
// it has one, otherwise its members and co-sysops and up. It reads the
// sender's stored record, so a membership or level just taken away counts.
func (e *MenuExecutor) mayMailGroup(um *user.UserMgr, sender *user.User, g perm.Group, s ssh.Session, start time.Time) bool {
	fresh := storedActor(um, sender)
	if fresh == nil {
		return false
	}
	if g.MailACS != "" {
		return checkACS(g.MailACS, fresh, s, nil, start)
	}
	return perm.InGroup(fresh, g.Name, nil) || e.isCoSysOpOrAbove(fresh)
}

// groupMailMembers lists who a letter to g is delivered to, by ID: every
// member but the sender, leaving out deleted and banned accounts.
func groupMailMembers(um *user.UserMgr, g perm.Group, sender *user.User) ([]*user.User, error) {
	if um == nil {
		return nil, errGroupMailNoRecipients
	}
	var out []*user.User
	for _, u := range um.GetAllUsers() {
		if u == nil || u.DeletedUser || (u.AccessLevel == 0 && !u.Validated) {
			continue
		}
		if sender != nil && u.ID == sender.ID {
			continue
		}
		if perm.InGroup(u, g.Name, nil) {
			out = append(out, u)
		}
	}
	if len(out) == 0 {
		return nil, errGroupMailNoRecipients
	}
	if len(out) > maxGroupMailRecipients {
		return nil, errGroupMailTooMany
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// checkGroupMail is the check made before the editor opens, so no typing is
// lost: sender may mail g and someone would receive it. It returns the
// message to show when they can't, "" when they can.
func (e *MenuExecutor) checkGroupMail(um *user.UserMgr, sender *user.User, g perm.Group, s ssh.Session, start time.Time) string {
	if !e.mayMailGroup(um, sender, g, s, start) {
		return groupMailRefusal(g, errGroupMailNotAllowed)
	}
	if _, err := groupMailMembers(um, g, sender); err != nil {
		return groupMailRefusal(g, err)
	}
	return ""
}

// groupMailRefusal is the caller's message for err, sending to g.
func groupMailRefusal(g perm.Group, err error) string {
	switch {
	case errors.Is(err, errGroupMailNotAllowed):
		return fmt.Sprintf("|01You may not send mail to @%s.|07", g.Name)
	case errors.Is(err, errGroupMailNoRecipients):
		return fmt.Sprintf("|01Nobody in @%s can receive mail.|07", g.Name)
	case errors.Is(err, errGroupMailTooMany):
		return fmt.Sprintf("|01@%s has more than %d members, the most one letter can go to.|07", g.Name, maxGroupMailRecipients)
	}
	return "|01Error sending mail.|07"
}

// sendGroupMail writes one copy of a letter to each member of g in area
// areaID, checking again that sender may (the editor was open meanwhile).
// sent is how many copies were written; err reports why it stopped short,
// with sent > 0 when some members already have their copy.
func (e *MenuExecutor) sendGroupMail(um *user.UserMgr, sender *user.User, g perm.Group, areaID int, fromName, subject, body string, s ssh.Session, start time.Time) (sent int, err error) {
	if !e.mayMailGroup(um, sender, g, s, start) {
		return 0, errGroupMailNotAllowed
	}
	members, err := groupMailMembers(um, g, sender)
	if err != nil {
		return 0, err
	}
	id := strings.ReplaceAll(uuid.NewString(), "-", "")
	for _, m := range members {
		if _, perr := e.MessageMgr.Post(areaID, fromName, m.Handle, subject, body, message.PostOptions{
			Private:      true,
			SenderHandle: sender.Handle,
			MailGroup:    g.Name,
			MailGroupID:  id,
		}); perr != nil {
			slog.Error("group mail: writing a member's copy", "group", g.Name, "id", id, "to", m.Handle, "sent", sent, "error", perr)
			return sent, perr
		}
		sent++
	}
	slog.Info("group mail sent", "handle", sender.Handle, "group", g.Name, "id", id, "copies", sent)
	return sent, nil
}

// mailTo is how a header shows m's recipient: "@group" for a member's copy
// of group mail, so no copy names the member it went to, else m.To.
func mailTo(m *message.DisplayMessage) string {
	if a := m.GroupAddress(); a != "" {
		return a
	}
	return m.To
}

// firstGroupCopy reports whether m is the copy of its group letter the
// sender's sent mail shows: every copy but the first is left out, so the
// letter is listed once. The copies are written one after another, so a copy
// is the first unless the message just before it is a copy of the same
// letter. Mail that isn't a group copy is always shown.
func (e *MenuExecutor) firstGroupCopy(m *message.DisplayMessage) bool {
	if m.MailGroupID == "" || m.MsgNum <= 1 || e.MessageMgr == nil {
		return true
	}
	prev, err := e.MessageMgr.GetMessage(m.AreaID, m.MsgNum-1)
	if err != nil || prev == nil || prev.IsDeleted {
		return true
	}
	return prev.MailGroupID != m.MailGroupID
}

// groupLetterCopies lists the copies of the group letter id that handle
// sent, still in area areaID, by number.
func (e *MenuExecutor) groupLetterCopies(areaID int, id, handle string) []*message.DisplayMessage {
	var out []*message.DisplayMessage
	err := e.MessageMgr.EachMessage(areaID,
		func(m *message.DisplayMessage) bool { return !m.IsDeleted && m.MailGroupID == id && m.SentBy(handle) },
		func(m *message.DisplayMessage) bool { out = append(out, m); return true })
	if err != nil {
		slog.Warn("group mail: listing a letter's copies", "area", areaID, "id", id, "error", err)
	}
	return out
}

// unreadCopy reports whether m's recipient hasn't read it: their lastread in
// the area hasn't reached it. A lastread that can't be read counts as read.
func (e *MenuExecutor) unreadCopy(areaID int, m *message.DisplayMessage) bool {
	lastRead, err := e.MessageMgr.GetLastRead(areaID, strings.TrimSpace(m.To))
	if err != nil {
		slog.Warn("unsend: recipient's lastread", "area", areaID, "to", m.To, "error", err)
		return false
	}
	return lastRead < m.MsgNum
}

// groupLetterUnsendable reports whether any copy of m's group letter is
// still unread, so its sender may unsend it.
func (e *MenuExecutor) groupLetterUnsendable(areaID int, m *message.DisplayMessage, handle string) bool {
	for _, c := range e.groupLetterCopies(areaID, m.MailGroupID, handle) {
		if e.unreadCopy(areaID, c) {
			return true
		}
	}
	return false
}

// unsendGroupLetter deletes every copy of m's group letter whose recipient
// hasn't read it, each checked again as it is deleted, and packs the base
// once. It reports how many copies went and how many had been read and stay.
func (e *MenuExecutor) unsendGroupLetter(areaID int, m *message.DisplayMessage, handle string) (removed, kept int, err error) {
	for _, c := range e.groupLetterCopies(areaID, m.MailGroupID, handle) {
		if !e.unreadCopy(areaID, c) {
			kept++
			continue
		}
		copyShown := c
		derr := e.MessageMgr.DeleteMessageIf(areaID, c.MsgNum, func(cur *message.DisplayMessage) bool {
			return message.SameMessage(cur, copyShown) && e.unreadCopy(areaID, cur)
		})
		switch {
		case errors.Is(derr, message.ErrMessageChanged):
			kept++
		case derr != nil:
			err = derr
		default:
			removed++
		}
	}
	if removed > 0 {
		if perr := e.MessageMgr.PackAndLinkArea(areaID); perr != nil {
			slog.Error("pack/link area after unsend", "area", areaID, "error", perr)
		}
	}
	slog.Info("group mail unsent", "handle", handle, "group", m.MailGroup, "id", m.MailGroupID, "removed", removed, "kept", kept)
	return removed, kept, err
}

// reportGroupMail tells the sender how sending to g went, and counts the
// letter as one post when any copy was written.
func (e *MenuExecutor) reportGroupMail(terminal *term.Terminal, outputMode ansi.OutputMode, um *user.UserMgr, sender *user.User, g perm.Group, sent int, err error) {
	msg := fmt.Sprintf("\r\n|02Private message sent to @%s!|07\r\n", g.Name)
	switch {
	case err != nil && sent > 0:
		msg = fmt.Sprintf("\r\n|01Error: the letter reached only part of @%s.|07\r\n", g.Name)
	case err != nil:
		msg = "\r\n" + groupMailRefusal(g, err) + "\r\n"
	}
	if sent > 0 && um != nil {
		sender.MessagesPosted++
		if uerr := um.UpdateUser(sender); uerr != nil {
			slog.Error("failed to update MessagesPosted", "handle", sender.Handle, "error", uerr)
		}
	}
	terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
	uiPause(1 * time.Second)
}
