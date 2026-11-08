package pkzip1

import (
	"errors"
	"fmt"
	"slices"
)

// Imploding (method 6), PKZIP 1.x's LZ77 with Shannon-Fano coding, per
// PKWARE's APPNOTE.TXT. Flags bit 1 selects an 8K window (else 4K) and bit 2
// a third tree for literals (else literals are 8 raw bits).

const (
	implode8K       = 1 << 1
	implodeLitTree  = 1 << 2
	maxSFCodeLength = 16
)

var errBadTree = errors.New("pkzip1: bad Shannon-Fano tree")

// sfTree decodes one Shannon-Fano tree: table holds, for every possible
// next maxLen bits, the symbol whose code they start with and that code's
// length.
type sfTree struct {
	maxLen int
	sym    []uint16
	length []uint8
}

// readSFTree reads a tree of n symbols as APPNOTE stores it: a byte giving
// the number of bytes that follow, less one, then bytes each giving a bit
// length (low 4 bits, less one) and how many symbols in a row have it (high
// 4 bits, less one).
func readSFTree(br *bitReader, n int) (*sfTree, error) {
	count, err := br.read(8)
	if err != nil {
		return nil, err
	}
	lengths := make([]uint8, 0, n)
	for range count + 1 {
		v, err := br.read(8)
		if err != nil {
			return nil, err
		}
		for range v>>4 + 1 {
			if len(lengths) == n {
				return nil, errBadTree
			}
			lengths = append(lengths, uint8(v&15)+1)
		}
	}
	if len(lengths) != n {
		return nil, errBadTree
	}
	return buildSFTree(lengths)
}

// buildSFTree gives each symbol its code as APPNOTE assigns them: symbols
// sorted by bit length (ties in symbol order), codes counted up from zero
// starting with the last, as 16-bit values whose top bits are the code.
// The codes are sent with their bits reversed, low bit first.
func buildSFTree(lengths []uint8) (*sfTree, error) {
	order := make([]int, len(lengths))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int { return int(lengths[a]) - int(lengths[b]) })
	t := &sfTree{maxLen: int(lengths[order[len(order)-1]])}
	t.sym = make([]uint16, 1<<t.maxLen)
	t.length = make([]uint8, 1<<t.maxLen)
	var code, inc uint32
	var last uint8
	for i := len(order) - 1; i >= 0; i-- {
		s := order[i]
		code += inc
		if code > 0xFFFF {
			return nil, errBadTree
		}
		if lengths[s] != last {
			last = lengths[s]
			inc = 1 << (16 - last)
		}
		// The code's top lengths[s] bits, reversed: the order they arrive in.
		L := int(lengths[s])
		var rev uint32
		for b := range L {
			if code&(1<<(15-b)) != 0 {
				rev |= 1 << b
			}
		}
		for k := uint32(0); k < 1<<(t.maxLen-L); k++ {
			idx := rev | k<<L
			if t.length[idx] != 0 {
				return nil, errBadTree
			}
			t.sym[idx], t.length[idx] = uint16(s), lengths[s]
		}
	}
	return t, nil
}

// decode reads one symbol.
func (t *sfTree) decode(br *bitReader) (int, error) {
	idx := br.peek(t.maxLen)
	L := t.length[idx]
	if L == 0 {
		return 0, errBadTree
	}
	if err := br.skip(int(L)); err != nil {
		return 0, err
	}
	return int(t.sym[idx]), nil
}

// explode unpacks an imploded member to size bytes.
func explode(src []byte, size int, flags uint16) ([]byte, error) {
	br := &bitReader{src: src}
	lowBits, minMatch := 6, 2
	if flags&implode8K != 0 {
		lowBits = 7
	}
	var lit *sfTree
	var err error
	if flags&implodeLitTree != 0 {
		minMatch = 3
		if lit, err = readSFTree(br, 256); err != nil {
			return nil, fmt.Errorf("literal tree: %w", err)
		}
	}
	lenTree, err := readSFTree(br, 64)
	if err != nil {
		return nil, fmt.Errorf("length tree: %w", err)
	}
	distTree, err := readSFTree(br, 64)
	if err != nil {
		return nil, fmt.Errorf("distance tree: %w", err)
	}
	out := make([]byte, 0, size)
	for len(out) < size {
		isLit, err := br.read(1)
		if err != nil {
			return nil, err
		}
		if isLit == 1 {
			var c int
			if lit != nil {
				c, err = lit.decode(br)
			} else {
				var v uint32
				v, err = br.read(8)
				c = int(v)
			}
			if err != nil {
				return nil, err
			}
			out = append(out, byte(c))
			continue
		}
		low, err := br.read(lowBits)
		if err != nil {
			return nil, err
		}
		high, err := distTree.decode(br)
		if err != nil {
			return nil, err
		}
		dist := high<<lowBits | int(low) + 1
		n, err := lenTree.decode(br)
		if err != nil {
			return nil, err
		}
		if n == 63 {
			extra, err := br.read(8)
			if err != nil {
				return nil, err
			}
			n += int(extra)
		}
		out = copyMatch(out, dist, n+minMatch, size)
	}
	return out, nil
}

// copyMatch appends n bytes copied from dist back in out, stopping at size.
// Bytes from before the start of out are zeros, as Info-ZIP's unzip reads
// them.
func copyMatch(out []byte, dist, n, size int) []byte {
	for range n {
		if len(out) >= size {
			break
		}
		var c byte
		if dist <= len(out) {
			c = out[len(out)-dist]
		}
		out = append(out, c)
	}
	return out
}
