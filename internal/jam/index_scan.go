package jam

import (
	"encoding/binary"
	"fmt"
	"io"
)

// indexScanChunk is the number of index records read per .jdx read call.
// A variable rather than a constant so tests can shrink it to exercise the
// multi-batch path without writing thousands of messages.
var indexScanChunk = 1024

// CountMessagesToUser returns the number of messages in the base addressed to
// username. Matching uses the ToCRC stored in each .jdx index record, so the
// scan reads 8 bytes per message instead of a full header.
func (b *Base) CountMessagesToUser(username string) (int, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if !b.isOpen {
		return 0, ErrBaseNotOpen
	}
	if username == "" {
		return 0, nil
	}

	total, err := b.getMessageCountLocked()
	if err != nil {
		return 0, err
	}
	if total == 0 {
		return 0, nil
	}

	wantCRC := CRC32String(username)
	buf := make([]byte, indexScanChunk*IndexRecordSize)
	count := 0

	for scanned := 0; scanned < total; {
		batch := total - scanned
		if batch > indexScanChunk {
			batch = indexScanChunk
		}
		size := batch * IndexRecordSize
		n, err := b.jdxFile.ReadAt(buf[:size], int64(scanned*IndexRecordSize))
		// ReadAt reports EOF when the read ends exactly at the file end.
		if err != nil && (err != io.EOF || n != size) {
			return 0, fmt.Errorf("jam: failed to read index records at %d: %w", scanned, err)
		}
		if n != size {
			return 0, fmt.Errorf("jam: short read for index records at %d: got %d of %d bytes", scanned, n, size)
		}
		for off := 0; off < size; off += IndexRecordSize {
			toCRC := binary.LittleEndian.Uint32(buf[off : off+4])
			hdrOffset := binary.LittleEndian.Uint32(buf[off+4 : off+8])
			// 0xFFFFFFFF in both fields marks a free (never written) slot.
			if toCRC == 0xFFFFFFFF && hdrOffset == 0xFFFFFFFF {
				continue
			}
			if toCRC == wantCRC {
				count++
			}
		}
		scanned += batch
	}

	return count, nil
}
