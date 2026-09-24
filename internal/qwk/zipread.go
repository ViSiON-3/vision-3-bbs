package qwk

import (
	"archive/zip"
	"fmt"
	"io"
)

// Caps on how much a single packet member may expand to. Packets arrive from
// hubs over plain FTP and from users uploading REPs, and DEFLATE can inflate
// a small archive a thousandfold, so nothing is read without a bound. The
// limits sit far above any real packet: a MESSAGES.DAT this size is several
// hundred thousand messages.
const (
	maxMessageDataSize = 256 << 20 // MESSAGES.DAT, <ID>.MSG and HEADERS.DAT
	maxControlDataSize = 4 << 20   // CONTROL.DAT: identity and conference list
)

// readZipEntryLimited reads one archive member, refusing it once it passes max
// bytes. The declared size is checked first as a cheap reject, but the limit
// is enforced on the bytes actually inflated, since the header can lie.
func readZipEntryLimited(f *zip.File, max int64) ([]byte, error) {
	if f.UncompressedSize64 > uint64(max) {
		return nil, fmt.Errorf("%s is %d bytes uncompressed, over the %d-byte limit", f.Name, f.UncompressedSize64, max)
	}
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }() // read-only zip entry
	data, err := io.ReadAll(io.LimitReader(rc, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > max {
		return nil, fmt.Errorf("%s inflates past the %d-byte limit", f.Name, max)
	}
	return data, nil
}
