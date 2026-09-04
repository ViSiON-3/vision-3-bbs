package message

import (
	"fmt"
	"strings"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/jam"
)

// splitNetmailTo splits a "user@zone:net/node" string into the username and
// FTN address parts. If the string doesn't contain a valid FTN address after
// the '@', it returns the original string unchanged with an empty address.
func splitNetmailTo(to string) (name, addr string) {
	idx := strings.LastIndex(to, "@")
	if idx <= 0 {
		return to, ""
	}
	candidate := strings.TrimSpace(to[idx+1:])
	if _, err := jam.ParseAddress(candidate); err != nil {
		return to, "" // not a valid FTN address, keep as-is
	}
	return strings.TrimSpace(to[:idx]), candidate
}

// addMessage is the shared implementation behind AddMessage, AddMessageWithDate,
// AddPrivateMessage, and AddReply.
func (mm *MessageManager) addMessage(areaID int, from, to, subject, body, replyToMsgID string,
	replyToNum int, dateTime time.Time, private bool) (int, error) {
	b, area, err := mm.openBase(areaID)
	if err != nil {
		return 0, err
	}

	// Apply body transform (e.g. V3Net tearline/origin) for the local JAM copy.
	// The original body is preserved for OnMessagePosted so the wire message
	// carries tearline/origin as separate protocol fields, not inline.
	jamBody := body
	if !private && mm.BodyTransform != nil {
		jamBody = mm.BodyTransform(areaID, body)
	}

	msg := jam.NewMessage()
	msg.From = from
	msg.To = to
	msg.Subject = subject
	msg.Text = jamBody
	msg.DateTime = dateTime

	if private {
		msg.Header = &jam.MessageHeader{Attribute: jam.MsgPrivate | jam.MsgLocal}
	}
	// replyToNum is a 1-based JAM message number; 0 means "no parent".
	if replyToNum > 0 {
		msg.ReplyTo = uint32(replyToNum)
	}
	if replyToMsgID != "" {
		msg.ReplyID = replyToMsgID
	}

	msgType := jam.DetermineMessageType(area.AreaType, area.EchoTag)

	// For netmail, split "user@address" into separate To and DestAddr fields.
	if msgType.IsNetmail() {
		name, addr := splitNetmailTo(to)
		msg.To = name
		if addr != "" {
			msg.DestAddr = addr
		}
	}

	var msgNum int
	if msgType.IsEchomail() || msgType.IsNetmail() {
		msg.OrigAddr = area.OriginAddr
		msgNum, err = b.WriteMessageExt(msg, msgType, area.EchoTag, mm.originTextForNetwork(area.Network))
	} else {
		msgNum, err = b.WriteMessage(msg)
	}

	// Close the base before firing the callback. The V3Net hook calls
	// MarkMessageSent which re-opens the same JAM base, so having it
	// still open here can cause nested-open/file-sharing issues.
	cerr := b.Close()

	// The write already succeeded at this point, so run the post-write hooks
	// even if the close failed — the message is on disk and downstream
	// consumers (thread index, V3Net delivery) must still see it. The close
	// error is folded into the returned error afterwards.
	if err == nil {
		mm.invalidateThreadIndex(areaID)
		if !private && mm.OnMessagePosted != nil {
			mm.OnMessagePosted(area, msgNum, from, to, subject, body)
		}
		if cerr != nil {
			err = fmt.Errorf("closing message base: %w", cerr)
		}
	}
	return msgNum, err
}

// AddMessage creates and writes a new message to the specified area.
// For echomail areas, it automatically handles MSGID, kludges, tearline, and origin.
// For netmail areas, "user@zone:net/node" in the To field is automatically split
// into the username and destination address.
// Returns the 1-based message number assigned.
func (mm *MessageManager) AddMessage(areaID int, from, to, subject, body, replyToMsgID string) (int, error) {
	return mm.addMessage(areaID, from, to, subject, body, replyToMsgID, 0, time.Now(), false)
}

// AddMessageWithDate is like AddMessage but uses the provided timestamp instead
// of time.Now(). Used by V3Net to preserve the original authored date.
func (mm *MessageManager) AddMessageWithDate(areaID int, from, to, subject, body, replyToMsgID string, dateTime time.Time) (int, error) {
	return mm.addMessage(areaID, from, to, subject, body, replyToMsgID, 0, dateTime, false)
}

// AddPrivateMessage creates and writes a new private (MSG_PRIVATE) message.
// For netmail areas, "user@zone:net/node" in the To field is automatically split
// into the username and destination address. Returns the 1-based message number.
func (mm *MessageManager) AddPrivateMessage(areaID int, from, to, subject, body, replyToMsgID string) (int, error) {
	return mm.addMessage(areaID, from, to, subject, body, replyToMsgID, 0, time.Now(), true)
}

// AddReply creates and writes a reply, recording the parent's message number
// (replyToNum) as the JAM ReplyTo so local areas thread, and the parent's FTN
// MSGID (replyToMsgID, may be "") as ReplyID so echomail links via jam.Link().
func (mm *MessageManager) AddReply(areaID int, from, to, subject, body, replyToMsgID string, replyToNum int) (int, error) {
	return mm.addMessage(areaID, from, to, subject, body, replyToMsgID, replyToNum, time.Now(), false)
}

// AddPrivateReply is AddReply for private mail: it records the parent pointer
// while preserving the MSG_PRIVATE flag, so a reply to a private message stays
// private (and therefore visible to its recipient).
func (mm *MessageManager) AddPrivateReply(areaID int, from, to, subject, body, replyToMsgID string, replyToNum int) (int, error) {
	return mm.addMessage(areaID, from, to, subject, body, replyToMsgID, replyToNum, time.Now(), true)
}
