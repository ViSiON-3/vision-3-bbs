package jam

import "fmt"

// In-place header updates. Rewriting a header keeps the record at its existing
// offset, so the index and text records stay valid.

// UpdateMessageHeader rewrites an existing message header in place.
// This is used by the tosser to update DateProcessed after export.
func (b *Base) UpdateMessageHeader(msgNum int, hdr *MessageHeader) error {
	return b.withFileLock(func() error {
		b.mu.Lock()
		defer b.mu.Unlock()
		return b.updateMessageHeaderLocked(msgNum, hdr)
	})
}

// updateMessageHeaderLocked is the lock-free internal implementation.
// Caller must hold both the file lock and b.mu.
func (b *Base) updateMessageHeaderLocked(msgNum int, hdr *MessageHeader) error {
	if !b.isOpen {
		return ErrBaseNotOpen
	}

	idx, err := b.readIndexRecordLocked(msgNum)
	if err != nil {
		return err
	}

	if _, err := b.jhrFile.Seek(int64(idx.HdrOffset), 0); err != nil {
		return fmt.Errorf("jam: seek failed on .jhr: %w", err)
	}

	if err := writeBinaryLE(b.jhrFile, hdr.Signature, "header signature"); err != nil {
		return err
	}
	if err := writeBinaryLE(b.jhrFile, hdr.Revision, "header revision"); err != nil {
		return err
	}
	if err := writeBinaryLE(b.jhrFile, hdr.ReservedWord, "header reserved word"); err != nil {
		return err
	}
	if err := writeBinaryLE(b.jhrFile, hdr.SubfieldLen, "header subfield length"); err != nil {
		return err
	}
	if err := writeBinaryLE(b.jhrFile, hdr.TimesRead, "header times read"); err != nil {
		return err
	}
	if err := writeBinaryLE(b.jhrFile, hdr.MSGIDcrc, "header MSGID crc"); err != nil {
		return err
	}
	if err := writeBinaryLE(b.jhrFile, hdr.REPLYcrc, "header REPLY crc"); err != nil {
		return err
	}
	if err := writeBinaryLE(b.jhrFile, hdr.ReplyTo, "header reply to"); err != nil {
		return err
	}
	if err := writeBinaryLE(b.jhrFile, hdr.Reply1st, "header reply first"); err != nil {
		return err
	}
	if err := writeBinaryLE(b.jhrFile, hdr.ReplyNext, "header reply next"); err != nil {
		return err
	}
	if err := writeBinaryLE(b.jhrFile, hdr.DateWritten, "header date written"); err != nil {
		return err
	}
	if err := writeBinaryLE(b.jhrFile, hdr.DateReceived, "header date received"); err != nil {
		return err
	}
	if err := writeBinaryLE(b.jhrFile, hdr.DateProcessed, "header date processed"); err != nil {
		return err
	}
	if err := writeBinaryLE(b.jhrFile, hdr.MessageNumber, "header message number"); err != nil {
		return err
	}
	if err := writeBinaryLE(b.jhrFile, hdr.Attribute, "header attribute"); err != nil {
		return err
	}
	if err := writeBinaryLE(b.jhrFile, hdr.Attribute2, "header attribute2"); err != nil {
		return err
	}
	if err := writeBinaryLE(b.jhrFile, hdr.Offset, "header text offset"); err != nil {
		return err
	}
	if err := writeBinaryLE(b.jhrFile, hdr.TxtLen, "header text length"); err != nil {
		return err
	}
	if err := writeBinaryLE(b.jhrFile, hdr.PasswordCRC, "header password crc"); err != nil {
		return err
	}
	if err := writeBinaryLE(b.jhrFile, hdr.Cost, "header cost"); err != nil {
		return err
	}

	// Note: subfields are not rewritten since they don't change
	// and follow immediately after the fixed header portion.

	return nil
}
