package jam

import (
	"fmt"
	"io"
)

// Reading and writing a single JAM message header record, including its
// subfields. Header layout is fixed by the JAM specification.

// ReadMessageHeader reads a message header for the given 1-based message number.
func (b *Base) ReadMessageHeader(msgNum int) (*MessageHeader, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.readMessageHeaderLocked(msgNum)
}

func (b *Base) readMessageHeaderLocked(msgNum int) (*MessageHeader, error) {
	if !b.isOpen {
		return nil, ErrBaseNotOpen
	}

	idx, err := b.readIndexRecordLocked(msgNum)
	if err != nil {
		return nil, err
	}

	if _, err := b.jhrFile.Seek(int64(idx.HdrOffset), 0); err != nil {
		return nil, fmt.Errorf("jam: seek failed on .jhr: %w", err)
	}

	hdr := &MessageHeader{}
	if err := readBinaryLE(b.jhrFile, &hdr.Signature, "header signature"); err != nil {
		return nil, err
	}
	if err := readBinaryLE(b.jhrFile, &hdr.Revision, "header revision"); err != nil {
		return nil, err
	}
	if err := readBinaryLE(b.jhrFile, &hdr.ReservedWord, "header reserved word"); err != nil {
		return nil, err
	}
	if err := readBinaryLE(b.jhrFile, &hdr.SubfieldLen, "header subfield length"); err != nil {
		return nil, err
	}
	if err := readBinaryLE(b.jhrFile, &hdr.TimesRead, "header times read"); err != nil {
		return nil, err
	}
	if err := readBinaryLE(b.jhrFile, &hdr.MSGIDcrc, "header MSGID crc"); err != nil {
		return nil, err
	}
	if err := readBinaryLE(b.jhrFile, &hdr.REPLYcrc, "header REPLY crc"); err != nil {
		return nil, err
	}
	if err := readBinaryLE(b.jhrFile, &hdr.ReplyTo, "header reply to"); err != nil {
		return nil, err
	}
	if err := readBinaryLE(b.jhrFile, &hdr.Reply1st, "header reply first"); err != nil {
		return nil, err
	}
	if err := readBinaryLE(b.jhrFile, &hdr.ReplyNext, "header reply next"); err != nil {
		return nil, err
	}
	if err := readBinaryLE(b.jhrFile, &hdr.DateWritten, "header date written"); err != nil {
		return nil, err
	}
	if err := readBinaryLE(b.jhrFile, &hdr.DateReceived, "header date received"); err != nil {
		return nil, err
	}
	if err := readBinaryLE(b.jhrFile, &hdr.DateProcessed, "header date processed"); err != nil {
		return nil, err
	}
	if err := readBinaryLE(b.jhrFile, &hdr.MessageNumber, "header message number"); err != nil {
		return nil, err
	}
	if err := readBinaryLE(b.jhrFile, &hdr.Attribute, "header attribute"); err != nil {
		return nil, err
	}
	if err := readBinaryLE(b.jhrFile, &hdr.Attribute2, "header attribute2"); err != nil {
		return nil, err
	}
	if err := readBinaryLE(b.jhrFile, &hdr.Offset, "header text offset"); err != nil {
		return nil, err
	}
	if err := readBinaryLE(b.jhrFile, &hdr.TxtLen, "header text length"); err != nil {
		return nil, err
	}
	if err := readBinaryLE(b.jhrFile, &hdr.PasswordCRC, "header password crc"); err != nil {
		return nil, err
	}
	if err := readBinaryLE(b.jhrFile, &hdr.Cost, "header cost"); err != nil {
		return nil, err
	}

	if string(hdr.Signature[:]) != Signature {
		return nil, ErrInvalidSignature
	}

	// Read subfields
	bytesRead := uint32(0)
	for bytesRead < hdr.SubfieldLen {
		sf := Subfield{}
		if err := readBinaryLE(b.jhrFile, &sf.LoID, "subfield loID"); err != nil {
			return nil, err
		}
		if err := readBinaryLE(b.jhrFile, &sf.HiID, "subfield hiID"); err != nil {
			return nil, err
		}
		if err := readBinaryLE(b.jhrFile, &sf.DatLen, "subfield data length"); err != nil {
			return nil, err
		}
		sf.Buffer = make([]byte, sf.DatLen)
		if _, err := io.ReadFull(b.jhrFile, sf.Buffer); err != nil {
			return nil, fmt.Errorf("jam: read subfield buffer: %w", err)
		}
		hdr.Subfields = append(hdr.Subfields, sf)
		bytesRead += SubfieldHdrSize + sf.DatLen
	}

	return hdr, nil
}

// writeMessageHeader writes a message header to the .jhr file and returns
// the byte offset where it was written.
func (b *Base) writeMessageHeader(hdr *MessageHeader) (uint32, error) {
	if !b.isOpen {
		return 0, ErrBaseNotOpen
	}

	pos, err := b.jhrFile.Seek(0, io.SeekEnd)
	if err != nil {
		return 0, fmt.Errorf("jam: seek failed on .jhr: %w", err)
	}
	if pos == 0 {
		pos = HeaderSize
		if _, err := b.jhrFile.Seek(pos, 0); err != nil {
			return 0, fmt.Errorf("jam: seek failed on .jhr: %w", err)
		}
	}

	if err := writeBinaryLE(b.jhrFile, hdr.Signature, "header signature"); err != nil {
		return 0, err
	}
	if err := writeBinaryLE(b.jhrFile, hdr.Revision, "header revision"); err != nil {
		return 0, err
	}
	if err := writeBinaryLE(b.jhrFile, hdr.ReservedWord, "header reserved word"); err != nil {
		return 0, err
	}
	if err := writeBinaryLE(b.jhrFile, hdr.SubfieldLen, "header subfield length"); err != nil {
		return 0, err
	}
	if err := writeBinaryLE(b.jhrFile, hdr.TimesRead, "header times read"); err != nil {
		return 0, err
	}
	if err := writeBinaryLE(b.jhrFile, hdr.MSGIDcrc, "header MSGID crc"); err != nil {
		return 0, err
	}
	if err := writeBinaryLE(b.jhrFile, hdr.REPLYcrc, "header REPLY crc"); err != nil {
		return 0, err
	}
	if err := writeBinaryLE(b.jhrFile, hdr.ReplyTo, "header reply to"); err != nil {
		return 0, err
	}
	if err := writeBinaryLE(b.jhrFile, hdr.Reply1st, "header reply first"); err != nil {
		return 0, err
	}
	if err := writeBinaryLE(b.jhrFile, hdr.ReplyNext, "header reply next"); err != nil {
		return 0, err
	}
	if err := writeBinaryLE(b.jhrFile, hdr.DateWritten, "header date written"); err != nil {
		return 0, err
	}
	if err := writeBinaryLE(b.jhrFile, hdr.DateReceived, "header date received"); err != nil {
		return 0, err
	}
	if err := writeBinaryLE(b.jhrFile, hdr.DateProcessed, "header date processed"); err != nil {
		return 0, err
	}
	if err := writeBinaryLE(b.jhrFile, hdr.MessageNumber, "header message number"); err != nil {
		return 0, err
	}
	if err := writeBinaryLE(b.jhrFile, hdr.Attribute, "header attribute"); err != nil {
		return 0, err
	}
	if err := writeBinaryLE(b.jhrFile, hdr.Attribute2, "header attribute2"); err != nil {
		return 0, err
	}
	if err := writeBinaryLE(b.jhrFile, hdr.Offset, "header text offset"); err != nil {
		return 0, err
	}
	if err := writeBinaryLE(b.jhrFile, hdr.TxtLen, "header text length"); err != nil {
		return 0, err
	}
	if err := writeBinaryLE(b.jhrFile, hdr.PasswordCRC, "header password crc"); err != nil {
		return 0, err
	}
	if err := writeBinaryLE(b.jhrFile, hdr.Cost, "header cost"); err != nil {
		return 0, err
	}

	for _, sf := range hdr.Subfields {
		if err := writeBinaryLE(b.jhrFile, sf.LoID, "subfield loID"); err != nil {
			return 0, err
		}
		if err := writeBinaryLE(b.jhrFile, sf.HiID, "subfield hiID"); err != nil {
			return 0, err
		}
		if err := writeBinaryLE(b.jhrFile, sf.DatLen, "subfield data length"); err != nil {
			return 0, err
		}
		if err := writeAll(b.jhrFile, sf.Buffer, "subfield buffer"); err != nil {
			return 0, err
		}
	}

	return uint32(pos), nil
}
