package jam

import (
	"fmt"
	"os"
	"strings"
)

// SetReceiverName readdresses an existing message: it replaces the message's
// receiver-name (To) subfield with to and updates the To CRC in its .jdx
// index record, so CountMessagesToUser and anything else that matches the
// index sees the new recipient. Text and every other field are unchanged.
//
// A new name rarely has the old one's length, so the header cannot be
// rewritten in place. Instead the updated header is appended to the .jhr (as
// for a new message), the index record is pointed at it,
// and the old copy is marked deleted (with no text) so no reader or packer
// treats it as a message; Pack drops it. The ModCounter is bumped so cached
// views of the base reload.
func (b *Base) SetReceiverName(msgNum int, to string) error {
	return b.withFileLock(func() error {
		b.mu.Lock()
		defer b.mu.Unlock()

		if !b.isOpen {
			return ErrBaseNotOpen
		}
		if err := b.readFixedHeader(); err != nil {
			return err
		}
		idx, err := b.readIndexRecordLocked(msgNum)
		if err != nil {
			return err
		}
		old, err := b.readMessageHeaderLocked(msgNum)
		if err != nil {
			return err
		}

		updated := *old
		updated.Subfields = make([]Subfield, 0, len(old.Subfields)+1)
		replaced := false
		for _, sf := range old.Subfields {
			if sf.LoID == SfldReceiverName {
				if replaced {
					continue // a header carries one receiver; drop extras
				}
				sf = CreateSubfield(SfldReceiverName, to)
				replaced = true
			}
			updated.Subfields = append(updated.Subfields, sf)
		}
		if !replaced {
			updated.Subfields = append(updated.Subfields, CreateSubfield(SfldReceiverName, to))
		}
		updated.SubfieldLen = 0
		for _, sf := range updated.Subfields {
			updated.SubfieldLen += SubfieldHdrSize + sf.DatLen
		}

		// Append the new header and repoint the index before retiring the
		// old copy: a crash in between leaves an unreferenced header, never
		// an index entry pointing at a deleted one.
		newOffset, err := b.writeMessageHeader(&updated)
		if err != nil {
			return err
		}
		if err := b.jhrFile.Sync(); err != nil {
			return fmt.Errorf("jam: sync .jhr: %w", err)
		}
		if err := b.writeIndexRecord(msgNum, &IndexRecord{
			ToCRC:     CRC32String(strings.ToLower(to)), // as WriteMessage computes it
			HdrOffset: newOffset,
		}); err != nil {
			return err
		}
		if err := b.jdxFile.Sync(); err != nil {
			return fmt.Errorf("jam: sync .jdx: %w", err)
		}

		retired := *old
		retired.Attribute |= MsgDeleted
		retired.TxtLen = 0
		if err := b.writeFixedHeaderFieldsAt(int64(idx.HdrOffset), &retired); err != nil {
			return err
		}

		b.fixedHeader.ModCounter++
		if err := b.writeFixedHeader(); err != nil {
			return err
		}
		for _, f := range []*os.File{b.jhrFile, b.jdxFile} {
			if err := f.Sync(); err != nil {
				return fmt.Errorf("jam: sync %s: %w", f.Name(), err)
			}
		}
		return nil
	})
}

// writeFixedHeaderFieldsAt overwrites the fixed-size part of the header
// stored at offset in the .jhr with hdr's fields, leaving its subfields as
// they are. Caller must hold both the file lock and b.mu.
func (b *Base) writeFixedHeaderFieldsAt(offset int64, hdr *MessageHeader) error {
	if _, err := b.jhrFile.Seek(offset, 0); err != nil {
		return fmt.Errorf("jam: seek failed on .jhr: %w", err)
	}
	for _, field := range []interface{}{
		hdr.Signature, hdr.Revision, hdr.ReservedWord, hdr.SubfieldLen,
		hdr.TimesRead, hdr.MSGIDcrc, hdr.REPLYcrc, hdr.ReplyTo,
		hdr.Reply1st, hdr.ReplyNext, hdr.DateWritten, hdr.DateReceived,
		hdr.DateProcessed, hdr.MessageNumber, hdr.Attribute, hdr.Attribute2,
		hdr.Offset, hdr.TxtLen, hdr.PasswordCRC, hdr.Cost,
	} {
		if err := writeBinaryLE(b.jhrFile, field, "header field"); err != nil {
			return err
		}
	}
	return nil
}
