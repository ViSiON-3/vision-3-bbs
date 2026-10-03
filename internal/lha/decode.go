package lha

import "errors"

// The -lh4- to -lh7- methods: LZ77 over a sliding window (4KB for -lh4-, up
// to 64KB for -lh7-), with literals, match lengths and match distances coded
// by Huffman trees sent at the start of each block. The trees are canonical:
// only each symbol's code length is sent.

const (
	maxMatch  = 256
	threshold = 3                              // shortest match
	numC      = 256 + maxMatch - threshold + 2 // literals and match lengths: 510
	cBits     = 9                              // bits that count c lengths
	numT      = 19                             // symbols that code c lengths
	tBits     = 5
	maxCode   = 16 // longest Huffman code
)

var errCorrupt = errors.New("compressed data is corrupt")

// decode decompresses size bytes of -lh4-/-lh5-/-lh6-/-lh7- data whose window
// is 1<<dicBits bytes.
func decode(data []byte, size, dicBits int) ([]byte, error) {
	numP := dicBits + 1 // distance classes
	pBits := 4
	if dicBits > 13 {
		pBits = 5
	}
	r := bitReader{data: data}
	out := make([]byte, 0, size)
	var c, t, p huffman
	blockLeft := 0
	for len(out) < size {
		if blockLeft == 0 {
			blockLeft = r.bits(16)
			if err := t.readLengths(&r, numT, tBits, 3); err != nil {
				return nil, err
			}
			if err := c.readCLengths(&r, &t); err != nil {
				return nil, err
			}
			if err := p.readLengths(&r, numP, pBits, -1); err != nil {
				return nil, err
			}
		}
		blockLeft--
		sym, err := c.decode(&r)
		if err != nil {
			return nil, err
		}
		if sym < 256 {
			out = append(out, byte(sym))
			continue
		}
		length := sym - 256 + threshold
		dist, err := p.decode(&r)
		if err != nil {
			return nil, err
		}
		if dist > 1 {
			dist = 1<<(dist-1) + r.bits(dist-1)
		}
		from := len(out) - dist - 1
		if from < 0 || len(out)+length > size {
			return nil, errCorrupt
		}
		for i := range length {
			out = append(out, out[from+i])
		}
	}
	if r.over > 16 {
		return nil, errCorrupt
	}
	return out, nil
}

// bitReader reads bits most significant first. Past the end of the data it
// reads zeros, counting them in over: an encoder may stop short of the last
// byte of a code it peeks past.
type bitReader struct {
	data []byte
	pos  int // next bit
	over int
}

func (r *bitReader) bit() int {
	i, shift := r.pos/8, 7-r.pos%8
	r.pos++
	if i >= len(r.data) {
		r.over++
		return 0
	}
	return int(r.data[i]>>shift) & 1
}

func (r *bitReader) bits(n int) int {
	v := 0
	for range n {
		v = v<<1 | r.bit()
	}
	return v
}

// huffman is a canonical Huffman code, decoded a bit at a time: codes of the
// same length are consecutive, in symbol order, and shorter codes come first.
type huffman struct {
	count  [maxCode + 1]int // codes of each length
	symbol []int            // symbols in code order
	only   int              // the symbol, for a code with a single zero-length one
}

// build makes the code from each symbol's code length (0: unused).
func (h *huffman) build(lengths []int) error {
	h.count = [maxCode + 1]int{}
	for _, l := range lengths {
		if l > maxCode {
			return errCorrupt
		}
		h.count[l]++
	}
	h.count[0] = 0
	// A code that is over-full cannot be decoded.
	left := 1
	for l := 1; l <= maxCode; l++ {
		left = left<<1 - h.count[l]
		if left < 0 {
			return errCorrupt
		}
	}
	h.symbol = h.symbol[:0]
	for l := 1; l <= maxCode; l++ {
		for s, sl := range lengths {
			if sl == l {
				h.symbol = append(h.symbol, s)
			}
		}
	}
	h.only = -1
	return nil
}

// single makes a code with one symbol and no bits.
func (h *huffman) single(sym int) {
	h.count = [maxCode + 1]int{}
	h.symbol = h.symbol[:0]
	h.only = sym
}

func (h *huffman) decode(r *bitReader) (int, error) {
	if h.only >= 0 {
		return h.only, nil
	}
	code, first, index := 0, 0, 0
	for l := 1; l <= maxCode; l++ {
		code |= r.bit()
		n := h.count[l]
		if code-first < n {
			return h.symbol[index+code-first], nil
		}
		index += n
		first = (first + n) << 1
		code <<= 1
	}
	return 0, errCorrupt
}

// readLengths reads the code lengths of the t (c length) or p (distance)
// code. After the special'th length, two bits say how many zero lengths come
// next.
func (h *huffman) readLengths(r *bitReader, max, nBits, special int) error {
	n := r.bits(nBits)
	if n == 0 {
		sym := r.bits(nBits)
		if sym >= max {
			return errCorrupt
		}
		h.single(sym)
		return nil
	}
	if n > max {
		return errCorrupt
	}
	lengths := make([]int, max)
	for i := 0; i < n; {
		l := r.bits(3)
		if l == 7 {
			for r.bit() == 1 {
				if l++; l > maxCode {
					return errCorrupt
				}
			}
		}
		lengths[i] = l
		i++
		if i == special {
			for zeros := r.bits(2); zeros > 0 && i < n; zeros-- {
				i++
			}
		}
	}
	return h.build(lengths)
}

// readCLengths reads the literal and match-length code's lengths, coded with
// t. A t symbol of 0, 1 or 2 starts a run of zero lengths; any other is a
// length plus 2.
func (h *huffman) readCLengths(r *bitReader, t *huffman) error {
	n := r.bits(cBits)
	if n == 0 {
		sym := r.bits(cBits)
		if sym >= numC {
			return errCorrupt
		}
		h.single(sym)
		return nil
	}
	if n > numC {
		return errCorrupt
	}
	lengths := make([]int, numC)
	for i := 0; i < n; {
		sym, err := t.decode(r)
		if err != nil {
			return err
		}
		if sym > 2 {
			lengths[i] = sym - 2
			i++
			continue
		}
		zeros := 1
		switch sym {
		case 1:
			zeros = r.bits(4) + 3
		case 2:
			zeros = r.bits(cBits) + 20
		}
		if i+zeros > n {
			return errCorrupt
		}
		i += zeros
	}
	return h.build(lengths)
}
