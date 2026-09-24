package qwk

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"strings"
)

// NetREPOptions controls how a node's REP packet is built.
type NetREPOptions struct {
	// Tagline is added below a tearline at the end of every message, the
	// node's signature on the network. Blank adds nothing.
	Tagline string
	// NoHeaders omits HEADERS.DAT; the body kludges still carry the IDs.
	NoHeaders bool
	// NodeID is this node's QWK ID, used in Message-IDs that have to be
	// synthesized and in the tagline.
	NodeID string
}

// TaglineText renders the tearline-and-tagline block appended to outbound
// messages, in the form QWK networks expect: a "---" tearline and a line
// naming the software and the node. The CP437 filled square (0xFE) is the
// conventional bullet.
func TaglineText(software, tagline string) string {
	tag := strings.TrimSpace(tagline)
	if tag == "" {
		return ""
	}
	return "---\n \xfe " + software + " \xfe " + tag + "\n"
}

// WriteNetREP writes a QWK network REP packet for hubID: a ZIP holding
// <hubID>.MSG whose first block names the hub, followed by the messages,
// plus HEADERS.DAT unless disabled. It differs from WriteREP (the
// offline-reader form) in what each message carries: the hub's conference
// number in both the ASCII and binary fields, the QWKnet body kludges, the
// tearline/tagline, and the fuller HEADERS.DAT.
func WriteNetREP(w io.Writer, hubID string, msgs []NetMessage, opts NetREPOptions) error {
	hubID = strings.ToUpper(hubID)
	if len(hubID) > 8 {
		hubID = hubID[:8]
	}
	if hubID == "" {
		return fmt.Errorf("hub ID is required")
	}

	var msgBuf bytes.Buffer
	first := bytes.Repeat([]byte{' '}, BlockSize)
	copy(first, hubID)
	msgBuf.Write(first)

	tag := TaglineText("ViSiON/3", opts.Tagline)
	var hdrs []ExtHeader
	for _, m := range msgs {
		offset := msgBuf.Len()
		msgBuf.Write(formatNetMessage(m, tag))
		if !opts.NoHeaders {
			hdrs = append(hdrs, extHeaderForNet(m, offset))
		}
	}

	zw := zip.NewWriter(w)
	name := hubID + ".MSG"
	if err := writeZipEntry(zw, name, msgBuf.Bytes()); err != nil {
		_ = zw.Close() // cleanup on error path
		return fmt.Errorf("%s: %w", name, err)
	}
	if len(hdrs) > 0 {
		if err := writeZipEntry(zw, "HEADERS.DAT", encodeHeadersDAT(hdrs)); err != nil {
			_ = zw.Close() // cleanup on error path
			return fmt.Errorf("HEADERS.DAT: %w", err)
		}
	}
	if err := zw.Close(); err != nil {
		return fmt.Errorf("finalize REP archive: %w", err)
	}
	return nil
}

// formatNetMessage encodes one message in REP form, padded to whole blocks.
func formatNetMessage(m NetMessage, tagline string) []byte {
	body := encodeKludgeLines(m) + strings.TrimRight(m.Body, " \n") + "\n"
	if tagline != "" && !hasTearline(m.Body) {
		body += tagline
	}
	body = strings.ReplaceAll(body, "\r\n", "\n")
	body = strings.ReplaceAll(body, "\n", "\xe3")

	pm := PacketMessage{
		Conference:    m.Conference,
		Number:        m.Conference, // REP: the number field carries the conference
		From:          m.From,
		To:            m.To,
		Subject:       m.Subject,
		DateTime:      m.DateTime,
		Private:       m.Private,
		ReplyToNumber: m.ReplyToNumber,
	}
	header := formatMessage(pm)[:BlockSize]
	total := BlockSize + len(body)
	numBlocks := (total + BlockSize - 1) / BlockSize
	copyPadded(header[116:122], fmt.Sprintf("%6d", numBlocks), 6)

	out := make([]byte, numBlocks*BlockSize)
	for i := range out {
		out[i] = ' '
	}
	copy(out, header)
	copy(out[BlockSize:], body)
	return out
}

// hasTearline reports whether the body already ends in a tearline block,
// so a relayed message does not collect a second one.
func hasTearline(body string) bool {
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "---") && !strings.HasPrefix(line, "----") {
			return true
		}
	}
	return false
}

// extHeaderForNet builds the HEADERS.DAT section for an outbound message.
func extHeaderForNet(m NetMessage, offset int) ExtHeader {
	h := ExtHeader{
		Offset:        offset,
		MessageID:     m.MessageID,
		ReplyID:       m.ReplyID,
		Subject:       m.Subject,
		To:            m.To,
		From:          m.From,
		SenderNetAddr: m.SenderNetAddr,
		UTF8:          m.UTF8,
		Conference:    m.Conference,
	}
	if !m.DateTime.IsZero() {
		h.WhenWritten = formatWhenWritten(m.DateTime)
	}
	return h
}
