package menu

import (
	"bufio"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/gliderlabs/ssh"
	"golang.org/x/term"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// Moving a message to another area from the reader (V): ViSiON/2's &M. A
// co-sysop may move any message, and an area's sponsor a message out of
// their area, into another local area they may post in. Only local public
// areas take part: a move into echomail, QWK or V3Net would send the message
// to other boards, one out of them would only take away this board's copy,
// and private mail belongs to its recipient.

// localPublicArea reports whether area is a local area other than private
// mail.
func localPublicArea(area *message.MessageArea) bool {
	if area == nil || strings.EqualFold(area.Tag, "PRIVMAIL") {
		return false
	}
	t := strings.ToLower(strings.TrimSpace(area.AreaType))
	return t == "" || t == "local"
}

// areaPlainName is area's name with its colour codes taken out, for text
// stored in a message.
func areaPlainName(area *message.MessageArea) string {
	name := strings.TrimSpace(ansi.StripAnsi(string(ansi.ReplacePipeCodes([]byte(area.Name)))))
	if name == "" {
		return area.Tag
	}
	return name
}

// movedLineFormat is the line added to the end of a moved message: where it
// came from, who moved it and when.
const movedLineFormat = " * Moved from %s by %s on %s"

// withMovedLine is body ending with a line saying handle moved it from
// fromName at when.
func withMovedLine(body, fromName, handle string, when time.Time) string {
	return strings.TrimRight(body, "\n ") + "\n\n" +
		fmt.Sprintf(movedLineFormat, fromName, handle, strings.ToLower(when.Format("01/02/06 at 3:04 PM")))
}

// moveTargets are the areas currentUser may move a message from area into:
// the other local public areas they may read and post in, in the order the
// area list has them.
func moveTargets(e *MenuExecutor, s ssh.Session, terminal *term.Terminal, u *user.User, from *message.MessageArea, sessionStart time.Time) []*message.MessageArea {
	var out []*message.MessageArea
	for _, a := range e.MessageMgr.ListAreas() {
		if a.ID == from.ID || !localPublicArea(a) {
			continue
		}
		if a.ACSRead != "" && !checkACS(a.ACSRead, u, s, terminal, sessionStart) {
			continue
		}
		if a.ACSWrite != "" && !checkACS(a.ACSWrite, u, s, terminal, sessionStart) {
			continue
		}
		out = append(out, a)
	}
	return out
}

// moveMessage moves msg, which the reader is showing in area from, for
// currentUser: it lists the areas it can go to, takes one by number or tag,
// and asks to confirm. may reports whether the caller may move a message,
// as a sponsor or not; sponsor tells whether they are one now. Both are asked
// again as it moves, along with whether it is still the message shown. It
// returns true when the message was moved, and an error reading the caller's
// answers.
func (e *MenuExecutor) moveMessage(s ssh.Session, reader *bufio.Reader, terminal *term.Terminal,
	currentUser *user.User, nodeNumber int, outputMode ansi.OutputMode, sessionStart time.Time,
	from *message.MessageArea, msg *message.DisplayMessage,
	may func(m *message.DisplayMessage, sponsor bool) bool, sponsor func() bool,
	termHeight int) (bool, error) {

	notice := func(text string) {
		e.showReaderNotice(terminal, outputMode, text, termHeight)
	}
	say := func(text string) {
		if err := e.writeString(terminal, outputMode, text); err != nil {
			slog.Debug("move message write", "node", nodeNumber, "error", err)
		}
	}

	targets := moveTargets(e, s, terminal, currentUser, from, sessionStart)
	if len(targets) == 0 {
		notice("|07There's no other area to move it to.")
		return false, nil
	}
	say("\r\n|15Move this message to:|07\r\n")
	pager := e.newMorePager(s, terminal, outputMode, currentUser, termHeight, 2, "")
	for i, a := range targets {
		say(fmt.Sprintf("|15%3d|07. |11%-12s |07%s\r\n", i+1, callerText(a.Tag), callerText(areaPlainName(a))))
		if !pager.Line() {
			if err := pager.Err(); err != nil {
				return false, err
			}
			break
		}
	}
	say("|07Area |15#|07 or tag (Enter cancels): |15")
	input, err := readLineFromSessionIHMax(s, terminal, 20)
	say("|07")
	if err != nil {
		return false, err
	}
	input = strings.TrimSpace(input)
	if input == "" {
		return false, nil
	}
	var to *message.MessageArea
	if n, convErr := strconv.Atoi(input); convErr == nil {
		if n >= 1 && n <= len(targets) {
			to = targets[n-1]
		}
	} else {
		for _, a := range targets {
			if strings.EqualFold(a.Tag, input) {
				to = a
				break
			}
		}
	}
	if to == nil {
		notice("|01That isn't one of the areas listed.|07")
		return false, nil
	}

	confirmed, err := promptSingleChar(reader, terminal,
		fmt.Sprintf("\r\n |07Move this message to |15%s|07? [Y/N] ", callerText(areaPlainName(to))), outputMode)
	if err != nil {
		return false, err
	}
	if confirmed != 'Y' {
		return false, nil
	}

	// The right to move is checked again now: the sysop may have changed the
	// area's sponsor meanwhile. Then the message must still be the one shown,
	// and still theirs to move, when the original is deleted.
	isSponsor := sponsor()
	if !may(msg, isSponsor) {
		notice("|01You can no longer move this message. Nothing was moved.|07")
		return false, nil
	}
	fromName := areaPlainName(from)
	when := config.NowIn(e.GetServerConfig().Timezone)
	_, err = e.MessageMgr.MoveMessageIf(from.ID, msg.MsgNum, to.ID,
		func(m *message.DisplayMessage) bool { return message.SameMessage(m, msg) && may(m, isSponsor) },
		func(m *message.DisplayMessage) string {
			return withMovedLine(m.Body, fromName, currentUser.Handle, when)
		})
	if errors.Is(err, message.ErrMessageChanged) {
		notice("|01That message was changed or deleted elsewhere. Nothing was moved.|07")
		return false, nil
	}
	if err != nil {
		slog.Error("move message", "node", nodeNumber, "from", from.Tag, "to", to.Tag, "msg", msg.MsgNum, "error", err)
		notice("|01Error moving the message.|07")
		return false, nil
	}
	slog.Info("message moved", "node", nodeNumber, "handle", currentUser.Handle, "from", from.Tag, "to", to.Tag, "msg", msg.MsgNum, "author", msg.From)
	notice(fmt.Sprintf("|07Message moved to |15%s|07.", callerText(areaPlainName(to))))
	return true, nil
}
