package keystore

import (
	"crypto/sha256"
	"fmt"
	"strings"
)

// encodeMnemonic converts a 32-byte ed25519 seed to a 24-word BIP39 mnemonic.
func encodeMnemonic(seed []byte) (string, error) {
	if len(seed) != 32 {
		return "", fmt.Errorf("mnemonic: seed must be 32 bytes, got %d", len(seed))
	}
	cs := sha256.Sum256(seed)
	data := make([]byte, 33)
	copy(data, seed)
	data[32] = cs[0]

	words := make([]string, 24)
	for i := 0; i < 24; i++ {
		idx := extract11Bits(data, i*11)
		words[i] = bip39Words[idx]
	}
	return strings.Join(words, " "), nil
}

// decodeMnemonic converts a 24-word BIP39 mnemonic back to a 32-byte seed.
func decodeMnemonic(mnemonic string) ([]byte, error) {
	s := strings.ToLower(strings.TrimSpace(mnemonic))
	s = collapseWhitespace(s)
	words := strings.Split(s, " ")

	if len(words) != 24 {
		return nil, fmt.Errorf("mnemonic: expected 24 words, got %d", len(words))
	}

	var indices [24]int
	for i, w := range words {
		idx, ok := bip39Index[w]
		if !ok {
			return nil, fmt.Errorf("mnemonic: unknown word %q at position %d", w, i+1)
		}
		indices[i] = idx
	}

	data := make([]byte, 33)
	for i, idx := range indices {
		place11Bits(data, i*11, idx)
	}

	seed := data[:32]
	cs := sha256.Sum256(seed)
	if data[32] != cs[0] {
		return nil, fmt.Errorf("mnemonic: checksum mismatch (wrong word or typo)")
	}
	return seed, nil
}

func extract11Bits(data []byte, bitOffset int) int {
	val := 0
	for i := 0; i < 11; i++ {
		byteIdx := (bitOffset + i) / 8
		bitIdx := 7 - ((bitOffset + i) % 8)
		if data[byteIdx]&(1<<bitIdx) != 0 {
			val |= 1 << (10 - i)
		}
	}
	return val
}

func place11Bits(data []byte, bitOffset, val int) {
	for i := 0; i < 11; i++ {
		if val&(1<<(10-i)) != 0 {
			byteIdx := (bitOffset + i) / 8
			bitIdx := 7 - ((bitOffset + i) % 8)
			data[byteIdx] |= 1 << bitIdx
		}
	}
}

func collapseWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
