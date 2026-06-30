package qwk

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"strings"
)

// WriteREP writes a QWK REP packet (ZIP archive) containing a single
// BBSID.MSG file, using the same 128-byte block format as MESSAGES.DAT.
//
// This is the symmetric counterpart to ReadREP. It is primarily useful for
// generating reply packets in tests and fixtures, and as the foundation for a
// future reply compiler. It intentionally emits only the baseline block format
// (no HEADERS.DAT or Synchronet extension tags).
func WriteREP(w io.Writer, bbsID string, msgs []PacketMessage) error {
	zw := zip.NewWriter(w)

	var msgBuf bytes.Buffer

	// First block is a spacer, mirroring MESSAGES.DAT. Readers skip it.
	spacer := make([]byte, BlockSize)
	for i := range spacer {
		spacer[i] = ' '
	}
	copy(spacer, "Produced by ViSiON/3 BBS")
	msgBuf.Write(spacer)

	for _, msg := range msgs {
		msgBytes := formatMessage(msg)
		numBlocks := (len(msgBytes) + BlockSize - 1) / BlockSize

		padded := make([]byte, numBlocks*BlockSize)
		for i := range padded {
			padded[i] = ' '
		}
		copy(padded, msgBytes)
		msgBuf.Write(padded)
	}

	name := strings.ToUpper(bbsID) + ".MSG"
	if err := writeZipEntry(zw, name, msgBuf.Bytes()); err != nil {
		zw.Close()
		return fmt.Errorf("%s: %w", name, err)
	}
	// Close explicitly so a central-directory flush failure is reported rather
	// than silently producing a truncated archive.
	if err := zw.Close(); err != nil {
		return fmt.Errorf("finalize REP archive: %w", err)
	}
	return nil
}
