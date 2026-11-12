package jam

import (
	"errors"
	"fmt"
	"os"
)

// ReplaceMessageIf replaces the subject and text of message msgNum. edit is
// given the message as it is at that moment, read under the base's file
// lock through files opened once the lock is held (as DeleteMessageIf does),
// and returns the new subject and text, or ok false to leave it alone. A
// message number past the end, a deleted message, or ok false gives
// ErrMessageChanged, and nothing is written.
//
// A message's text can't grow in place and its header can't either once the
// subject changes length, so both are written anew: the text at the end of
// the .jdt, the header at the end of the .jhr with every other field and
// subfield kept, and the index then points to the new header. The old header
// is marked deleted, so a tool that walks the .jhr instead of the index skips
// it; it and the old text stay as slack until the base is packed. The
// message keeps its number, links and lastread position.
func (b *Base) ReplaceMessageIf(msgNum int, edit func(*Message) (subject, text string, ok bool)) error {
	return b.withFileLock(func() error {
		base := b
		if b.BasePath != "" {
			fresh, err := Open(b.BasePath)
			if err != nil {
				return err
			}
			defer func() { _ = fresh.Close() }()
			base = fresh
		}
		base.mu.Lock()
		defer base.mu.Unlock()
		if !base.isOpen {
			return ErrBaseNotOpen
		}

		hdr, err := base.readMessageHeaderLocked(msgNum)
		if errors.Is(err, ErrInvalidMessage) {
			return ErrMessageChanged
		}
		if err != nil {
			return err
		}
		if hdr.Attribute&MsgDeleted != 0 {
			return ErrMessageChanged
		}
		oldText, err := base.readMessageTextLocked(hdr)
		if err != nil {
			return err
		}
		subject, text, ok := edit(MessageFrom(hdr, oldText))
		if !ok {
			return ErrMessageChanged
		}
		return base.replaceMessageLocked(msgNum, hdr, subject, text)
	})
}

// replaceMessageLocked writes msgNum's new subject and text as
// ReplaceMessageIf describes. hdr is its current header. Callers hold the
// file lock and b.mu.
func (b *Base) replaceMessageLocked(msgNum int, hdr *MessageHeader, subject, text string) error {
	idx, err := b.readIndexRecordLocked(msgNum)
	if err != nil {
		return err
	}
	oldHdrOffset := idx.HdrOffset

	newHdr := *hdr
	newHdr.Subfields = withSubject(hdr.Subfields, subject)
	newHdr.SubfieldLen = 0
	for _, sf := range newHdr.Subfields {
		newHdr.SubfieldLen += SubfieldHdrSize + sf.DatLen
	}
	offset, txtLen, err := b.writeMessageText(text)
	if err != nil {
		return err
	}
	newHdr.Offset = offset
	newHdr.TxtLen = txtLen
	hdrOffset, err := b.writeMessageHeader(&newHdr)
	if err != nil {
		return err
	}

	// The new text and header reach the disk before the index points at
	// them, and the old header is retired only after that.
	for _, f := range []*os.File{b.jdtFile, b.jhrFile} {
		if err := f.Sync(); err != nil {
			return fmt.Errorf("jam: sync %s: %w", f.Name(), err)
		}
	}
	idx.HdrOffset = hdrOffset
	if err := b.writeIndexRecord(msgNum, idx); err != nil {
		return err
	}
	if err := b.jdxFile.Sync(); err != nil {
		return fmt.Errorf("jam: sync %s: %w", b.jdxFile.Name(), err)
	}

	old := *hdr
	old.Attribute |= MsgDeleted
	if err := b.writeFixedHeaderFieldsAt(int64(oldHdrOffset), &old); err != nil {
		return err
	}
	if err := b.readFixedHeader(); err != nil {
		return err
	}
	b.fixedHeader.ModCounter++
	if err := b.writeFixedHeader(); err != nil {
		return err
	}
	if err := b.jhrFile.Sync(); err != nil {
		return fmt.Errorf("jam: sync %s: %w", b.jhrFile.Name(), err)
	}
	return nil
}

// withSubject is subfields with the subject set to subject: the existing
// subject subfield replaced where it was, or one added, or removed when
// subject is empty.
func withSubject(subfields []Subfield, subject string) []Subfield {
	out := make([]Subfield, 0, len(subfields)+1)
	done := false
	for _, sf := range subfields {
		if sf.LoID == SfldSubject {
			if !done && subject != "" {
				out = append(out, CreateSubfield(SfldSubject, subject))
			}
			done = true
			continue
		}
		out = append(out, sf)
	}
	if !done && subject != "" {
		out = append(out, CreateSubfield(SfldSubject, subject))
	}
	return out
}
