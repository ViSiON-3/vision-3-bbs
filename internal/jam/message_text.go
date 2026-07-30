package jam

import (
	"fmt"
	"io"
	"strings"
)

// Reading and appending message text in the .jdt data file.

// ReadMessageText reads the raw message text (CP437) for the given header.
func (b *Base) ReadMessageText(hdr *MessageHeader) (string, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.readMessageTextLocked(hdr)
}

func (b *Base) readMessageTextLocked(hdr *MessageHeader) (string, error) {
	if !b.isOpen {
		return "", ErrBaseNotOpen
	}
	if hdr.TxtLen == 0 {
		return "", nil
	}
	if _, err := b.jdtFile.Seek(int64(hdr.Offset), 0); err != nil {
		return "", fmt.Errorf("jam: seek failed on .jdt: %w", err)
	}
	buf := make([]byte, hdr.TxtLen)
	if _, err := io.ReadFull(b.jdtFile, buf); err != nil {
		return "", fmt.Errorf("jam: failed to read text: %w", err)
	}
	return string(buf), nil
}

// writeMessageText appends text to the .jdt file. LF is converted to CR
// per the JAM specification. Returns the offset and byte length written.
func (b *Base) writeMessageText(text string) (uint32, uint32, error) {
	if !b.isOpen {
		return 0, 0, ErrBaseNotOpen
	}
	text = strings.ReplaceAll(text, "\r\n", "\r") // Handle Windows line endings first
	text = strings.ReplaceAll(text, "\n", "\r")   // Then convert remaining Unix LF to CR
	pos, err := b.jdtFile.Seek(0, io.SeekEnd)
	if err != nil {
		return 0, 0, fmt.Errorf("jam: seek failed on .jdt: %w", err)
	}
	buf := []byte(text)
	if _, err := b.jdtFile.Write(buf); err != nil {
		return 0, 0, fmt.Errorf("jam: failed to write text: %w", err)
	}
	return uint32(pos), uint32(len(buf)), nil
}
