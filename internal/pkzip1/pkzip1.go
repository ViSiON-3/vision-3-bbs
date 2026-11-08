// Package pkzip1 reads ZIP members compressed with the PKZIP 1.x methods
// that archive/zip doesn't: shrinking (1), reducing (2 to 5) and imploding
// (6). Old art packs and BBS uploads from before PKZIP 2.04 (1993) use them.
// Members are unpacked in memory; nothing is written to disk.
package pkzip1

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
)

// The methods this package unpacks.
const (
	Shrunk   = 1
	Reduced1 = 2
	Reduced4 = 5
	Imploded = 6
)

// ErrTooLarge is returned for a member that unpacks to more than the limit
// the caller set.
var ErrTooLarge = errors.New("pkzip1: member too large")

// Supported reports whether a member compressed with method can be read:
// stored and deflated through archive/zip, the PKZIP 1.x methods here.
func Supported(method uint16) bool {
	return method == zip.Store || method == zip.Deflate || (method >= Shrunk && method <= Imploded)
}

// Encrypted reports whether f is encrypted, which nothing here can read.
func Encrypted(f *zip.File) bool { return f.Flags&1 != 0 }

// Open opens member f for reading, as f.Open does, for every method
// Supported allows; encrypted members are refused. A PKZIP 1.x member is unpacked in memory and checked
// against its CRC first, so it is refused, with ErrTooLarge, when its
// header says it unpacks to more than limit bytes. Other methods return
// zip.ErrAlgorithm, as f.Open would.
func Open(f *zip.File, limit int64) (io.ReadCloser, error) {
	if !Supported(f.Method) || Encrypted(f) {
		return nil, zip.ErrAlgorithm
	}
	if f.Method == zip.Store || f.Method == zip.Deflate {
		return f.Open()
	}
	if f.UncompressedSize64 > uint64(limit) {
		return nil, ErrTooLarge
	}
	raw, err := f.OpenRaw()
	if err != nil {
		return nil, err
	}
	// Old methods compress, so the packed data is no bigger than the
	// member plus its trees; more than that is a broken header.
	src, err := io.ReadAll(io.LimitReader(raw, limit+64*1024))
	if err != nil {
		return nil, err
	}
	data, err := Decompress(f.Method, f.Flags, src, int(f.UncompressedSize64))
	if err != nil {
		return nil, err
	}
	if crc32.ChecksumIEEE(data) != f.CRC32 {
		return nil, zip.ErrChecksum
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

// Decompress unpacks src, compressed with method (one of the PKZIP 1.x
// methods) and the member's general-purpose flags, to size bytes.
func Decompress(method, flags uint16, src []byte, size int) ([]byte, error) {
	switch {
	case method == Shrunk:
		return unshrink(src, size)
	case method >= Reduced1 && method <= Reduced4:
		return unreduce(src, size, int(method-Reduced1)+1)
	case method == Imploded:
		return explode(src, size, flags)
	}
	return nil, fmt.Errorf("pkzip1: method %d: %w", method, zip.ErrAlgorithm)
}
