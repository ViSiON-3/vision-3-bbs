package jam

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"sort"
	"strings"
)

// GetLastRead returns the lastread record for the given username.
// Returns ErrNotFound if the user has no record in this base.
func (b *Base) GetLastRead(username string) (*LastReadRecord, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.getLastReadLocked(username)
}

func (b *Base) getLastReadLocked(username string) (*LastReadRecord, error) {
	if !b.isOpen {
		return nil, ErrBaseNotOpen
	}

	userCRC := CRC32String(strings.ToLower(username))

	info, err := b.jlrFile.Stat()
	if err != nil {
		return nil, fmt.Errorf("jam: failed to stat .jlr: %w", err)
	}
	if info.Size()%LastReadSize != 0 {
		return nil, fmt.Errorf("jam: invalid .jlr size %d (not aligned to record size %d)", info.Size(), LastReadSize)
	}
	recordCount := info.Size() / LastReadSize

	for i := int64(0); i < recordCount; i++ {
		offset := i * LastReadSize
		buf := make([]byte, LastReadSize)
		n, err := b.jlrFile.ReadAt(buf, offset)
		if err != nil && err != io.EOF {
			return nil, fmt.Errorf("jam: read failed in .jlr: %w", err)
		}
		if n != int(LastReadSize) {
			return nil, fmt.Errorf("jam: short read in .jlr: got %d bytes", n)
		}
		reader := bytes.NewReader(buf)

		lr := &LastReadRecord{}
		if err := binary.Read(reader, binary.LittleEndian, &lr.UserCRC); err != nil {
			return nil, fmt.Errorf("jam: read failed in .jlr: %w", err)
		}
		if err := binary.Read(reader, binary.LittleEndian, &lr.UserID); err != nil {
			return nil, fmt.Errorf("jam: read failed in .jlr: %w", err)
		}
		if err := binary.Read(reader, binary.LittleEndian, &lr.LastReadMsg); err != nil {
			return nil, fmt.Errorf("jam: read failed in .jlr: %w", err)
		}
		if err := binary.Read(reader, binary.LittleEndian, &lr.HighReadMsg); err != nil {
			return nil, fmt.Errorf("jam: read failed in .jlr: %w", err)
		}

		if lr.UserCRC == userCRC {
			return lr, nil
		}
	}
	return nil, ErrNotFound
}

// SetLastRead updates or creates a lastread record for the given username.
func (b *Base) SetLastRead(username string, lastRead, highRead uint32) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.setLastReadLocked(username, lastRead, highRead)
}

// GetNextUnreadMessage returns the next unread message number for the user.
// Returns ErrNotFound if there are no unread messages.
func (b *Base) GetNextUnreadMessage(username string) (int, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if !b.isOpen {
		return 0, ErrBaseNotOpen
	}

	lr, err := b.getLastReadLocked(username)
	if err != nil {
		if err == ErrNotFound {
			count, cerr := b.getMessageCountLocked()
			if cerr != nil {
				return 0, cerr
			}
			if count > 0 {
				return 1, nil
			}
			return 0, ErrNotFound
		}
		return 0, err
	}

	nextMsg := int(lr.LastReadMsg) + 1
	count, err := b.getMessageCountLocked()
	if err != nil {
		return 0, err
	}
	if nextMsg <= count {
		return nextMsg, nil
	}
	return 0, ErrNotFound
}

// MarkMessageRead updates the lastread pointer after reading a message.
func (b *Base) MarkMessageRead(username string, msgNum int) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if !b.isOpen {
		return ErrBaseNotOpen
	}

	lr, err := b.getLastReadLocked(username)
	if err != nil {
		if err == ErrNotFound {
			// Temporarily release/re-acquire not needed since we hold write lock
			return b.setLastReadLocked(username, uint32(msgNum), uint32(msgNum))
		}
		return err
	}

	newLast := uint32(msgNum)
	newHigh := lr.HighReadMsg
	if newLast > newHigh {
		newHigh = newLast
	}
	return b.setLastReadLocked(username, newLast, newHigh)
}

// setLastReadLocked is the non-locking version of SetLastRead, for use
// when the caller already holds the write lock.
func (b *Base) setLastReadLocked(username string, lastRead, highRead uint32) error {
	if !b.isOpen {
		return ErrBaseNotOpen
	}

	userCRC := CRC32String(strings.ToLower(username))
	var userID uint32 // JAM spec: numeric user record number (0 if unknown)

	info, err := b.jlrFile.Stat()
	if err != nil {
		return fmt.Errorf("jam: failed to stat .jlr: %w", err)
	}
	recordCount := info.Size() / LastReadSize

	for i := int64(0); i < recordCount; i++ {
		pos := i * LastReadSize
		if _, err := b.jlrFile.Seek(pos, 0); err != nil {
			return fmt.Errorf("jam: seek failed in .jlr: %w", err)
		}

		var readCRC uint32
		if err := binary.Read(b.jlrFile, binary.LittleEndian, &readCRC); err != nil {
			return fmt.Errorf("jam: read failed in .jlr: %w", err)
		}

		if readCRC == userCRC {
			if _, err := b.jlrFile.Seek(pos, 0); err != nil {
				return fmt.Errorf("jam: seek failed in .jlr: %w", err)
			}
			if err := binary.Write(b.jlrFile, binary.LittleEndian, userCRC); err != nil {
				return fmt.Errorf("jam: write failed in .jlr: %w", err)
			}
			if err := binary.Write(b.jlrFile, binary.LittleEndian, userID); err != nil {
				return fmt.Errorf("jam: write failed in .jlr: %w", err)
			}
			if err := binary.Write(b.jlrFile, binary.LittleEndian, lastRead); err != nil {
				return fmt.Errorf("jam: write failed in .jlr: %w", err)
			}
			if err := binary.Write(b.jlrFile, binary.LittleEndian, highRead); err != nil {
				return fmt.Errorf("jam: write failed in .jlr: %w", err)
			}
			return nil
		}
	}

	if _, err := b.jlrFile.Seek(0, io.SeekEnd); err != nil {
		return fmt.Errorf("jam: seek failed in .jlr: %w", err)
	}
	if err := binary.Write(b.jlrFile, binary.LittleEndian, userCRC); err != nil {
		return fmt.Errorf("jam: write failed in .jlr: %w", err)
	}
	if err := binary.Write(b.jlrFile, binary.LittleEndian, userID); err != nil {
		return fmt.Errorf("jam: write failed in .jlr: %w", err)
	}
	if err := binary.Write(b.jlrFile, binary.LittleEndian, lastRead); err != nil {
		return fmt.Errorf("jam: write failed in .jlr: %w", err)
	}
	if err := binary.Write(b.jlrFile, binary.LittleEndian, highRead); err != nil {
		return fmt.Errorf("jam: write failed in .jlr: %w", err)
	}
	return nil
}

// GetUnreadCount returns the number of unread messages for the user.
func (b *Base) GetUnreadCount(username string) (int, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if !b.isOpen {
		return 0, ErrBaseNotOpen
	}

	count, err := b.getMessageCountLocked()
	if err != nil {
		return 0, err
	}
	if count == 0 {
		return 0, nil
	}

	lr, err := b.getLastReadLocked(username)
	if err != nil {
		if err == ErrNotFound {
			return count, nil
		}
		return 0, err
	}

	unread := count - int(lr.LastReadMsg)
	if unread < 0 {
		unread = 0
	}
	return unread, nil
}

// remapLastReadLocked rewrites every lastread pointer from the pre-pack
// numbering to the post-pack numbering. survivors holds the old message numbers
// that were kept, in ascending order.
//
// Packing renumbers surviving messages from 1, so a pointer left alone silently
// changes meaning: after messages are removed, "I have read up to #3" starts
// describing a message the user has never seen — or, once the base is smaller
// than the pointer, every message in it. The login mail scan counts from
// lastread+1, so a stranded pointer reports no new mail no matter what arrives,
// and new private mail becomes invisible.
//
// A pointer at old message N becomes the number of survivors at or below N: the
// user has still read everything up to that point, whatever it is now numbered.
// Pointers past the end of the old base (already stale) collapse to the new
// message count, and a base packed empty leaves every pointer at 0, which is
// correct — there is nothing left to have read.
//
// The caller must hold b.mu and have the base open on the packed files.
func (b *Base) remapLastReadLocked(survivors []int) error {
	if !b.isOpen {
		return ErrBaseNotOpen
	}

	info, err := b.jlrFile.Stat()
	if err != nil {
		return fmt.Errorf("jam: failed to stat .jlr: %w", err)
	}
	if info.Size()%LastReadSize != 0 {
		return fmt.Errorf("jam: invalid .jlr size %d (not aligned to record size %d)", info.Size(), LastReadSize)
	}
	recordCount := info.Size() / LastReadSize

	// remap counts the survivors at or below an old message number. survivors is
	// ascending, so that is where old+1 would be inserted.
	remap := func(old uint32) uint32 {
		return uint32(sort.SearchInts(survivors, int(old)+1))
	}

	for i := int64(0); i < recordCount; i++ {
		pos := i * LastReadSize
		buf := make([]byte, LastReadSize)
		if _, err := b.jlrFile.ReadAt(buf, pos); err != nil && err != io.EOF {
			return fmt.Errorf("jam: read failed in .jlr: %w", err)
		}
		lastRead := binary.LittleEndian.Uint32(buf[8:12])
		highRead := binary.LittleEndian.Uint32(buf[12:16])

		newLastRead, newHighRead := remap(lastRead), remap(highRead)
		if newLastRead == lastRead && newHighRead == highRead {
			continue
		}
		binary.LittleEndian.PutUint32(buf[8:12], newLastRead)
		binary.LittleEndian.PutUint32(buf[12:16], newHighRead)
		if _, err := b.jlrFile.WriteAt(buf, pos); err != nil {
			return fmt.Errorf("jam: write failed in .jlr: %w", err)
		}
	}
	return nil
}
