package qwk

import (
	"archive/zip"
	"io"
	"strings"
)

// ReadArchiveHeaders returns the parsed HEADERS.DAT of a QWK or REP archive,
// or nil when the archive carries none.
func ReadArchiveHeaders(r io.ReaderAt, size int64) (map[int]ExtHeader, error) {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return nil, err
	}
	for _, f := range zr.File {
		if !strings.EqualFold(f.Name, "HEADERS.DAT") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		data, err := io.ReadAll(rc)
		_ = rc.Close() // read-only zip entry
		if err != nil {
			return nil, err
		}
		return parseHeadersDAT(data), nil
	}
	return nil, nil
}
