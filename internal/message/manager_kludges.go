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

	// Kludges stored in the body text rather than in subfields go in the same
	// groups as their header counterparts. Text after the tear line holds
	// SEEN-BY and PATH on systems that keep them there; @PATH is the form
	// some keep for a message's own routing record.
	for _, line := range strings.Split(strings.NewReplacer("\r\n", "\n", "\r", "\n").Replace(msg.Text), "\n") {
		kludge := strings.HasPrefix(line, "\x01")
		val := cleanKludge(line)
		switch {
		case kludge && hasAnyPrefix(val, "MSGID:", "REPLY:", "PID:"):
			ids = append(ids, val)
		case kludge && strings.HasPrefix(val, "PATH:"):
			paths = append(paths, val)
		case kludge:
			other = append(other, val)
		case strings.HasPrefix(line, "SEEN-BY:"):
			seenBys = append(seenBys, val)
		case strings.HasPrefix(line, "PATH:"), strings.HasPrefix(line, "@PATH:"):
			paths = append(paths, val)
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

// cleanKludge drops the ^A lead-in and any other ASCII control bytes, which
// would otherwise reach the terminal, and trims surrounding spaces. It works
// on bytes: values are the sender's raw text, often CP437, and decoding them
// as UTF-8 would turn every byte above 0x7F into U+FFFD before the writer
// could convert it.
func cleanKludge(s string) string {
	b := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if c := s[i]; c >= 0x20 && c != 0x7f {
			b = append(b, c)
		}
	}
	return strings.Trim(string(b), " ")
}

// hasAnyPrefix reports whether s starts with one of prefixes.
func hasAnyPrefix(s string, prefixes ...string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
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
