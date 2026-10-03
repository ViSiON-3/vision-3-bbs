package message

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/jam"
)

// GetMessageKludges returns a message's control information as display
// lines, for a sysop diagnosing how it arrived (duplicates, routing,
// software): the JAM addresses, dates and attributes, then the FTN kludges —
// MSGID, REPLY and PID first, the remaining kludge lines (from the header
// subfields and any left in the body text), and finally PATH and SEEN-BY, in
// the order they appear on the wire.
func (mm *MessageManager) GetMessageKludges(areaID, msgNum int) ([]string, error) {
	b, _, err := mm.openBase(areaID)
	if err != nil {
		return nil, err
	}
	defer func() {
		if cerr := b.Close(); cerr != nil {
			slog.Warn("closing JAM base", "error", cerr)
		}
	}()

	msg, err := b.ReadMessage(msgNum)
	if err != nil {
		return nil, err
	}
	return messageKludgeLines(msg), nil
}

// messageKludgeLines builds GetMessageKludges' lines from a read message.
func messageKludgeLines(msg *jam.Message) []string {
	var lines []string
	add := func(label, val string) {
		lines = append(lines, label+": "+val)
	}

	hdr := msg.Header
	var ids, other, paths, seenBys []string
	for _, sf := range hdr.Subfields {
		val := cleanKludge(string(sf.Buffer))
		switch sf.LoID {
		case jam.SfldOAddress:
			add("Orig address", val)
		case jam.SfldDAddress:
			add("Dest address", val)
		case jam.SfldMsgID:
			ids = append(ids, "MSGID: "+val)
		case jam.SfldReplyID:
			ids = append(ids, "REPLY: "+val)
		case jam.SfldPID:
			ids = append(ids, "PID: "+val)
		case jam.SfldTrace:
			other = append(other, "Via "+val)
		case jam.SfldFTSKludge:
			other = append(other, val)
		case jam.SfldFlags:
			other = append(other, "FLAGS "+val)
		case jam.SfldTZUTCInfo:
			other = append(other, "TZUTC: "+val)
		case jam.SfldPath2D:
			paths = append(paths, "PATH: "+val)
		case jam.SfldSeenBy2D:
			seenBys = append(seenBys, "SEEN-BY: "+val)
		}
	}

	// Kludges stored in the body text rather than in subfields. Text after
	// the tear line holds SEEN-BY and PATH on systems that keep them there.
	for _, line := range strings.Split(strings.NewReplacer("\r\n", "\n", "\r", "\n").Replace(msg.Text), "\n") {
		switch {
		case strings.HasPrefix(line, "\x01"):
			other = append(other, cleanKludge(line))
		case strings.HasPrefix(line, "SEEN-BY:"):
			seenBys = append(seenBys, cleanKludge(line))
		}
	}

	add("Written", jamTime(hdr.DateWritten))
	add("Received", jamTime(hdr.DateReceived))
	add("Processed", jamTime(hdr.DateProcessed))
	add("Attributes", fmt.Sprintf("%08X %s", hdr.Attribute, attributeNames(hdr.Attribute)))
	if hdr.ReplyTo != 0 || hdr.Reply1st != 0 || hdr.ReplyNext != 0 {
		add("Reply links", fmt.Sprintf("to #%d, first #%d, next #%d", hdr.ReplyTo, hdr.Reply1st, hdr.ReplyNext))
	}

	lines = append(lines, ids...)
	lines = append(lines, other...)
	lines = append(lines, seenBys...)
	return append(lines, paths...)
}

// cleanKludge drops the ^A lead-in and any other control characters, which
// would otherwise reach the terminal.
func cleanKludge(s string) string {
	return strings.TrimSpace(strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s))
}

// jamTime formats a JAM timestamp, which is stored as local time without a
// zone, so it is shown as recorded.
func jamTime(ts uint32) string {
	if ts == 0 {
		return "(none)"
	}
	return time.Unix(int64(ts), 0).UTC().Format("2006-01-02 15:04:05")
}

// attributeNames lists the set JAM attribute flags by name.
func attributeNames(attr uint32) string {
	names := []struct {
		bit  uint32
		name string
	}{
		{jam.MsgLocal, "Local"}, {jam.MsgInTransit, "InTransit"}, {jam.MsgPrivate, "Private"},
		{jam.MsgRead, "Read"}, {jam.MsgSent, "Sent"}, {jam.MsgKillSent, "KillSent"},
		{jam.MsgArchiveSent, "ArchiveSent"}, {jam.MsgHold, "Hold"}, {jam.MsgCrash, "Crash"},
		{jam.MsgImmediate, "Immediate"}, {jam.MsgDirect, "Direct"}, {jam.MsgGate, "Gate"},
		{jam.MsgFileRequest, "FileReq"}, {jam.MsgFileAttach, "FileAttach"},
		{jam.MsgTruncFile, "TruncFile"}, {jam.MsgKillFile, "KillFile"},
		{jam.MsgReceiptReq, "ReceiptReq"}, {jam.MsgConfirmReq, "ConfirmReq"},
		{jam.MsgOrphan, "Orphan"}, {jam.MsgEncrypt, "Encrypted"}, {jam.MsgCompress, "Compressed"},
		{jam.MsgEscaped, "Escaped"}, {jam.MsgFPU, "FPU"}, {jam.MsgTypeLocal, "TypeLocal"},
		{jam.MsgTypeEcho, "Echo"}, {jam.MsgTypeNet, "Netmail"}, {jam.MsgNoDisp, "NoDisp"},
		{jam.MsgLocked, "Locked"}, {jam.MsgDeleted, "Deleted"},
	}
	var set []string
	for _, n := range names {
		if attr&n.bit != 0 {
			set = append(set, n.name)
		}
	}
	if len(set) == 0 {
		return "(none)"
	}
	return strings.Join(set, " ")
}
