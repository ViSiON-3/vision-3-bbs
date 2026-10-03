// Package lha reads LHA (LZH) archives: the format Amiga networks, and many
// older BBS file areas, pack their files in. It lists an archive's members
// and extracts the ones stored (-lh0-) or compressed with the static Huffman
// methods (-lh4- to -lh7-), which cover everything current LHA tools write.
package lha

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
)

// File is one member of an archive.
type File struct {
	Name   string // the member's name, without any directory it was stored under
	Method string // compression method, e.g. "-lh5-"
	Size   int64  // uncompressed size

	crc  uint16
	data []byte // the compressed data
}

// IsArchive reports whether data starts like an LHA archive: a member header
// whose method is "-lh?-" or "-lz?-".
func IsArchive(data []byte) bool {
	return len(data) >= 21 && data[2] == '-' && data[3] == 'l' &&
		(data[4] == 'h' || data[4] == 'z') && data[6] == '-'
}

// Read lists the members of the archive in data.
func Read(data []byte) ([]File, error) {
	var files []File
	for off := 0; off < len(data) && data[off] != 0; {
		f, next, err := readHeader(data, off)
		if err != nil {
			return nil, err
		}
		files = append(files, f)
		off = next
	}
	if len(files) == 0 {
		return nil, errors.New("lha: archive has no members")
	}
	return files, nil
}

// Extract decompresses f. It refuses a member larger than limit bytes.
func (f File) Extract(limit int64) ([]byte, error) {
	if f.Size > limit {
		return nil, fmt.Errorf("lha: %s is larger than the %d-byte limit", f.Name, limit)
	}
	var (
		out []byte
		err error
	)
	switch f.Method {
	case "-lh0-", "-lz4-":
		if int64(len(f.data)) != f.Size {
			return nil, fmt.Errorf("lha: %s is stored with %d bytes, its header says %d", f.Name, len(f.data), f.Size)
		}
		out = bytes.Clone(f.data)
	case "-lh4-":
		out, err = decode(f.data, int(f.Size), 12)
	case "-lh5-":
		out, err = decode(f.data, int(f.Size), 13)
	case "-lh6-":
		out, err = decode(f.data, int(f.Size), 15)
	case "-lh7-":
		out, err = decode(f.data, int(f.Size), 16)
	default:
		return nil, fmt.Errorf("lha: %s uses compression method %s, which is not supported", f.Name, f.Method)
	}
	if err != nil {
		return nil, fmt.Errorf("lha: %s: %w", f.Name, err)
	}
	if got := crc16(out); got != f.crc {
		return nil, fmt.Errorf("lha: %s has CRC %04x, its header says %04x", f.Name, got, f.crc)
	}
	return out, nil
}

// readHeader reads the member header at off and returns the member and the
// offset of the next header. Header levels 0, 1 and 2 are read; level 3 is
// rare enough to leave out.
func readHeader(data []byte, off int) (File, int, error) {
	h := data[off:]
	if len(h) < 22 {
		return File{}, 0, errors.New("lha: truncated member header")
	}
	if !IsArchive(h) {
		return File{}, 0, fmt.Errorf("lha: no member header at offset %d", off)
	}
	f := File{
		Method: string(h[2:7]),
		Size:   int64(binary.LittleEndian.Uint32(h[11:15])),
	}
	// int64, so a corrupt size cannot overflow the bounds checks below on a
	// 32-bit build.
	packed := int64(binary.LittleEndian.Uint32(h[7:11]))

	var (
		dataStart int
		name      string
	)
	switch level := h[20]; level {
	case 0, 1:
		// The first byte is the header's length, not counting itself or the
		// checksum byte after it.
		size := int(h[0]) + 2
		if size < 24 || size > len(h) {
			return File{}, 0, errors.New("lha: bad member header size")
		}
		// The second byte is the sum of the bytes after it, so a damaged
		// name or size is caught before it is used.
		var sum byte
		for _, b := range h[2:size] {
			sum += b
		}
		if sum != h[1] {
			return File{}, 0, fmt.Errorf("lha: member header at offset %d is damaged (bad checksum)", off)
		}
		nameLen := int(h[21])
		if 22+nameLen+2 > size {
			return File{}, 0, errors.New("lha: member name runs past its header")
		}
		name = string(h[22 : 22+nameLen])
		f.crc = binary.LittleEndian.Uint16(h[22+nameLen:])
		dataStart = size
		if level == 1 {
			// Extended headers follow the base header and are counted in
			// the packed size; the size of the first is the base header's
			// last two bytes.
			next := int(binary.LittleEndian.Uint16(h[size-2:]))
			pos := size
			for next != 0 {
				if next < 3 || pos+next > len(h) {
					return File{}, 0, errors.New("lha: bad extended header")
				}
				if n := extName(h[pos : pos+next-2]); n != "" {
					name = n
				}
				pos += next
				packed -= int64(next)
				next = int(binary.LittleEndian.Uint16(h[pos-2:]))
			}
			if packed < 0 {
				return File{}, 0, errors.New("lha: extended headers are larger than the member")
			}
			dataStart = pos
		}
	case 2:
		// The first two bytes are the length of the whole header, extended
		// headers included.
		size := int(binary.LittleEndian.Uint16(h[0:2]))
		if size < 26 || size > len(h) {
			return File{}, 0, errors.New("lha: bad member header size")
		}
		f.crc = binary.LittleEndian.Uint16(h[21:23])
		next := int(binary.LittleEndian.Uint16(h[24:26]))
		for pos := 26; next != 0; {
			if next < 3 || pos+next > size {
				return File{}, 0, errors.New("lha: bad extended header")
			}
			if n := extName(h[pos : pos+next-2]); n != "" {
				name = n
			}
			pos += next
			next = int(binary.LittleEndian.Uint16(h[pos-2:]))
		}
		dataStart = size
	default:
		return File{}, 0, fmt.Errorf("lha: header level %d is not supported", level)
	}

	if packed < 0 || packed > int64(len(h)-dataStart) {
		return File{}, 0, errors.New("lha: member data runs past the end of the archive")
	}
	end := dataStart + int(packed)
	f.Name = baseName(name)
	f.data = h[dataStart:end]
	return f, off + end, nil
}

// extName returns the file name an extended header carries (type 1), or "".
// ext is the header without its trailing next-size field.
func extName(ext []byte) string {
	if len(ext) > 1 && ext[0] == 0x01 {
		return string(ext[1:])
	}
	return ""
}

// baseName drops any directory from a stored name. LHA separates directories
// with 0xFF in extended headers, and DOS archivers with a backslash.
func baseName(name string) string {
	for i := len(name) - 1; i >= 0; i-- {
		if c := name[i]; c == '/' || c == '\\' || c == 0xFF {
			return name[i+1:]
		}
	}
	return name
}

// crcTable is CRC-16/ARC (polynomial 0xA001, reflected), the CRC LHA uses.
var crcTable = func() (t [256]uint16) {
	for i := range t {
		c := uint16(i)
		for range 8 {
			if c&1 != 0 {
				c = c>>1 ^ 0xA001
			} else {
				c >>= 1
			}
		}
		t[i] = c
	}
	return t
}()

func crc16(data []byte) uint16 {
	var c uint16
	for _, b := range data {
		c = crcTable[byte(c)^b] ^ c>>8
	}
	return c
}
