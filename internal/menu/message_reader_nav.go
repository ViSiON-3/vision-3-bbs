package menu

import (
	"bufio"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"github.com/gliderlabs/ssh"
	"golang.org/x/term"
)

// replyAddressee returns who a reply to msg goes to: name for display in the
// editor header, and to for the message's To field.
//
// Netmail is delivered by FTN address, not by name. AddReply splits
// "user@zone:net/node" into the To and destination-address fields, and a To
// with no address leaves the JAM message without a DADDRESS subfield, so the
// tosser has nothing to address the outbound packet with — the reply is
// written but can never be delivered. Every other area type replies to the
// bare name.
//
// The addressee is normally the parent's author, at the address the message
// came from. Netmail the user sent themselves is the exception: its origin is
// this system, so the reply follows the parent to its addressee instead of
// looping back here.
func replyAddressee(area *message.MessageArea, msg *message.DisplayMessage) (name, to string) {
	if !isNetmailArea(area) {
		return msg.From, msg.From
	}

	name, addr := msg.From, msg.OrigAddr
	if msg.DestAddr != "" && area.OriginAddr != "" && addr == area.OriginAddr {
		name, addr = msg.To, msg.DestAddr
	}
	if addr == "" {
		return name, name // unaddressable, but the reply is still worth keeping
	}
	return name, fmt.Sprintf("%s@%s", name, addr)
}

// privateReplyHandle returns the handle a private reply to msg is addressed
// to, for mail outside netmail. Only the handle in To or From can read a
// private message (message.DisplayMessage.VisibleTo), so a reply addressed to
// anything else would be written for no one.
//
// The addressee is msg's sender, or its recipient when replier sent msg
// themselves, so a follow-up goes to the other party rather than back to the
// replier. The name is resolved with user.UserMgr.ResolveRecipient: a handle
// stands for itself, and mail signed with a real name before private mail was
// signed by handle resolves to the one account with that real name.
//
// ok is false when the name identifies no current account: it is unknown,
// shared by two accounts, anonymous, or belongs to a deleted account. reason
// then says why, for the caller to show.
func privateReplyHandle(um *user.UserMgr, msg *message.DisplayMessage, replier, anonymousName string) (handle, reason string, ok bool) {
	name := strings.TrimSpace(msg.From)
	if replier != "" && strings.EqualFold(name, strings.TrimSpace(replier)) {
		name = strings.TrimSpace(msg.To)
	}
	if name == "" {
		return "", "The sender of this message is anonymous, so it cannot be answered privately.", false
	}
	// An account's handle wins, even one spelled like the anonymous name:
	// private mail is signed by handle, so that is who sent it.
	if um != nil && um.HandleExists(name) {
		if u, found := um.ResolveRecipient(name); found {
			return u.Handle, "", true
		}
		return "", fmt.Sprintf("%s no longer has an account here, so the reply could not be sent.", name), false
	}
	// Otherwise a name matching the anonymous signature is an anonymous post:
	// COMPOSEMSG signed those with the configured name, or "Anonymous".
	anon := strings.TrimSpace(anonymousName)
	if strings.EqualFold(name, "Anonymous") || (anon != "" && strings.EqualFold(name, anon)) {
		return "", "The sender of this message is anonymous, so it cannot be answered privately.", false
	}
	if um != nil {
		if u, found := um.ResolveRecipient(name); found {
			return u.Handle, "", true
		}
	}
	return "", fmt.Sprintf("Can't tell which user '%s' is, so the reply could not be sent.", name), false
}

// handleReply manages the reply flow matching Pascal's reply handling.
//
// The reader goes on showing the message that was replied to, so a reply
// leaves the current message number alone and the next N moves on from the
// message replied to. The reply is appended to the area, so totalMsgCount
// grows by one.
func handleReply(e *MenuExecutor, s ssh.Session, ih *editor.InputHandler, terminal *term.Terminal,
	userManager *user.UserMgr, currentUser *user.User, nodeNumber int,
	outputMode ansi.OutputMode, currentMsg *message.DisplayMessage,
	currentAreaID int, totalMsgCount *int, confName, areaName string) string {

	// Prepare quote data for /Q command
	// Split message body into lines for quoting
	quoteLines := strings.Split(currentMsg.Body, "\n")

	// Format date/time from message
	quoteDate := currentMsg.DateTime.Format("01/02/2006")
	quoteTime := currentMsg.DateTime.Format("3:04 PM")

	// Auto-generate subject with "RE: " prefix (no prompt needed)
	newSubject := generateReplySubject(currentMsg.Subject)
	if strings.TrimSpace(newSubject) == "" {
		terminalio.WriteProcessedBytes(terminal, []byte(e.Strings().MsgReplySubjectEmpty), outputMode)
		time.Sleep(1 * time.Second)
		return ""
	}

	// Work out who the reply is addressed to before opening the editor, so the
	// header shows the reply's own addressee rather than the parent's.
	replyArea, _ := e.MessageMgr.GetAreaByID(currentAreaID)
	replyName, replyTo := replyAddressee(replyArea, currentMsg)

	// A private reply outside netmail is addressed by handle, the only name
	// private mail is delivered to (and signed by one: the reply's From is
	// always the replier's handle). A reply no account could read is refused
	// before the editor opens, so no typing is lost.
	if currentMsg.IsPrivate && !isNetmailArea(replyArea) {
		handle, reason, ok := privateReplyHandle(userManager, currentMsg, currentUser.Handle, e.Strings().AnonymousName)
		if !ok {
			slog.Info("private reply refused: addressee not identified", "node", nodeNumber,
				"handle", currentUser.Handle, "area", currentAreaID, "msg", currentMsg.MsgNum, "from", currentMsg.From)
			terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n|01"+reason+"|07\r\n")), outputMode)
			time.Sleep(1 * time.Second)
			return ""
		}
		replyName, replyTo = handle, handle
	}

	terminalio.WriteProcessedBytes(terminal, []byte(e.Strings().MsgLaunchingEditor), outputMode)

	// Start with empty editor - user will use /Q command to quote if desired
	// Pass message metadata for quoting (from, title, date, time, isAnon, lines)
	replyNextMsg := *totalMsgCount + 1
	replyCtx := editor.EditorContext{
		NodeNumber: nodeNumber,
		NextMsgNum: replyNextMsg,
		ConfArea:   fmt.Sprintf("%s > %s", confName, areaName),
	}
	replyBody, saved, editErr := editor.RunEditorWithMetadata("", s, sessionOutput(s), outputMode, newSubject, replyName, currentUser.Handle, false,
		currentMsg.From, currentMsg.Subject, quoteDate, quoteTime, false, quoteLines, ih, replyCtx)
	if editErr != nil {
		slog.Error("editor failed", "node", nodeNumber, "error", editErr)
		terminalio.WriteProcessedBytes(terminal, []byte(e.Strings().MsgEditorError), outputMode)
		time.Sleep(2 * time.Second)
		return ""
	}

	if !saved {
		terminalio.WriteProcessedBytes(terminal, []byte(e.Strings().MsgReplyCancelled), outputMode)
		time.Sleep(1 * time.Second)
		return ""
	}

	// Append auto-signature if user has one
	if currentUser.AutoSignature != "" {
		replyBody = replyBody + "\n\n" + currentUser.AutoSignature
	}

	// Save reply, recording the parent message number for threading. A reply to
	// a private message stays private so it remains visible to its recipient.
	replyMsgID := currentMsg.MsgID
	var err error
	if currentMsg.IsPrivate {
		_, err = e.MessageMgr.AddPrivateReply(currentAreaID, currentUser.Handle, replyTo,
			newSubject, replyBody, replyMsgID, currentMsg.MsgNum)
	} else {
		_, err = e.MessageMgr.AddReply(currentAreaID, currentUser.Handle, replyTo,
			newSubject, replyBody, replyMsgID, currentMsg.MsgNum)
	}
	if err != nil {
		slog.Error("failed to save reply", "node", nodeNumber, "error", err)
		terminalio.WriteProcessedBytes(terminal, []byte(e.Strings().MsgReplyError), outputMode)
		time.Sleep(2 * time.Second)
	} else {
		currentUser.MessagesPosted++
		if err := userManager.UpdateUser(currentUser); err != nil {
			slog.Error("failed to update MessagesPosted", "node", nodeNumber, "handle", currentUser.Handle, "error", err)
		}
		terminalio.WriteProcessedBytes(terminal, []byte(e.Strings().MsgReplySuccess), outputMode)
		time.Sleep(1 * time.Second)
		*totalMsgCount++
	}
	return ""
}

// handleThread prompts for forward/backward and searches for matching subject.
func handleThread(reader *bufio.Reader, e *MenuExecutor, terminal *term.Terminal,
	outputMode ansi.OutputMode, areaID int,
	currentMsgNum *int, totalMsgs int, subject string) {

	terminalio.WriteProcessedBytes(terminal, []byte("\r\n"), outputMode)
	terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.Strings().MsgThreadPrompt)), outputMode)

	key, err := readSingleKey(reader)
	if err != nil {
		return
	}

	forward := unicode.ToUpper(key) != 'B'

	newMsg, found := forwardBackThread(e, areaID, *currentMsgNum, totalMsgs, subject, forward)
	if found {
		*currentMsgNum = newMsg
	} else {
		dir := "forward"
		if !forward {
			dir = "backward"
		}
		msg := fmt.Sprintf(e.Strings().MsgNoThreadFound, dir)
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		time.Sleep(1 * time.Second)
	}
}

// forwardBackThread searches for messages with matching subjects, like Pascal's forwardbackthread.
func forwardBackThread(e *MenuExecutor, areaID int, currentMsg int,
	totalMsgs int, subject string, forward bool) (int, bool) {

	// Strip " -Re: #N-" suffix and "Re: " prefix for matching
	searchSubject := message.NormalizeThreadSubject(subject)

	if forward {
		for i := currentMsg + 1; i <= totalMsgs; i++ {
			msg, err := e.MessageMgr.GetMessage(areaID, i)
			if err != nil || msg.IsDeleted {
				continue
			}
			if message.SubjectsMatchThread(msg.Subject, searchSubject) {
				return i, true
			}
		}
	} else {
		for i := currentMsg - 1; i >= 1; i-- {
			msg, err := e.MessageMgr.GetMessage(areaID, i)
			if err != nil || msg.IsDeleted {
				continue
			}
			if message.SubjectsMatchThread(msg.Subject, searchSubject) {
				return i, true
			}
		}
	}
	return currentMsg, false
}

// handleJump prompts the user for a message number to jump to.
func handleJump(reader *bufio.Reader, terminal *term.Terminal, outputMode ansi.OutputMode,
	currentMsgNum *int, totalMsgs int, jumpPromptFmt string, invalidMsgStr string) {

	prompt := fmt.Sprintf(jumpPromptFmt, totalMsgs)
	terminalio.WriteProcessedBytes(terminal, []byte("\r\n"), outputMode)
	terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(prompt)), outputMode)

	input, err := readLineInput(reader, terminal, outputMode, 6)
	if err != nil {
		return
	}

	if input == "" {
		return
	}

	num, parseErr := strconv.Atoi(input)
	if parseErr != nil || num < 1 || num > totalMsgs {
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(invalidMsgStr)), outputMode)
		time.Sleep(500 * time.Millisecond)
		return
	}

	*currentMsgNum = num
}

// displayReaderHelp shows the help screen for message reader commands.
func displayReaderHelp(terminal *term.Terminal, outputMode ansi.OutputMode, isSysop bool) {
	help := "\r\n" +
		"|15Message Reader Help|07\r\n" +
		"|08" + strings.Repeat("-", 40) + "|07\r\n" +
		"|15N|07ext Message          |15#|07 Read Message #\r\n" +
		"|15R|07eply to Message       |15P|07ost a Message\r\n" +
		"|15S|07 Prev Message        |15T|07hread Search\r\n" +
		"|15J|07ump to Message #     |15M|07ail Reply\r\n" +
		"|15L|07ist Titles           |15Q|07uit Reader\r\n"
	if isSysop {
		help += "|01D|07elete Message\r\n"
	}
	help += "|08" + strings.Repeat("-", 40) + "|07\r\n"

	terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(help)), outputMode)
	time.Sleep(2 * time.Second)
}
