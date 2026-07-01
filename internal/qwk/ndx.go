package qwk

import "math"

// makeNDXRecord creates a 5-byte NDX index record.
// offset is the 1-based block number; conf is the conference number.
//
// The QWK spec requires offsets encoded as Microsoft BASIC single-precision
// float (MSBIN4 / MBF4), not IEEE 754. MBF4 layout (little-endian in file):
//
//	byte[0] = mantissa bits 7-0
//	byte[1] = mantissa bits 15-8
//	byte[2] = sign (bit 7) | mantissa bits 22-16
//	byte[3] = exponent (bias 128; 0 means zero)
//
// Conversion from IEEE 754: same mantissa/sign bits; MBF exponent = IEEE exponent + 2.
func makeNDXRecord(offset int, conf int) []byte {
	rec := make([]byte, 5)
	f := float32(offset)
	if f != 0 {
		ieee := math.Float32bits(f)
		sign := byte(ieee >> 31)
		ieeeExp := (ieee >> 23) & 0xFF
		mantissa := ieee & 0x7FFFFF
		mbfExp := ieeeExp + 2 // adjust bias: 127 → 128, plus implicit-bit shift
		rec[0] = byte(mantissa & 0xFF)
		rec[1] = byte((mantissa >> 8) & 0xFF)
		rec[2] = (sign << 7) | byte((mantissa>>16)&0x7F)
		rec[3] = byte(mbfExp & 0xFF)
	}
	rec[4] = byte(conf & 0xFF)
	return rec
}
