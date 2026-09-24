package qwk

import (
	"archive/zip"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Packet is a parsed QWK packet as a network node receives it from its hub.
type Packet struct {
	BBSID       string // hub's QWK ID from CONTROL.DAT
	BBSName     string
	Conferences []ConferenceInfo // hub's conference list from CONTROL.DAT
	Messages    []NetMessage
	// ParseError is set when MESSAGES.DAT ended early; Messages holds what
	// was read before the bad block.
	ParseError error
}

// ReadPacket parses a QWK packet archive: CONTROL.DAT for the hub identity
// and conference list, MESSAGES.DAT for the messages, HEADERS.DAT for the
// extended fields. Files are matched case-insensitively, since packers
// differ.
func ReadPacket(r io.ReaderAt, size int64) (*Packet, error) {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return nil, fmt.Errorf("open QWK archive: %w", err)
	}
	find := func(name string) *zip.File {
		for _, f := range zr.File {
			if strings.EqualFold(f.Name, name) {
				return f
			}
		}
		return nil
	}
	read := func(f *zip.File) ([]byte, error) {
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		defer func() { _ = rc.Close() }() // read-only zip entry
		return io.ReadAll(rc)
	}

	p := &Packet{}
	if f := find("CONTROL.DAT"); f != nil {
		data, err := read(f)
		if err != nil {
			return nil, fmt.Errorf("read CONTROL.DAT: %w", err)
		}
		p.BBSID, p.BBSName, p.Conferences = parseControlDAT(data)
	}

	headers := map[int]ExtHeader{}
	if f := find("HEADERS.DAT"); f != nil {
		if data, err := read(f); err == nil {
			headers = parseHeadersDAT(data)
		}
	}

	f := find("MESSAGES.DAT")
	if f == nil {
		return p, nil // an empty packet from a hub with nothing new
	}
	data, err := read(f)
	if err != nil {
		return nil, fmt.Errorf("read MESSAGES.DAT: %w", err)
	}
	if len(data) < BlockSize {
		return p, nil
	}
	p.Messages, p.ParseError = parseNetMessages(data, headers)
	return p, nil
}

// ParseMSGPayload parses a REP-style .MSG payload (first block is the BBS
// ID, then messages). It is what a hub, or a node re-reading its own
// unsent REP, needs.
func ParseMSGPayload(data []byte, headers map[int]ExtHeader) (bbsID string, msgs []NetMessage, err error) {
	if len(data) < BlockSize {
		return "", nil, fmt.Errorf("payload too short (%d bytes)", len(data))
	}
	msgs, err = parseNetMessages(data, headers)
	return firstBlockID(data), msgs, err
}

// parseControlDAT reads the fields a node cares about. The format is
// line-oriented: BBS name, city, phone, sysop, "serial,BBSID", date, user,
// blank, 0, total, conference-count-minus-one, then number/name pairs.
func parseControlDAT(data []byte) (bbsID, bbsName string, confs []ConferenceInfo) {
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	lines := strings.Split(text, "\n")
	get := func(i int) string {
		if i < len(lines) {
			return strings.TrimSpace(lines[i])
		}
		return ""
	}
	bbsName = get(0)
	if _, id, ok := strings.Cut(get(4), ","); ok {
		bbsID = strings.ToUpper(strings.TrimSpace(id))
	}
	count, err := strconv.Atoi(get(10))
	if err != nil {
		return bbsID, bbsName, nil
	}
	for i := 0; i <= count; i++ {
		numLine := get(11 + i*2)
		name := get(12 + i*2)
		if numLine == "" {
			break
		}
		n, err := strconv.Atoi(numLine)
		if err != nil {
			break
		}
		confs = append(confs, ConferenceInfo{Number: n, Name: name})
	}
	return bbsID, bbsName, confs
}
