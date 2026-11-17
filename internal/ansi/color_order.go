package ansi

import "strconv"

// colorOrder maps a colour 0-7 between DOS order and ANSI order. It swaps
// blue and red, and cyan and brown, so it is its own inverse.
var colorOrder = [8]int{0, 4, 2, 6, 1, 5, 3, 7}

// ColorOrder converts a colour between the two orders the codes use: DOS
// order, which the foreground codes |00-|07 and a DOS attribute follow (1
// blue, 3 cyan, 4 red, 6 brown), and ANSI order, which the background codes
// |B0-|B7 and SGR follow (1 red, 3 brown, 4 blue, 6 cyan). The mapping is
// its own inverse, so the one function converts either way. Only the low
// three bits of n count, so a bright colour maps as its plain one.
func ColorOrder(n int) int { return colorOrder[n&7] }

// PipeBackground returns the background colour a pipe code such as
// "|00|B6" sets, 0-7 in ANSI order (a bright |B8-|B15 as its plain colour),
// and false when it sets none.
func PipeBackground(code string) (int, bool) {
	for i := 0; i+2 < len(code); i++ {
		if code[i] != '|' || code[i+1] != 'B' {
			continue
		}
		j := i + 2
		for j < len(code) && j < i+4 && code[j] >= '0' && code[j] <= '9' {
			j++
		}
		if n, err := strconv.Atoi(code[i+2 : j]); err == nil && n <= 15 {
			return n & 7, true
		}
	}
	return 0, false
}
