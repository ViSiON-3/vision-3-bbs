package pkzip1

import "errors"

// Shrinking (method 1), PKZIP 1.x's LZW: codes of 9 to 13 bits, with code 256
// followed by 1 to widen the codes or by 2 for a partial clear, which frees
// every entry that isn't the prefix of another. New entries take the lowest
// free code. PKZIP clears only when the table is full; the rules here are
// APPNOTE's, which 7-Zip and Info-ZIP's unzip agree on for such streams.

const (
	shrinkTableSize = 8192
	shrinkFree      = shrinkTableSize // parent of an unused code
	shrinkControl   = 256
	shrinkMaxBits   = 13
)

var errBadShrink = errors.New("pkzip1: bad shrunk data")

func unshrink(src []byte, size int) ([]byte, error) {
	br := &bitReader{src: src}
	var parent [shrinkTableSize]uint16
	var suffix [shrinkTableSize]byte
	for i := shrinkControl + 1; i < shrinkTableSize; i++ {
		parent[i] = shrinkFree
	}
	var stack [shrinkTableSize]byte
	out := make([]byte, 0, size)
	nbits := 9
	head := shrinkControl + 1 // no code below head is free
	clears := 0
	prev := -1
	nextFree := func() int {
		for head < shrinkTableSize && parent[head] != shrinkFree {
			head++
		}
		return head
	}
	// str writes code's string into the end of stack and returns it.
	str := func(code int) ([]byte, error) {
		i := len(stack)
		for code > shrinkControl {
			if parent[code] == shrinkFree || i == 1 {
				return nil, errBadShrink
			}
			i--
			stack[i] = suffix[code]
			code = int(parent[code])
		}
		i--
		stack[i] = byte(code)
		return stack[i:], nil
	}
	for len(out) < size {
		v, err := br.read(nbits)
		if err != nil {
			return nil, err
		}
		code := int(v)
		if code == shrinkControl {
			sub, err := br.read(nbits)
			if err != nil {
				return nil, err
			}
			switch sub {
			case 1:
				if nbits++; nbits > shrinkMaxBits {
					return nil, errBadShrink
				}
			case 2:
				// Each clear scans the table. PKZIP clears only when the
				// table is full, thousands of bytes apart, so many clears
				// in little output is a broken or hostile stream.
				if clears++; clears > len(out)/1024+8 {
					return nil, errBadShrink
				}
				var hasChild [shrinkTableSize]bool
				for c := shrinkControl + 1; c < shrinkTableSize; c++ {
					if p := parent[c]; p != shrinkFree {
						hasChild[p] = true
					}
				}
				for c := shrinkControl + 1; c < shrinkTableSize; c++ {
					if !hasChild[c] {
						parent[c] = shrinkFree
					}
				}
				head = shrinkControl + 1
			default:
				return nil, errBadShrink
			}
			continue
		}
		if prev < 0 {
			// The first code is a byte.
			if code > 0xFF {
				return nil, errBadShrink
			}
			out = append(out, byte(code))
			prev = code
			continue
		}
		var s []byte
		if code > shrinkControl && parent[code] == shrinkFree {
			// KwKwK: the entry this code is about to get, the previous
			// string plus its own first byte.
			if code != nextFree() {
				return nil, errBadShrink
			}
			p, err := str(prev)
			if err != nil {
				return nil, err
			}
			first := p[0]
			s = append(append(make([]byte, 0, len(p)+1), p...), first)
		} else {
			if s, err = str(code); err != nil {
				return nil, err
			}
		}
		if c := nextFree(); c < shrinkTableSize {
			parent[c] = uint16(prev)
			suffix[c] = s[0]
			head++
		}
		out = append(out, s...)
		prev = code
	}
	if len(out) > size {
		out = out[:size]
	}
	return out, nil
}
