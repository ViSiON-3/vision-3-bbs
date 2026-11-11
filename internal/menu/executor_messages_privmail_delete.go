package menu

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"sync"

	"github.com/gliderlabs/ssh"

	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// sessionCurrentMail holds, per session, the caller's current mail: the last
// private mail addressed to them that the message reader showed, as
// ViSiON/2's mail menu kept it. DELETEPRIVMAIL offers it first. It lasts the
// call and is dropped when the call ends.
var sessionCurrentMail sync.Map // ssh.Session -> *message.DisplayMessage

// setCurrentMail records m as s's current mail.
func setCurrentMail(s ssh.Session, m *message.DisplayMessage) {
	if s != nil && m != nil {
		sessionCurrentMail.Store(s, m)
	}
}

// clearCurrentMail forgets s's current mail, after it is deleted or when the
// call ends.
func clearCurrentMail(s ssh.Session) {
	if s != nil {
		sessionCurrentMail.Delete(s)
	}
}

// currentMailOf returns s's current mail if it is still in the PRIVMAIL area
// (areaID), unchanged and addressed to handle. It is looked for at the
// number it was read at first; a pack since then (another caller's delete)
// can have moved it to a lower number, so if that number now holds another
// message it is looked for among handle's mail. Gone, it is forgotten.
func currentMailOf(e *MenuExecutor, s ssh.Session, areaID int, handle string) *message.DisplayMessage {
	v, ok := sessionCurrentMail.Load(s)
	if !ok {
		return nil
	}
	shown := v.(*message.DisplayMessage)
	if m, err := e.MessageMgr.GetMessage(areaID, shown.MsgNum); err == nil &&
		!m.IsDeleted && message.SameMessage(m, shown) && m.MailFor(handle) {
		return m
	}
	mail, err := ownPrivateMail(e, areaID, handle)
	if err == nil {
		for _, m := range mail {
			if message.SameMessage(m, shown) {
				setCurrentMail(s, m)
				return m
			}
		}
	}
	clearCurrentMail(s)
	return nil
}

// ownPrivateMail returns the caller's undeleted private mail in the PRIVMAIL
// area, oldest first, by the message numbers LISTPRIVMAIL shows.
func ownPrivateMail(e *MenuExecutor, areaID int, handle string) ([]*message.DisplayMessage, error) {
	var mail []*message.DisplayMessage
	theirs := func(m *message.DisplayMessage) bool { return !m.IsDeleted && m.MailFor(handle) }
	err := e.MessageMgr.EachMessage(areaID, theirs, func(m *message.DisplayMessage) bool {
		mail = append(mail, m)
		return true
	})
	return mail, err
}

// runDeletePrivateMail is DELETEPRIVMAIL, ViSiON/2's "Delete Current Mail":
// it deletes the caller's current mail (the last mail of theirs the reader
// showed this call) after a Y/N, or, with none, asks which of their mail to
// delete by the number LISTPRIVMAIL shows ("?" lists it). Only mail
// addressed to the caller can be deleted, and the message is checked again
// as it is deleted, as the reader's own delete does.
func runDeletePrivateMail(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	s := c.s
	terminal := c.terminal
	currentUser := c.currentUser
	nodeNumber := c.nodeNumber
	outputMode := c.outputMode

	say := func(text string) {
		if err := e.writeString(terminal, outputMode, text); err != nil {
			slog.Debug("DELETEPRIVMAIL write", "node", nodeNumber, "error", err)
		}
	}

	if currentUser == nil {
		say("\r\n|01Error: You must be logged in to delete private mail.|07\r\n")
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

	target := currentMailOf(e, s, area.ID, currentUser.Handle)
	if target == nil {
		mail, err := ownPrivateMail(e, area.ID, currentUser.Handle)
		if err != nil {
			slog.Error("DELETEPRIVMAIL: listing mail", "node", nodeNumber, "error", err)
			say("\r\n|01Error loading private mail.|07\r\n")
			return currentUser, "", nil
		}
		if len(mail) == 0 {
			say("\r\n|07You have no mail.|07\r\n")
			return currentUser, "", nil
		}
		for target == nil {
			say("\r\n|07Mail # to delete (|15?|07/List): |15")
			line, err := readLineFromSessionIHMax(s, terminal, 8)
			say("|07")
			if err != nil {
				if errors.Is(err, io.EOF) {
					return nil, "LOGOFF", io.EOF
				}
				return currentUser, "", nil
			}
			line = strings.TrimSpace(line)
			switch line {
			case "":
				return currentUser, "", nil
			case "?":
				say("\r\n")
				for _, m := range mail {
					say(fmt.Sprintf("|15%5d |07%-24.24s |11%s|07\r\n", m.MsgNum, callerText(m.From), callerText(m.Subject)))
				}
				continue
			}
			n, convErr := strconv.Atoi(line)
			for _, m := range mail {
				if convErr == nil && m.MsgNum == n {
					target = m
					break
				}
			}
			if target == nil {
				say("\r\n|01That isn't one of your mails.|07\r\n")
			}
		}
	}

	say(fmt.Sprintf("\r\n|11%s|07 by |15%s|07\r\n", callerText(target.Subject), callerText(target.From)))
	yes, err := e.PromptYesNo(s, terminal, "|04Delete this mail? @", outputMode, nodeNumber, c.termWidth, c.termHeight, false)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, "LOGOFF", io.EOF
		}
		return currentUser, "", nil
	}
	if !yes {
		return currentUser, "", nil
	}

	// Checked again under the base's lock: since it was shown, another session
	// may have deleted it, or packed the base and moved other mail into its
	// number.
	shown := target
	handle := currentUser.Handle
	delErr := e.MessageMgr.DeleteMessageIf(area.ID, shown.MsgNum, func(m *message.DisplayMessage) bool {
		return message.SameMessage(m, shown) && m.MailFor(handle)
	})
	if errors.Is(delErr, message.ErrMessageChanged) {
		clearCurrentMail(s)
		say("\r\n|01That mail was changed or deleted elsewhere. Nothing was deleted.|07\r\n")
		return currentUser, "", nil
	}
	if delErr != nil {
		slog.Error("DELETEPRIVMAIL: delete", "node", nodeNumber, "msg", shown.MsgNum, "error", delErr)
		say("\r\n|01Error deleting mail.|07\r\n")
		return currentUser, "", nil
	}
	clearCurrentMail(s)
	if packErr := e.MessageMgr.PackAndLinkArea(area.ID); packErr != nil {
		slog.Error("DELETEPRIVMAIL: pack/link after delete", "node", nodeNumber, "area", area.ID, "error", packErr)
	}
	say(fmt.Sprintf("\r\n|11%s|07 by |15%s|07 has been deleted.\r\n", callerText(shown.Subject), callerText(shown.From)))
	return currentUser, "", nil
}
