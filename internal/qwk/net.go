package qwk

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// NetMessage is a message as it travels between QWK network nodes: the
// 128-byte header fields plus the routing and threading data that
// HEADERS.DAT and the Synchronet-style body kludges carry. It is what the
// packet reader returns for a hub's QWK packet and what the REP writer takes
// for the node's outbound packet.
type NetMessage struct {
	Conference    int
	Number        int // message number on the sending system (0 if unknown)
	From          string
	To            string
	Subject       string
	DateTime      time.Time // in the sender's zone when the packet said which
	Body          string    // text only: kludges and QWKE header lines removed
	Private       bool
	ReplyToNumber int // QWK reference field: parent message number (0 = none)

	MessageID     string // RFC822-style Message-ID (HEADERS.DAT or @MSGID)
	ReplyID       string // Reply-ID / In-Reply-To (HEADERS.DAT or @REPLY)
	Via           string // @VIA route as received; empty for the sender's own posts
	SenderNetAddr string // HEADERS.DAT SenderNetAddr (QWK ID of the origin)
	UTF8          bool   // HEADERS.DAT Utf8: the body is UTF-8, not CP437
	Status        byte   // raw status byte from the header block
}

// Body kludge prefixes defined by Synchronet for QWK networking. They sit at
// the top of the body, one per line, before the text.
const (
	kludgeVia     = "@VIA:"
	kludgeMsgID   = "@MSGID:"
	kludgeReply   = "@REPLY:"
	kludgeTZ      = "@TZ:"
	kludgeReplyTo = "@REPLYTO:"
)

// bodyKludges is what the top of a packet body said before the text began.
type bodyKludges struct {
	via, msgID, replyID, tz, replyTo string
	to, from, subject                string // QWKE full-length header lines
}

// splitBodyKludges takes the decoded body (newline-terminated lines) and
// peels off the leading kludge lines. Matching is case-insensitive, as
// Synchronet's is. The first line that is not a kludge ends the prefix; the
// remaining text is returned untouched apart from that split.
func splitBodyKludges(body string) (bodyKludges, string) {
	var k bodyKludges
	rest := body
	for rest != "" {
		line, tail, _ := strings.Cut(rest, "\n")
		upper := strings.ToUpper(line)
		var target *string
		var prefix string
		switch {
		case strings.HasPrefix(upper, kludgeVia):
			target, prefix = &k.via, kludgeVia
		case strings.HasPrefix(upper, kludgeMsgID):
			target, prefix = &k.msgID, kludgeMsgID
		// @REPLYTO: shares the @REPLY prefix, so it has to be tested first.
		case strings.HasPrefix(upper, kludgeReplyTo):
			target, prefix = &k.replyTo, kludgeReplyTo
		case strings.HasPrefix(upper, kludgeReply):
			target, prefix = &k.replyID, kludgeReply
		case strings.HasPrefix(upper, kludgeTZ):
			target, prefix = &k.tz, kludgeTZ
		case strings.HasPrefix(upper, "TO:"):
			target, prefix = &k.to, "TO:"
		case strings.HasPrefix(upper, "FROM:"):
			target, prefix = &k.from, "FROM:"
		case strings.HasPrefix(upper, "SUBJECT:"):
			target, prefix = &k.subject, "SUBJECT:"
		default:
			return k, rest
		}
		*target = strings.TrimSpace(line[len(prefix):])
		rest = tail
	}
	return k, rest
}

// ParseSMBTimezone decodes a Synchronet SMB time zone value into a UTC
// offset in seconds. Values within +/-1000 are a plain signed minute offset;
// otherwise bits 0-11 are minutes, 0x2000/0x4000 (western, US) make the
// offset negative and 0x8000 adds an hour of daylight saving.
func ParseSMBTimezone(hex string) (int, bool) {
	hex = strings.TrimPrefix(strings.TrimSpace(strings.ToLower(hex)), "0x")
	if hex == "" {
		return 0, false
	}
	v, err := strconv.ParseUint(hex, 16, 16)
	if err != nil {
		return 0, false
	}
	if signed := int16(v); signed >= -1000 && signed <= 1000 {
		return int(signed) * 60, true
	}
	mins := int(v & 0x0fff)
	if v&0x6000 != 0 {
		mins = -mins
	}
	if v&0x8000 != 0 {
		mins += 60
	}
	return mins * 60, true
}

// parseWhenWritten reads a HEADERS.DAT WhenWritten value: an ISO-8601
// timestamp with numeric offset, optionally followed by the SMB zone.
func parseWhenWritten(s string) (time.Time, bool) {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return time.Time{}, false
	}
	for _, layout := range []string{"20060102150405-0700", "20060102150405Z0700", "20060102150405"} {
		if t, err := time.Parse(layout, fields[0]); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// parseBlockDate reads the header's MM-DD-YY and HH:MM fields. Two-digit
// years below 80 are taken as 20xx. The result is in loc.
func parseBlockDate(header []byte, loc *time.Location) time.Time {
	digit := func(i int) int { return int(header[i] & 0x0f) }
	mon := digit(8)*10 + digit(9)
	day := digit(11)*10 + digit(12)
	yy := digit(14)*10 + digit(15)
	hh := digit(16)*10 + digit(17)
	mm := digit(19)*10 + digit(20)
	year := 1900 + yy
	if yy < 80 {
		year = 2000 + yy
	}
	if mon < 1 || mon > 12 || day < 1 || day > 31 || hh > 23 || mm > 59 {
		return time.Time{}
	}
	return time.Date(year, time.Month(mon), day, hh, mm, 0, 0, loc)
}

// blockFields is the fixed-width content of one 128-byte header block.
type blockFields struct {
	status     byte
	number     int
	to         string
	from       string
	subject    string
	ref        int
	blocks     int
	conference int
}

// parseHeaderBlock decodes the fixed-width fields. The conference comes from
// the binary field at 123-124; the ASCII number field is returned as-is so
// callers can treat it as a message number (QWK) or conference (REP).
func parseHeaderBlock(header []byte) blockFields {
	atoi := func(b []byte) int {
		n, _ := strconv.Atoi(strings.TrimSpace(string(b)))
		return n
	}
	return blockFields{
		status:     header[0],
		number:     atoi(header[1:8]),
		to:         strings.TrimSpace(string(header[21:46])),
		from:       strings.TrimSpace(string(header[46:71])),
		subject:    strings.TrimSpace(string(header[71:96])),
		ref:        atoi(header[108:116]),
		blocks:     atoi(header[116:122]),
		conference: int(header[123]) | int(header[124])<<8,
	}
}

// decodeNetBody turns raw body bytes into text with newlines, keeping the
// interior intact (kludge parsing needs the exact line structure) and only
// trimming the block padding at the end.
func decodeNetBody(data []byte) string {
	data = bytes.TrimRight(data, " \x00")
	out := make([]byte, len(data))
	for i, b := range data {
		if b == 0xE3 {
			out[i] = '\n'
		} else {
			out[i] = b
		}
	}
	return strings.ReplaceAll(string(out), "\r\n", "\n")
}

// parseNetMessages walks a MESSAGES.DAT or .MSG payload (first block already
// skipped by the caller through pos) and returns every message. A message
// whose block count is bad or runs past the end stops the walk; messages
// before it are still returned along with the error.
func parseNetMessages(data []byte, headers map[int]ExtHeader) ([]NetMessage, error) {
	var msgs []NetMessage
	pos := BlockSize
	for pos+BlockSize <= len(data) {
		header := data[pos : pos+BlockSize]
		bf := parseHeaderBlock(header)
		if bf.blocks < 1 {
			// A run of blank padding blocks at the end is normal in some
			// packers; anything else is corruption.
			if bytes.TrimSpace(header) == nil || len(bytes.TrimSpace(header)) == 0 {
				pos += BlockSize
				continue
			}
			return msgs, fmt.Errorf("bad block count at offset %d", pos)
		}
		total := bf.blocks * BlockSize
		if pos+total > len(data) {
			return msgs, fmt.Errorf("message at offset %d runs past end of data", pos)
		}
		msgs = append(msgs, buildNetMessage(bf, header, data[pos+BlockSize:pos+total], headers[pos]))
		pos += total
	}
	return msgs, nil
}

// buildNetMessage merges the header block, body kludges and HEADERS.DAT
// entry for one message. Precedence for names and IDs is HEADERS.DAT, then
// the body kludges, then the 25-character block fields.
func buildNetMessage(bf blockFields, header, body []byte, ext ExtHeader) NetMessage {
	kl, text := splitBodyKludges(decodeNetBody(body))
	m := NetMessage{
		Conference:    bf.conference,
		Number:        bf.number,
		From:          bf.from,
		To:            bf.to,
		Subject:       bf.subject,
		Body:          strings.TrimRight(text, " \n"),
		Private:       bf.status == StatusPrivate || bf.status == '+',
		ReplyToNumber: bf.ref,
		Status:        bf.status,
		Via:           kl.via,
		MessageID:     kl.msgID,
		ReplyID:       kl.replyID,
	}
	if kl.to != "" {
		m.To = kl.to
	}
	if kl.from != "" {
		m.From = kl.from
	}
	if kl.subject != "" {
		m.Subject = kl.subject
	}
	if ext.To != "" {
		m.To = ext.To
	}
	if ext.From != "" {
		m.From = ext.From
	}
	if ext.Subject != "" {
		m.Subject = ext.Subject
	}
	if ext.MessageID != "" {
		m.MessageID = ext.MessageID
	}
	if ext.ReplyID != "" {
		m.ReplyID = ext.ReplyID
	}
	m.SenderNetAddr = ext.SenderNetAddr
	m.UTF8 = ext.UTF8
	if ext.Conference > 0 {
		m.Conference = ext.Conference
	}

	// Date: HEADERS.DAT WhenWritten carries the offset; otherwise the block
	// date read in the @TZ zone, or UTC when nothing says.
	if t, ok := parseWhenWritten(ext.WhenWritten); ok {
		m.DateTime = t
	} else {
		loc := time.UTC
		if off, ok := ParseSMBTimezone(kl.tz); ok {
			loc = time.FixedZone("", off)
		}
		m.DateTime = parseBlockDate(header, loc)
	}
	return m
}

// encodeKludgeLines renders the body kludges a node sends with a message.
// @VIA is passed through when relaying; the hub prepends the node's ID.
func encodeKludgeLines(m NetMessage) string {
	var b strings.Builder
	if m.Via != "" {
		b.WriteString(kludgeVia + " " + m.Via + "\n")
	}
	if m.MessageID != "" {
		b.WriteString(kludgeMsgID + " " + m.MessageID + "\n")
	}
	if m.ReplyID != "" {
		b.WriteString(kludgeReply + " " + m.ReplyID + "\n")
	}
	if !m.DateTime.IsZero() {
		_, off := m.DateTime.Zone()
		b.WriteString(kludgeTZ + " " + smbTimezone(off) + "\n")
	}
	return b.String()
}
