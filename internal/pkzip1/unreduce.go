package pkzip1

import "errors"

// Reducing (methods 2 to 5, compression factor 1 to 4), PKZIP 0.9x's
// method, per APPNOTE: each byte is coded against a "follower set" of the
// bytes most likely after the previous one, and the bytes so decoded drive
// a small LZ77 expander in which DLE (144) introduces a length and distance.

const reduceDLE = 144

var errBadReduce = errors.New("pkzip1: bad reduced data")

// followerBits is how many bits index a follower set of n bytes.
func followerBits(n int) int {
	b := 1
	for 1<<b < n {
		b++
	}
	return b
}

func unreduce(src []byte, size int, factor int) ([]byte, error) {
	br := &bitReader{src: src}
	var sets [256][]byte
	for j := 255; j >= 0; j-- {
		n, err := br.read(6)
		if err != nil {
			return nil, err
		}
		if n > 32 {
			return nil, errBadReduce
		}
		sets[j] = make([]byte, n)
		for i := range sets[j] {
			v, err := br.read(8)
			if err != nil {
				return nil, err
			}
			sets[j][i] = byte(v)
		}
	}
	lenMask := 0xFF >> factor
	out := make([]byte, 0, size)
	var last byte
	next := func() (byte, error) {
		set := sets[last]
		if len(set) > 0 {
			bit, err := br.read(1)
			if err != nil {
				return 0, err
			}
			if bit == 0 {
				i, err := br.read(followerBits(len(set)))
				if err != nil {
					return 0, err
				}
				if int(i) >= len(set) {
					return 0, errBadReduce
				}
				last = set[i]
				return last, nil
			}
		}
		v, err := br.read(8)
		if err != nil {
			return 0, err
		}
		last = byte(v)
		return last, nil
	}
	for len(out) < size {
		c, err := next()
		if err != nil {
			return nil, err
		}
		if c != reduceDLE {
			out = append(out, c)
			continue
		}
		v, err := next()
		if err != nil {
			return nil, err
		}
		if v == 0 {
			out = append(out, reduceDLE)
			continue
		}
		n := int(v) & lenMask
		if n == lenMask {
			extra, err := next()
			if err != nil {
				return nil, err
			}
			n += int(extra)
		}
		d, err := next()
		if err != nil {
			return nil, err
		}
		dist := (int(v)>>(8-factor))<<8 | int(d) + 1
		out = copyMatch(out, dist, n+3, size)
	}
	return out, nil
}
