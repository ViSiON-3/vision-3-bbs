package menu

import (
	"bufio"
	"fmt"
	"log"
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

// handleReply manages the reply flow matching Pascal's reply handling.
func handleReply(e *MenuExecutor, s ssh.Session, ih *editor.InputHandler, terminal *term.Terminal,
	userManager *user.UserMgr, currentUser *user.User, nodeNumber int,
	outputMode ansi.OutputMode, currentMsg *message.DisplayMessage,
	currentAreaID int, totalMsgCount *int, currentMsgNum *int, confName, areaName string) string {

	// Prepare quote data for /Q command
	// Split message body into lines for quoting
	quoteLines := strings.Split(currentMsg.Body, "\n")

	// Format date/time from message
	quoteDate := currentMsg.DateTime.Format("01/02/2006")
	quoteTime := currentMsg.DateTime.Format("3:04 PM")

	// Auto-generate subject with "RE: " prefix (no prompt needed)
	newSubject := generateReplySubject(currentMsg.Subject)
	if strings.TrimSpace(newSubject) == "" {
		terminalio.WriteProcessedBytes(terminal, []byte(e.LoadedStrings.MsgReplySubjectEmpty), outputMode)
		time.Sleep(1 * time.Second)
		return ""
	}

	terminalio.WriteProcessedBytes(terminal, []byte(e.LoadedStrings.MsgLaunchingEditor), outputMode)

	// Start with empty editor - user will use /Q command to quote if desired
	// Pass message metadata for quoting (from, title, date, time, isAnon, lines)
	replyNextMsg := *totalMsgCount + 1
	replyCtx := editor.EditorContext{
		NodeNumber: nodeNumber,
		NextMsgNum: replyNextMsg,
		ConfArea:   fmt.Sprintf("%s > %s", confName, areaName),
	}
	replyBody, saved, editErr := editor.RunEditorWithMetadata("", s, s, outputMode, newSubject, currentMsg.To, currentUser.Handle, false,
		currentMsg.From, currentMsg.Subject, quoteDate, quoteTime, false, quoteLines, ih, replyCtx)
	if editErr != nil {
		log.Printf("ERROR: Node %d: Editor failed: %v", nodeNumber, editErr)
		terminalio.WriteProcessedBytes(terminal, []byte(e.LoadedStrings.MsgEditorError), outputMode)
		time.Sleep(2 * time.Second)
		return ""
	}

	if !saved {
		terminalio.WriteProcessedBytes(terminal, []byte(e.LoadedStrings.MsgReplyCancelled), outputMode)
		time.Sleep(1 * time.Second)
		return ""
	}

	// Append auto-signature if user has one
	if currentUser.AutoSignature != "" {
		replyBody = replyBody + "\n\n" + currentUser.AutoSignature
	}

	// Save reply
	replyMsgID := currentMsg.MsgID
	_, err := e.MessageMgr.AddMessage(currentAreaID, currentUser.Handle, currentMsg.From,
		newSubject, replyBody, replyMsgID)
	if err != nil {
		log.Printf("ERROR: Node %d: Failed to save reply: %v", nodeNumber, err)
		terminalio.WriteProcessedBytes(terminal, []byte(e.LoadedStrings.MsgReplyError), outputMode)
		time.Sleep(2 * time.Second)
	} else {
		currentUser.MessagesPosted++
		if err := userManager.UpdateUser(currentUser); err != nil {
			log.Printf("ERROR: Node %d: Failed to update MessagesPosted for user %s: %v", nodeNumber, currentUser.Handle, err)
		}
		terminalio.WriteProcessedBytes(terminal, []byte(e.LoadedStrings.MsgReplySuccess), outputMode)
		time.Sleep(1 * time.Second)
		*totalMsgCount++
		if *currentMsgNum < *totalMsgCount {
			*currentMsgNum++
		}
	}
	return ""
}

// handleThread prompts for forward/backward and searches for matching subject.
func handleThread(reader *bufio.Reader, e *MenuExecutor, terminal *term.Terminal,
	outputMode ansi.OutputMode, areaID int,
	currentMsgNum *int, totalMsgs int, subject string) {

	terminalio.WriteProcessedBytes(terminal, []byte("\r\n"), outputMode)
	terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.LoadedStrings.MsgThreadPrompt)), outputMode)

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
		msg := fmt.Sprintf(e.LoadedStrings.MsgNoThreadFound, dir)
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
