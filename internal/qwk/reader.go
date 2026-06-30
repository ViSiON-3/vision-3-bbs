package qwk

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"
)

// REPPacket is a parsed REP upload: the destination BBS ID found in the first
// block, the reply messages, and the raw .MSG payload (used for fingerprinting).
type REPPacket struct {
	BBSID    string
	Messages []REPMessage
	Payload  []byte
}

// ReadREP extracts messages from a QWK REP packet (ZIP archive). It is a thin
// wrapper over ReadREPPacket retained for callers that only need the messages.
func ReadREP(r io.ReaderAt, size int64, bbsID string) ([]REPMessage, error) {
	p, err := ReadREPPacket(r, size, bbsID)
	if err != nil {
		return nil, err
	}
	return p.Messages, nil
}

// ReadREPPacket reads a REP packet's <bbsID>.MSG payload and returns the
// first-block BBS ID, the parsed messages, and the raw payload bytes.
func ReadREPPacket(r io.ReaderAt, size int64, bbsID string) (*REPPacket, error) {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return nil, fmt.Errorf("failed to open REP archive: %w", err)
	}

	msgFileName := strings.ToUpper(bbsID) + ".MSG"
	var msgFile *zip.File
	for _, f := range zr.File {
		if strings.EqualFold(f.Name, msgFileName) {
			msgFile = f
			break
		}
	}
	// Fall back: if the named file is absent (packet from a different BBS ID),
	// accept the first .MSG file found so the caller can inspect the first-block
	// BBS ID and decide whether to reject it.
	if msgFile == nil {
		for _, f := range zr.File {
			if strings.HasSuffix(strings.ToUpper(f.Name), ".MSG") {
				msgFile = f
				break
			}
		}
	}
	if msgFile == nil {
		return nil, fmt.Errorf("REP packet contains no .MSG file")
	}

	rc, err := msgFile.Open()
	if err != nil {
		return nil, fmt.Errorf("failed to open %s: %w", msgFile.Name, err)
	}
	defer rc.Close()

	data, err := io.ReadAll(rc)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", msgFile.Name, err)
	}

	msgs, err := parseREPMessages(data)
	if err != nil {
		return nil, err
	}
	return &REPPacket{BBSID: firstBlockID(data), Messages: msgs, Payload: data}, nil
}

// firstBlockID extracts the BBS ID from the first 128-byte block of a REP
// payload: the leading whitespace-delimited token, upper-cased and capped at the
// 8-character QWK BBS-ID length. Returns "" when the block is blank or absent.
func firstBlockID(data []byte) string {
	if len(data) < BlockSize {
		return ""
	}
	block := data[:BlockSize]
	start := 0
	for start < len(block) && block[start] == ' ' {
		start++
	}
	end := start
	for end < len(block) && block[end] != ' ' && end-start < 8 {
		end++
	}
	return strings.ToUpper(string(block[start:end]))
}

// parseREPMessages extracts messages from the raw block data.
func parseREPMessages(data []byte) ([]REPMessage, error) {
	if len(data) < BlockSize {
		return nil, fmt.Errorf("REP data too short (%d bytes)", len(data))
	}

	// Skip the first block (header/spacer)
	pos := BlockSize
	var messages []REPMessage

	for pos+BlockSize <= len(data) {
		header := data[pos : pos+BlockSize]

		// Parse number of blocks from positions 116-121
		blkStr := strings.TrimSpace(string(header[116:122]))
		numBlocks, err := strconv.Atoi(blkStr)
		if err != nil || numBlocks < 1 {
			slog.Warn("invalid QWK REP block count", "value", blkStr, "offset", pos)
			break
		}

		totalBytes := numBlocks * BlockSize
		if pos+totalBytes > len(data) {
			slog.Warn("QWK REP message extends past end of data", "offset", pos)
			break
		}

		// Parse conference from positions 123-124 (little-endian uint16)
		confNum := int(header[123]) | int(header[124])<<8

		// Parse fields
		to := strings.TrimSpace(string(header[21:46]))
		subject := strings.TrimSpace(string(header[71:96]))
		refStr := strings.TrimSpace(string(header[108:116]))
		refNum, _ := strconv.Atoi(refStr) // 0 / unparsable => no parent

		// Extract body (starts after header block)
		bodyBytes := data[pos+BlockSize : pos+totalBytes]
		body := decodeQWKBody(bodyBytes)

		messages = append(messages, REPMessage{
			Conference:    confNum,
			To:            to,
			Subject:       subject,
			Body:          body,
			ReplyToNumber: refNum,
		})

		pos += totalBytes
	}

	slog.Info("parsed QWK REP messages", "count", len(messages))
	return messages, nil
}

// decodeQWKBody converts QWK body bytes (0xE3 line endings) to normal text.
func decodeQWKBody(data []byte) string {
	// Trim trailing spaces
	data = bytes.TrimRight(data, " ")

	// Replace QWK line ending (0xE3) with newline
	var buf strings.Builder
	buf.Grow(len(data))
	for _, b := range data {
		if b == 0xE3 {
			buf.WriteByte('\n')
		} else {
			buf.WriteByte(b)
		}
	}
	return strings.TrimSpace(buf.String())
}
