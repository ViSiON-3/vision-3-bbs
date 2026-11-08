package pkzip1

import "errors"

// errTruncated is returned when the compressed data ends before the member
// has been unpacked to its full size.
var errTruncated = errors.New("pkzip1: compressed data ends early")

// bitReader reads src least significant bit first, as PKZIP 1.x packs its
// codes.
type bitReader struct {
	src []byte
	pos int // next bit to read
}

// read returns the next n bits (n <= 24), the first read in the lowest bit.
func (b *bitReader) read(n int) (uint32, error) {
	v := b.peek(n)
	b.pos += n
	if b.pos > len(b.src)*8 {
		return 0, errTruncated
	}
	return v, nil
}

// peek returns the next n bits without consuming them, as zeros past the
// end of src.
func (b *bitReader) peek(n int) uint32 {
	var v uint32
	for i := 0; i < n; {
		byteIdx := (b.pos + i) >> 3
		if byteIdx >= len(b.src) {
			break
		}
		shift := (b.pos + i) & 7
		take := min(8-shift, n-i)
		v |= uint32(b.src[byteIdx]>>shift&(1<<take-1)) << i
		i += take
	}
	return v
}

// skip consumes n bits already peeked.
func (b *bitReader) skip(n int) error {
	b.pos += n
	if b.pos > len(b.src)*8 {
		return errTruncated
	}
	return nil
}
