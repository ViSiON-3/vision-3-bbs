package qwk

import (
	"bytes"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ExtHeader is the subset of HEADERS.DAT fields ViSiON/3 emits and consumes.
type ExtHeader struct {
	Offset      int // byte offset of the message header in MESSAGES.DAT / .MSG
	MessageID   string
	Subject     string
	To          string
	From        string
	WhenWritten string
}

// encodeHeadersDAT renders HEADERS.DAT sections (ordered by offset) as INI bytes.
func encodeHeadersDAT(hs []ExtHeader) []byte {
	sorted := make([]ExtHeader, len(hs))
	copy(sorted, hs)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Offset < sorted[j].Offset })

	var buf bytes.Buffer
	for i, h := range sorted {
		if i > 0 {
			buf.WriteString("\r\n")
		}
		fmt.Fprintf(&buf, "[%x]\r\n", h.Offset)
		writeHeaderField(&buf, "Message-ID", h.MessageID)
		writeHeaderField(&buf, "Subject", h.Subject)
		writeHeaderField(&buf, "To", h.To)
		writeHeaderField(&buf, "From", h.From)
		writeHeaderField(&buf, "WhenWritten", h.WhenWritten)
	}
	return buf.Bytes()
}

func writeHeaderField(buf *bytes.Buffer, key, val string) {
	if val == "" {
		return
	}
	buf.WriteString(key)
	buf.WriteString(": ")
	buf.WriteString(val)
	buf.WriteString("\r\n")
}

// parseHeadersDAT parses HEADERS.DAT into a map keyed by message byte offset.
// Malformed lines and unknown keys are ignored; empty/garbage input yields an
// empty map.
func parseHeadersDAT(data []byte) map[int]ExtHeader {
	out := make(map[int]ExtHeader)
	var cur ExtHeader
	have := false
	flush := func() {
		if have {
			out[cur.Offset] = cur
		}
	}
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(strings.TrimRight(raw, "\r"))
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			flush()
			off, err := strconv.ParseInt(line[1:len(line)-1], 16, 64)
			if err != nil {
				have = false
				continue
			}
			cur = ExtHeader{Offset: int(off)}
			have = true
			continue
		}
		if !have {
			continue
		}
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		val = strings.TrimSpace(val)
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "message-id":
			cur.MessageID = val
		case "subject":
			cur.Subject = val
		case "to":
			cur.To = val
		case "from":
			cur.From = val
		case "whenwritten":
			cur.WhenWritten = val
		}
	}
	flush()
	return out
}

// smbTimezone encodes a UTC offset (in seconds) as the SMB hex timezone. Bits
// 0-11 hold the absolute minutes; 0x2000 marks west of UTC, 0x1000 east. The US
// and daylight flag bits are intentionally not set (they cannot be reliably
// inferred; the ISO-8601 offset carries the real offset).
func smbTimezone(offsetSeconds int) string {
	mins := offsetSeconds / 60
	var v uint16
	if mins < 0 {
		v = 0x2000 | uint16(-mins)
	} else {
		v = 0x1000 | uint16(mins)
	}
	return fmt.Sprintf("%x", v)
}

// synthMessageID builds a deterministic RFC822-style Message-ID for a local
// message, which has no FTN MSGID.
func synthMessageID(bbsID string, conference, number int) string {
	return fmt.Sprintf("<%d.%d@%s>", number, conference, strings.ToLower(bbsID))
}

// formatWhenWritten renders a timestamp as ISO-8601 with offset plus the SMB
// hex timezone.
func formatWhenWritten(t time.Time) string {
	_, off := t.Zone()
	return t.Format("20060102150405-0700") + "  " + smbTimezone(off)
}

// extHeadersFor builds the HEADERS.DAT entries for a set of packet messages
// given their byte offsets in MESSAGES.DAT / .MSG.
func extHeadersFor(msgs []PacketMessage, offsets []int, bbsID string) []ExtHeader {
	hs := make([]ExtHeader, 0, len(msgs))
	for i, m := range msgs {
		hs = append(hs, ExtHeader{
			Offset:      offsets[i],
			MessageID:   synthMessageID(bbsID, m.Conference, m.Number),
			Subject:     m.Subject,
			To:          m.To,
			From:        m.From,
			WhenWritten: formatWhenWritten(m.DateTime),
		})
	}
	return hs
}
