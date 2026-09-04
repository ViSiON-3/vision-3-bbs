package jam

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// Whole-message operations: the read/write/delete/scan entry points that
// combine a header, its subfields and its text.

// ReadMessage reads a complete message (header + subfields + text) for
// the given 1-based message number.
func (b *Base) ReadMessage(msgNum int) (*Message, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	hdr, err := b.readMessageHeaderLocked(msgNum)
	if err != nil {
		return nil, err
	}
	text, err := b.readMessageTextLocked(hdr)
	if err != nil {
		return nil, err
	}

	msg := &Message{
		Header:   hdr,
		Text:     text,
		DateTime: time.Unix(int64(hdr.DateWritten), 0),
	}

	for _, sf := range hdr.Subfields {
		val := string(sf.Buffer)
		switch sf.LoID {
		case SfldOAddress:
			msg.OrigAddr = val
		case SfldDAddress:
			msg.DestAddr = val
		case SfldSenderName:
			msg.From = val
		case SfldReceiverName:
			msg.To = val
		case SfldMsgID:
			msg.MsgID = val
		case SfldReplyID:
			msg.ReplyID = val
		case SfldSubject:
			msg.Subject = val
		case SfldPID:
			msg.PID = val
		case SfldFTSKludge:
			msg.Kludges = append(msg.Kludges, val)
		case SfldSeenBy2D:
			msg.SeenBy = val
		case SfldPath2D:
			msg.Path = val
		case SfldFlags:
			msg.Flags = val
		}
	}

	// If echomail/netmail but OrigAddr missing, try to extract from origin line
	isEcho := (hdr.Attribute & MsgTypeEcho) != 0
	isNet := (hdr.Attribute & MsgTypeNet) != 0
	if (isEcho || isNet) && msg.OrigAddr == "" {
		msg.OrigAddr = ExtractOriginAddress(text)
	}

	return msg, nil
}

// WriteMessage writes a complete local message to the base.
// Returns the 1-based message number assigned to the new message.
func (b *Base) WriteMessage(msg *Message) (int, error) {
	var msgNum int
	err := b.withFileLock(func() error {
		b.mu.Lock()
		defer b.mu.Unlock()

		if !b.isOpen {
			return ErrBaseNotOpen
		}
		if err := b.readFixedHeader(); err != nil {
			return err
		}

		hdr := &MessageHeader{
			Revision:      1,
			DateWritten:   uint32(msg.DateTime.Unix()),
			DateProcessed: uint32(time.Now().Unix()),
			Attribute:     msg.GetAttribute(),
		}
		copy(hdr.Signature[:], Signature)
		hdr.ReplyTo = msg.ReplyTo

		hdr.Subfields = buildSubfields(msg)

		// Set CRC fields for MSGID/REPLY linking (matches WriteMessageExt behaviour)
		if msg.MsgID != "" {
			hdr.MSGIDcrc = CRC32String(msg.MsgID)
		}
		if msg.ReplyID != "" {
			hdr.REPLYcrc = CRC32String(msg.ReplyID)
		}

		// Calculate total subfield length
		hdr.SubfieldLen = 0
		for _, sf := range hdr.Subfields {
			hdr.SubfieldLen += SubfieldHdrSize + sf.DatLen
		}

		offset, txtLen, err := b.writeMessageText(msg.Text)
		if err != nil {
			return err
		}
		hdr.Offset = offset
		hdr.TxtLen = txtLen

		count, err := b.getMessageCountLocked()
		if err != nil {
			return err
		}
		msgNum = count + 1
		hdr.MessageNumber = uint32(msgNum) + b.fixedHeader.BaseMsgNum - 1

		hdrOffset, err := b.writeMessageHeader(hdr)
		if err != nil {
			return err
		}

		idx := &IndexRecord{
			ToCRC:     CRC32String(strings.ToLower(msg.To)),
			HdrOffset: hdrOffset,
		}
		if err := b.writeIndexRecord(msgNum, idx); err != nil {
			return err
		}

		b.fixedHeader.ActiveMsgs++
		b.fixedHeader.ModCounter++
		if err := b.writeFixedHeader(); err != nil {
			return err
		}

		// Sync all files to ensure consistency on crash. Order matters:
		// message data (.jdt) must reach disk before the header (.jhr) and
		// index (.jdx) records that reference it.
		for _, sf := range []struct {
			name string
			file *os.File
		}{
			{".jdt", b.jdtFile},
			{".jhr", b.jhrFile},
			{".jdx", b.jdxFile},
		} {
			if err := sf.file.Sync(); err != nil {
				return fmt.Errorf("jam: sync %s: %w", sf.name, err)
			}
		}

		return nil
	})
	return msgNum, err
}

// DeleteMessage marks a message as deleted and zeroes its text length.
func (b *Base) DeleteMessage(msgNum int) error {
	return b.withFileLock(func() error {
		b.mu.Lock()
		defer b.mu.Unlock()

		if !b.isOpen {
			return ErrBaseNotOpen
		}

		hdr, err := b.readMessageHeaderLocked(msgNum)
		if err != nil {
			return err
		}

		hdr.Attribute |= MsgDeleted
		hdr.TxtLen = 0

		idx, err := b.readIndexRecordLocked(msgNum)
		if err != nil {
			return err
		}

		// Rewrite header at original offset
		if _, err := b.jhrFile.Seek(int64(idx.HdrOffset), 0); err != nil {
			return fmt.Errorf("jam: seek header for delete: %w", err)
		}
		for _, field := range []interface{}{
			hdr.Signature, hdr.Revision, hdr.ReservedWord, hdr.SubfieldLen,
			hdr.TimesRead, hdr.MSGIDcrc, hdr.REPLYcrc, hdr.ReplyTo,
			hdr.Reply1st, hdr.ReplyNext, hdr.DateWritten, hdr.DateReceived,
			hdr.DateProcessed, hdr.MessageNumber, hdr.Attribute, hdr.Attribute2,
			hdr.Offset, hdr.TxtLen, hdr.PasswordCRC, hdr.Cost,
		} {
			if err := writeBinaryLE(b.jhrFile, field, "deleted message header field"); err != nil {
				return err
			}
		}

		b.fixedHeader.ActiveMsgs--
		b.fixedHeader.ModCounter++
		return b.writeFixedHeader()
	})
}

// ScanMessages reads up to maxMessages starting from startMsg (1-based),
// skipping deleted messages. If maxMessages is 0, reads all.
func (b *Base) ScanMessages(startMsg, maxMessages int) ([]*Message, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if !b.isOpen {
		return nil, ErrBaseNotOpen
	}
	count, err := b.getMessageCountLocked()
	if err != nil {
		return nil, err
	}
	if startMsg < 1 {
		startMsg = 1
	}

	var messages []*Message
	var scanErr error
	read := 0
	for n := startMsg; n <= count && (maxMessages == 0 || read < maxMessages); n++ {
		hdr, err := b.readMessageHeaderLocked(n)
		if err != nil {
			if err == ErrNotFound {
				continue // deleted index entry, skip
			}
			scanErr = fmt.Errorf("jam: scan error at message %d: %w", n, err)
			continue
		}
		if hdr.Attribute&MsgDeleted != 0 {
			continue
		}
		text, err := b.readMessageTextLocked(hdr)
		if err != nil {
			scanErr = fmt.Errorf("jam: scan text error at message %d: %w", n, err)
			continue
		}
		msg := &Message{
			Header:   hdr,
			Text:     text,
			DateTime: time.Unix(int64(hdr.DateWritten), 0),
		}
		for _, sf := range hdr.Subfields {
			val := string(sf.Buffer)
			switch sf.LoID {
			case SfldSenderName:
				msg.From = val
			case SfldReceiverName:
				msg.To = val
			case SfldSubject:
				msg.Subject = val
			case SfldMsgID:
				msg.MsgID = val
			case SfldReplyID:
				msg.ReplyID = val
			case SfldOAddress:
				msg.OrigAddr = val
			}
		}
		messages = append(messages, msg)
		read++
	}
	return messages, scanErr
}
